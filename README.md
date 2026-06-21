# kgraph

**See your Kubernetes cluster.** `kgraph` connects via your kubeconfig, discovers
every resource type dynamically (built-ins **and** CRDs), builds a typed graph of
the relationships, and turns it into:

- **layer-scoped diagrams** (ArgoCD, Crossplane, Cluster API, monitoring, Cilium, …),
- committable **docs-as-code** (Markdown + diagrams + inventory),
- **observed-traffic** maps from Cilium Hubble, with **security** analysis,
- an **interactive web UI** with Kubernetes icons and animated traffic,
- optional **natural-language** explanations via an LLM.

It's a single self-contained Go binary — diagrams render with the embedded
[D2](https://d2lang.com) engine and the web UI ships an embedded
[Cytoscape.js](https://js.cytoscape.org); no external binaries, no CDN.

## Contents

- [Why](#why) · [Install](#install) · [Quick start](#quick-start)
- [Commands](#commands) · [Common flags](#common-flags)
- [Docs-as-code](#docs-as-code) · [AI](#ai-optional)
- [Traffic](#traffic-cilium-hubble): [security](#security-overlay) ·
  [throughput](#throughput-rates-prometheus) · [TLS/mTLS](#tlsmtls-hubble-relay)
- [Web UI](#web-ui) · [Authentication](#authentication)
- [How it reads relationships](#how-it-reads-relationships) ·
  [Project layout](#project-layout)
- [Testing](#testing) · [Publishing](#publishing) ·
  [Development](#development-spec-driven) · [Limitations](#limitations--notes)

## Why

A real cluster has hundreds of resource types across many stacks. Dumping a whole
namespace into one diagram is unreadable. `kgraph` **detects the stacks present**
and lets you focus one at a time, so each picture stays small and meaningful — and
treats the cluster as what it is: a **graph** (ownerReferences, label selectors,
object references, observed flows), not a pile of YAML.

## Install

Requires Go 1.26+.

```bash
go build -o kgraph ./cmd/kgraph        # single self-contained binary
./kgraph --version
```

Or run the published container image (multi-arch, distroless):

```bash
docker run --rm -v ~/.kube:/home/nonroot/.kube:ro ghcr.io/villadalmine/kgraph doctor
```

`kgraph` uses your current kubeconfig/context (override with `--kubeconfig` /
`--context`). It is **read-only** — it never mutates the cluster.

## Quick start

```bash
kgraph doctor                       # what does this cluster support?
kgraph layers monitoring            # which stacks live in a namespace?
kgraph ns argocd -o argocd.svg      # diagram a namespace (auto-detect layers)
kgraph serve                        # interactive UI at http://127.0.0.1:8080
```

## Commands

| Command | What it does |
|---|---|
| `kgraph ns <ns>` | Diagram a namespace's topology (owner/selector/ref edges). `--traffic` overlays observed flows. |
| `kgraph cluster` | Diagram cluster-scoped resources (Crossplane, CAPI, CRDs…). |
| `kgraph layers <ns>` | List the stacks/layers detected in a namespace. |
| `kgraph doc <ns>` | Docs-as-code: a Markdown page + architecture & per-layer diagrams + inventory. |
| `kgraph traffic <ns>` | Observed network traffic via Cilium Hubble (security/L7/throughput overlays). |
| `kgraph explain <ns>` | LLM natural-language summary of a namespace/layer. |
| `kgraph ask <ns> "..."` | Ask the LLM a question about a namespace. |
| `kgraph doctor` | Read-only capability probes (cluster, Cilium, Hubble, Prometheus, LLM, stacks) with remediation. |
| `kgraph serve` | Interactive browser UI (icons, animated traffic, export). |

```bash
# Focus a single stack — fast and legible
kgraph ns monitoring --layer monitoring -o monitoring.svg
# Cluster-scoped
kgraph cluster --layer crossplane -o crossplane.svg
# Just the D2 source (instant, no layout)
kgraph ns longhorn-system --format d2
```

### Common flags

| Flag | Applies to | Meaning |
|---|---|---|
| `--layer <name>` | `ns`, `cluster`, `explain`, `ask` | Focus a single detected stack |
| `--format svg\|d2` | `ns`, `cluster`, `traffic` | Output format (default `svg`) |
| `-o, --output` | most | Output file / directory |
| `--all` | `ns`, `cluster`, `doc` | Keep noisy kinds (Secrets, ReplicaSets, RBAC, CRDs…) |
| `--icons` | `ns`, `cluster`, `doc` | Attach K8s icons to the SVG (references external URLs) |
| `--kubeconfig`, `--context` | all | Target a specific cluster/context |

By default low-signal kinds (Events, EndpointSlices, ReplicaSets, RBAC,
unreferenced ConfigMaps/Secrets, CRD objects, …) are pruned to keep diagrams
readable; pass `--all` to include them. Collection itself is fast (raised client
QPS + concurrent listing) and **RBAC-tolerant** — it skips what it can't read and
reports it.

## Docs-as-code

`kgraph doc <ns>` writes a committable Markdown page (default
`kgraph-docs/<ns>/`) that auto-documents a namespace, making editorial choices for
you:

- leads with an **Architecture** diagram of the whole namespace;
- breaks it into **layer** sections ordered by **significance tier**
  (control/GitOps → platform networking/PKI → observability → storage/runtime →
  apps), then by size;
- **folds** single-resource layers into the overview (listed in the table, no
  near-empty diagram);
- ends with a per-kind **inventory** table.

```bash
kgraph doc monitoring               # -> kgraph-docs/monitoring/
kgraph doc pihole --traffic         # also adds Observed-traffic + Security sections
kgraph doc monitoring --ai          # also embeds an AI overview
```

`--traffic` is opt-in and graceful: if Hubble isn't available the page is still
written with an explanatory note.

## AI (optional)

`kgraph explain` / `ask` and `doc --ai` use an LLM via
[OpenRouter](https://openrouter.ai) over the graph — a lightweight **GraphRAG**:
the graph itself is the retrieval context, not vector chunks.

```bash
export OPENROUTER_API_KEY=sk-or-...        # never hardcode or commit this
kgraph explain argocd --layer argocd
kgraph ask monitoring "How does Prometheus discover its targets?"
```

The key is read from `OPENROUTER_API_KEY` or a `.env` file in the working
directory (auto-loaded at startup; real env vars win). **Keep `.env` out of git.**
The default model is a free OpenRouter model with an automatic fallback chain
(free models are rate-limited); override with `--model` / `OPENROUTER_MODEL`. Any
OpenAI-compatible provider can be added behind the `llm.Provider` interface (BYO).

## Traffic (Cilium Hubble)

`kgraph traffic <ns>` draws **observed** network traffic (vs the declarative
topology). It connects to the Hubble Relay over gRPC, **port-forwarding it for
you** (no `hubble` CLI, no manual `kubectl port-forward`), samples recent flows,
aggregates them to workload level, and renders a directed graph where edge width
scales with volume and colour shows the verdict (blue = allowed, red = dropped).
External/world endpoints get their own group.

```bash
kgraph traffic pihole --last 3000
kgraph traffic monitoring --relay-addr 127.0.0.1:4245   # bring your own relay
kgraph ns pihole --traffic                              # overlay flows on the topology
```

`kgraph ns <ns> --traffic` overlays observed flows onto the **declarative
topology** in one diagram — "what's deployed" and "what actually talks" together.
The web UI exposes the same as the **topology+traffic** view. If Hubble isn't
available, `kgraph doctor` and the command tell you exactly what to enable.

### Security overlay

`--policy` overlays declarative **NetworkPolicy / CiliumNetworkPolicy /
CiliumClusterwideNetworkPolicy** coverage onto the observed traffic:

- workloads selected by **no** policy are flagged ⚠ with a red border (wide open),
- **denied** flows are red, **gap** flows (allowed → unprotected) are amber dashed;
- a summary prints policies found, denied flows, gap flows, unprotected workloads.

`--suggest-policy k8s|cilium` turns observed flows into least-privilege policies
(one per destination workload, allowing the in-cluster sources/ports actually
seen); external sources are listed as comments for ipBlock/toFQDNs.

```bash
kgraph traffic kagent --policy
kgraph traffic monitoring --suggest-policy cilium
```

`--l7` labels edges with observed HTTP (method/path/status/latency) and DNS
detail, colouring 5xx red. L7 needs Hubble L7 visibility (an L7 CNP or the
proxy-visibility annotation); otherwise it falls back to L4 ports and tells you
how to enable it.

### Throughput rates (Prometheus)

`--throughput` annotates each workload with its current **rx/tx bytes/sec** from
Prometheus (cAdvisor `container_network_*_bytes_total`). kgraph auto-discovers and
port-forwards Prometheus anywhere in the cluster (chart-neutral); `--prom
<host:port|url>` overrides. Graceful: if Prometheus/metrics are absent it warns
and renders without rates.

```bash
kgraph traffic pihole --throughput
```

Per-edge (pod→pod) rates would need Hubble flow metrics scraped into Prometheus
(a `hubble-metrics` ServiceMonitor); `kgraph doctor` flags whether that's in place.

### TLS/mTLS Hubble Relay

The auto port-forwarded relay is plain TCP. To reach a real **TLS** relay, use
`--relay-addr` with `--relay-tls` (plus `--relay-server-name`, `--relay-ca`, or
`--relay-tls-skip-verify`). For **mTLS**, add client certs from files
(`--relay-cert/--relay-key`) or from a TLS secret (`--relay-mtls[-secret]`).

```bash
kgraph traffic pihole --relay-addr relay.example:443 --relay-tls --relay-mtls
```

## Web UI

```bash
kgraph serve --addr 127.0.0.1:8080      # then open the URL
```

An interactive single-page UI (no build step — embedded Cytoscape.js, fully
offline):

- nodes show official **Kubernetes resource icons** (Pod, Deployment, Service,
  ConfigMap, … + a generic CRD icon) — toggle with **Icons**;
- **draggable** nodes, selectable **layouts** (force / hierarchy / concentric /
  grid), wheel-zoom, **Fit**;
- **hover** for kind/namespace/labels/throughput; **click** to highlight neighbours;
- on **traffic** / **topology+traffic**, flow edges are **animated** (moving
  dashes), width ∝ volume, colour by verdict (allowed / dropped / error / gap);
- tick **Live** to auto-refresh traffic every few seconds;
- **SVG ⭳** / **D2 ⭳** export the current view; declarative views are cached
  (60s) so revisiting is instant.

The interactive graph comes from `GET /api/graph` (JSON), which skips the SVG
layout — so even large namespaces load in a second or two.

## Authentication

`kgraph serve` is **open by default** and uses one kubeconfig: anyone who can
reach the URL sees what that kubeconfig can read. Recommended access models:

1. **Local** — run it on your machine, open `127.0.0.1`. Your machine, your RBAC.
2. **In-cluster + `kubectl port-forward`** — deploy kgraph into a namespace; it
   uses its ServiceAccount, and access is gated by Kubernetes RBAC on
   `pods/portforward`. Zero-config and secure.
3. **Exposed (ingress)** — needs a login gate. Optional **OIDC (Azure AD,
   Keycloak, Google…) / GitHub** auth is designed in
   [`specs/0015-web-oidc-auth.md`](specs/0015-web-oidc-auth.md) (not yet built).

## How it reads relationships

- **owner** (solid): `ownerReferences` (Deployment → Pod)
- **selector** (green dashed): label selectors (Service → Pods)
- **ref** (amber dashed): object references (Pod → ConfigMap/Secret/PVC/SA,
  Ingress/HTTPRoute → Service, PVC → PV)
- **flow** (weighted, blue/red): observed traffic from Hubble

Nodes are coloured by category and carry the Kubernetes icon for their kind, so
the same kind looks the same across every diagram.

## Project layout

```
cmd/kgraph/         entrypoint (+ version stamping)
internal/collect/   kubeconfig + dynamic discovery + concurrent, RBAC-tolerant listing
internal/graph/     typed graph model; relation builders, prune, subgraph, overlay, describe
internal/layers/    declarative stack-detection rules (+ significance tiers)
internal/render/    graph -> D2 -> SVG (embedded d2lib); category colours; icon mapping
internal/docs/      Markdown docs-as-code generator
internal/llm/       pluggable LLM provider (OpenRouter)
internal/preflight/ capability checks behind `kgraph doctor`
internal/traffic/   Hubble client, relay port-forward, policy analysis, policy suggest
internal/metrics/   tiny Prometheus client (throughput rates)
internal/web/       serve: JSON API + embedded interactive UI (Cytoscape + K8s icons)
internal/cli/       cobra commands
docs/ · specs/      SPEC, IMPROVEMENTS, TESTING, PUBLISHING + per-feature specs
```

Add a new stack by appending one rule to `internal/layers/layers.go`.

## Testing

```bash
go test ./...        # all unit tests are offline (fixtures); no cluster/network
```

For a full local walkthrough (CLI, traffic, AI, the interactive web UI, Docker)
see [docs/TESTING.md](docs/TESTING.md).

## Publishing

To put this on GitHub and publish releases/images with `gh`, follow
[docs/PUBLISHING.md](docs/PUBLISHING.md). In short:

```bash
cd graph-k8s-all
git init -b main && git add -A && git status   # confirm .env is NOT listed
git commit -m "Initial commit: kgraph"
gh repo create villadalmine/kgraph --public --source=. --remote=origin --push
git tag v0.1.0 && git push origin v0.1.0        # triggers release + ghcr image
```

CI (`.github/workflows/ci.yml`) runs build + vet + tests on every push; tagging
`v*` cross-compiles binaries (goreleaser) and publishes a multi-arch image to
`ghcr.io/villadalmine/kgraph`.

## Development (spec-driven)

kgraph is built **spec-first**: each feature has a spec it's checked against.

- [`docs/SPEC.md`](docs/SPEC.md) — vision, constitution, feature inventory, graph contract
- [`specs/`](specs/) — one numbered spec per feature (0001–0015)
- [`docs/IMPROVEMENTS.md`](docs/IMPROVEMENTS.md) — backlog & deferred items
- [`CHANGELOG.md`](CHANGELOG.md) — what changed, by phase

## Limitations & notes

- Large namespaces are slow to lay out as a **single SVG** — use `--layer` or
  `--format d2`. The **web UI** sidesteps this (client-side layout).
- `--icons` (SVG) references icons by URL; they show in a browser but may not
  render in GitHub's sanitized SVG viewer. The web UI embeds icons (offline).
- Free LLM models are rate-limited; the fallback chain helps, but heavy use
  benefits from your own/paid key.
- Embedded `internal/web/static/cytoscape.min.js` and the K8s icons are vendored
  (MIT / Kubernetes community set).
