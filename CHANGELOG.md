# Changelog

All notable changes to **kgraph** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project is pre-1.0 and not yet tagged in git; entries are grouped by the
development **phase** in which they landed (see `docs/SPEC.md` for the roadmap).

## [Unreleased]

### Added
- **Prometheus throughput rates on the traffic view** (`kgraph traffic
  --throughput`) — annotates each workload with live **rx/tx bytes/sec** from
  Prometheus (cAdvisor `container_network_*_bytes_total`), shown on the diagram
  and as a table. Auto-discovers & port-forwards Prometheus (`--prom` override),
  graceful if absent. New `internal/metrics` (dependency-free Prometheus client),
  `graph.Node.Note`, `traffic.PortForwardPrometheus`. Spec:
  `specs/0002-prometheus-rates.md`. (Per-edge rates need the hubble-metrics
  ServiceMonitor, which this cluster lacks — tracked as follow-up.)
- **Traffic & security posture in the namespace doc** (`kgraph doc --traffic`) —
  opt-in **Observed traffic** (Hubble) and **Security posture** (policy coverage:
  policies in effect, denied/gap flows, unprotected workloads) sections. Graceful:
  if Hubble is absent or quiet the doc still generates with a note. New
  `docs.Page.Traffic/TrafficNote/Security`. Spec:
  `specs/0004-autodoc-traffic-security.md`.
- **Opinionated namespace auto-documentation** (`kgraph doc`) — leads with a
  whole-namespace **Architecture** diagram, orders layer sections by a curated
  **significance tier** (control/GitOps → platform → observability → storage →
  apps) then size, and **folds** layers with a single resource into the overview
  instead of emitting near-empty diagrams. New `layers.Rule.Tier` +
  `layers.Rank`. Spec: `specs/0003-namespace-autodoc.md`.
- **Live traffic streaming in the web UI** (`/api/traffic/stream`, SSE) — the
  `traffic` view can now auto-refresh, keeping a single Hubble Relay
  port-forward open and re-rendering a rolling window of recent flows every few
  seconds. A **Live** toggle in the UI starts/stops the stream.
  Spec: `specs/0001-live-traffic-streaming.md`.

### Added
- **Kubernetes resource icons in the web view** — nodes show the official
  Kubernetes community icon for their kind (Pod, Deployment, Service, ConfigMap,
  …; generic CRD icon for custom resources), vendored & embedded (offline). New
  exported `render.IconName` (shared with the SVG `--icons`), an `icon` field on
  `/api/graph` nodes, and an **Icons** toggle (default on). Spec:
  `specs/0014-web-k8s-icons.md`.
- **Interactive web visualization** (`kgraph serve`) — the browser now renders
  the graph interactively with a vendored, embedded **Cytoscape.js** (no build
  step, fully offline): draggable nodes, selectable layouts (force/hierarchy/
  concentric/grid), hover tooltips, click-to-highlight neighbours, and **animated
  traffic** edges (flowing dashes, width ∝ volume, colour by verdict). A new
  `GET /api/graph` returns the graph as JSON — and skips the dagre layout, so
  views are far faster (monitoring **~1.6s** vs ~69s for the SVG). SVG/D2 export
  remain as buttons. Spec: `specs/0012-interactive-web-visualization.md`.
- **Combined topology + traffic view** (`kgraph ns <ns> --traffic`, web
  `topology+traffic`) — overlays observed Hubble flow edges onto the declarative
  namespace topology in one diagram, matching flows to workload nodes by
  `(namespace, name)` and pulling in external endpoints. New pure
  `graph.Overlay`. Spec: `specs/0007-combined-topology-traffic.md`.
- **Web UI: render cache, downloads, loading feedback, layer nudge**
  (`kgraph serve`) — declarative (ns/cluster) renders are memoized for 60s
  (repeat views are instant); the toolbar gains **SVG** and **D2** download
  buttons; a spinner shows and Render is disabled while rendering; large
  namespaces with no layer selected get a "pick a layer" hint. `/render` now
  accepts `&format=svg|d2` and returns an `X-Kgraph-Nodes` header. Spec:
  `specs/0006-web-ux-performance.md`.

### Build / distribution
- Added GitHub Actions **CI** (build + vet + `go test ./...`) and a **release**
  workflow, a **goreleaser** config (static cross-builds linux/darwin
  amd64/arm64, checksums), a **Dockerfile** (distroless static, non-root) and
  `.dockerignore`. `kgraph --version` reports build-stamped version/commit/date.
  Spec: `specs/0010-distribution.md`.
