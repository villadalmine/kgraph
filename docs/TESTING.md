# Testing kgraph locally

A practical walkthrough to build, unit-test, and exercise every feature against a
real cluster. All commands run from the module root (`graph-k8s-all/`).

## 0. Prerequisites

- Go 1.26+ and a working `kubeconfig` (kgraph uses your current context).
- Optional: Cilium **Hubble** (for `traffic`), **Prometheus** (for
  `--throughput`), an `OPENROUTER_API_KEY` (for the AI commands).
- Check what your cluster supports at any time:

```bash
go build -o kgraph ./cmd/kgraph
./kgraph doctor      # read-only probes: cluster, Cilium, Hubble, Prometheus, LLM, stacks
```

## 1. Build & unit tests (offline, no cluster)

```bash
go build ./...          # compiles everything
go vet ./...            # static checks
go test ./...           # all unit tests (fixtures only — no cluster/network)
CGO_ENABLED=0 go build -o kgraph ./cmd/kgraph   # static build (as released)
./kgraph --version
```

## 2. CLI against your cluster

```bash
# Which stacks live in a namespace?
./kgraph layers monitoring

# Namespace topology (auto-detect layers, prune noise)
./kgraph ns pihole -o pihole.svg

# Focus one stack — fast and legible
./kgraph ns monitoring --layer monitoring -o monitoring.svg

# Cluster-scoped resources
./kgraph cluster --layer crossplane -o crossplane.svg

# Just the D2 source (instant, no layout)
./kgraph ns longhorn-system --format d2

# Docs-as-code: architecture + per-layer diagrams + inventory
./kgraph doc monitoring                 # -> kgraph-docs/monitoring/
./kgraph doc pihole --traffic           # also adds observed-traffic + security sections
```

## 3. Traffic (needs Cilium Hubble)

kgraph auto-port-forwards the Hubble relay — no `hubble` CLI needed.

```bash
./kgraph traffic pihole --last 3000                 # observed flows
./kgraph traffic kagent --policy                    # policy-coverage overlay
./kgraph traffic monitoring --suggest-policy cilium # generate least-privilege policies
./kgraph traffic pihole --l7                        # L7 (HTTP/DNS) detail if available
./kgraph traffic pihole --throughput                # rx/tx rates from Prometheus
./kgraph ns pihole --traffic                        # overlay flows on the topology

# TLS / mTLS relay (for a real, non-port-forwarded relay):
./kgraph traffic pihole --relay-addr relay:443 --relay-tls --relay-mtls
```

If something is missing, the command and `doctor` tell you exactly what to enable.

## 4. AI (optional, needs OpenRouter)

```bash
export OPENROUTER_API_KEY=sk-or-...     # or put it in a .env file (auto-loaded)
./kgraph explain argocd --layer argocd
./kgraph ask monitoring "How does Prometheus discover its targets?"
./kgraph doc monitoring --ai
```

## 5. The interactive web UI

```bash
./kgraph serve --addr 127.0.0.1:8080
# open http://127.0.0.1:8080
```

What to try in the browser:

1. **view = namespace**, pick `pihole` → **Render**. Drag nodes, scroll to zoom,
   hover a node for details, click a node to highlight its neighbours, change the
   **layout** dropdown, hit **Fit**.
2. **view = traffic**, pick `pihole` → **Render**. Watch the **animated** flow
   edges (moving dashes; width = volume; colour = verdict). Tick **Live** to keep
   it refreshing every few seconds.
3. **view = topology+traffic** → the declarative graph with observed flows
   overlaid in one picture.
4. **SVG ⭳ / D2 ⭳** to export the current view; **Render** on a big namespace
   with no layer shows a "pick a layer" hint.

Quick API smoke test (no browser):

```bash
curl -s "http://127.0.0.1:8080/api/namespaces" | head
curl -s "http://127.0.0.1:8080/api/graph?view=ns&ns=pihole" | head -c 300
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/static/cytoscape.min.js
```

The page works **offline** — Cytoscape.js is embedded in the binary, no CDN.

## 6. Everything at once

```bash
./demo.sh         # generates a sample set of diagrams/docs/policies into demo-out/
```

## 7. Container image

```bash
docker build -t kgraph:dev .
docker run --rm -v ~/.kube:/home/nonroot/.kube:ro kgraph:dev doctor
```