- Added a **container-image** workflow that builds a multi-arch (amd64/arm64)
  image and pushes it to `ghcr.io` on tag. Spec:
  `specs/0013-container-image-publish.md`. (Config files only — `git init`/first
  commit/enabling Actions is the user's call.)

### Tested
- Offline unit tests added for `traffic` (SuggestPolicies, selector matching,
  parsePort, flow reconcile), `web` (render cache, d2 emit), `cli` (defaultOut,
  loadDotEnv) and `render` (D2 determinism). All 9 internal packages now have
  tests. Spec: `specs/0008-test-hardening.md`.

### Fixed
- **StatefulSet pods now reconcile to their workload** in the traffic graph
  (ordinal `-N` suffix stripped in the fallback). Spec:
  `specs/0011-traffic-correctness-mtls.md`.
- **Pod-vs-Workload duplicate nodes** in the traffic graph: a workload seen with
  Hubble workload info on some flows and without it on others now collapses to a
  single node (richer kind kept, labels merged, edge weights summed). Spec:
  `specs/0005-cluster-agnostic-traffic.md`.

### Changed
- **Prometheus port read from the pod spec** instead of assuming 9090 (named
  `web`/`http`/`http-web`/`metrics`, else a 9090 port, else fallback). Spec:
  `specs/0009-agnostic-robustness.md`.
- **TLS/mTLS Hubble Relay support**: `kgraph traffic --relay-tls`
  (`--relay-tls-skip-verify`, `--relay-server-name`, `--relay-ca`) dials a real
  TLS relay; `--relay-cert/--relay-key` or `--relay-mtls[-secret]` add client
  certificates (mTLS, from files or a TLS secret). `traffic.FetchFlows` gained
  variadic `Option`s (`WithTLS`/`WithClientCert`/`WithMTLSPEM`), default
  behaviour unchanged. Specs: `specs/0009`, `specs/0011`.
- **Prometheus discovery is now cluster-agnostic**: `--throughput` finds
  Prometheus anywhere in the cluster via chart-neutral selectors instead of
  assuming the `monitoring` namespace / kube-prometheus-stack. Spec:
  `specs/0005-cluster-agnostic-traffic.md`.

### Planned
- Per-edge Prometheus rates once Hubble flow metrics are scraped (ServiceMonitor
  for `hubble-metrics`).
- Optional web authentication (generic OIDC / GitHub) for exposed `kgraph serve`;
  default stays open, with the in-cluster `kubectl port-forward` RBAC model as a
  zero-config secure option. Design: `specs/0015-web-oidc-auth.md` (draft).

---

## Phase 5 — Web UI (`kgraph serve`)

### Added
- `kgraph serve` HTTP server (`internal/web`) with an embedded single-page UI
  (`//go:embed index.html`): pick view (`ns` / `cluster` / `traffic`),
  namespace, layer and L7, render live with pan/zoom.
- JSON endpoints `/api/namespaces`, `/api/layers?ns=`, and `/render` that reuse
  the exact CLI pipeline.
- `collect.Namespaces(ctx)` accessor to populate the namespace selector.

## Phase 4b — L7 traffic detail

### Added
- `--l7` on `kgraph traffic`: labels edges with observed HTTP
  (method/path/status/latency) and DNS detail; `Edge.Error` (5xx) renders red.
- `FetchFlows` returns an `l7edges` count so the CLI warns and explains how to
  enable L7 visibility when none is configured (falls back to L4 ports).

## Phase 4a — Security overlay (policy coverage + generation)

### Added
- `--policy` overlays declarative **NetworkPolicy / CiliumNetworkPolicy /
  CiliumClusterwideNetworkPolicy** onto observed traffic
  (`internal/traffic/policy.go`): workloads selected by no policy are flagged ⚠
  (red border), denied flows render red, gap flows (allowed → unprotected) amber
  dashed. Prints a coverage summary.
- `--suggest-policy k8s|cilium` generates least-privilege ingress policies from
  observed in-cluster flows (`internal/traffic/suggest.go`); external sources
  listed as comments.

## Phase 4 — AI (GraphRAG over OpenRouter)

### Added
- `internal/llm`: pluggable `Provider` interface with an OpenRouter
  implementation; default free model + automatic fallback chain and retries for
  rate limits.
- `kgraph explain` / `kgraph ask`, and `kgraph doc --ai`: natural-language
  architecture summaries and Q&A, using the graph itself as retrieval context
  (`graph.Describe`).
- `.env` auto-loading at startup (`cli/root.go` `PersistentPreRun`); real env
  vars take precedence; `.env` is gitignored.

## Phase 3b — Capability checks (`kgraph doctor`)

### Added
- `internal/preflight` read-only probes (cluster, Cilium, Hubble, hubble-relay,
  Prometheus, LLM key, detected stacks), each with actionable remediation.
- `preflight.RequireHubble` guard used by the traffic command and web view.

## Phase 3a — Traffic via Cilium Hubble (`kgraph traffic`)

### Added
- `internal/traffic`: gRPC client to Hubble Relay (cilium `api/v1` observer+flow
  protos — kept a light, indirect dependency), with automatic relay
  port-forward via client-go (no `hubble` CLI / manual `kubectl port-forward`).
- Flow aggregation to workload level; edges weighted by volume and coloured by
  verdict (allowed/dropped); external/world endpoints grouped separately.
- Graph additions: `RelFlow`, `Edge.Weight/Dropped`, `AddFlowEdge*`.
- `--relay-addr` escape hatch (non-TLS relay port).

## Phase 3 — Docs-as-code (`kgraph doc`)

### Added
- `internal/docs` Markdown generator: a page per namespace with overview, per
  layer sections, an inventory table, and one embedded SVG per detected layer.

## Phase 2 — Layer detection & filtering

### Added
- `internal/layers`: declarative stack-detection rule catalog (argocd,
  argo-workflows, crossplane, capi, monitoring, cilium, longhorn, kubevirt,
  cert-manager, gateway, kagent, holmesgpt).
- `kgraph layers <ns>` and `--layer <name>` filtering via `graph.Subgraph`.
- `kgraph cluster` for cluster-scoped resources.

## Phase 1 — MVP

### Added
- `internal/collect`: kubeconfig connection + dynamic discovery + RESTMapper +
  concurrent listing (RBAC-tolerant). Raised client QPS=50/Burst=100.
- `internal/graph`: typed knowledge graph (Node/Edge), relation builders
  (ownerReference / label selector / object ref), prune of noise kinds, subgraph.
- `internal/render`: graph → D2 → SVG via embedded `d2lib` (category colours,
  horizontal layout, optional `--icons`).
- `kgraph ns <namespace>` (SVG / D2 output), cobra root with persistent
  `--kubeconfig` / `--context` flags.
