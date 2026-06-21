# Spec 0002 — Prometheus throughput rates for traffic nodes

- **Status:** done (verified 2026-06-20: `traffic pihole --throughput` annotated
  pihole ↓185 B/s ↑207 B/s on diagram + table via auto + `--prom` paths; bad
  `--prom` warned and still rendered; unit tests for parseVector/WorkloadFromPod/
  HumanRate/D2 note green; build + `go test ./...` green)
- **Feature ID:** F18 (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

`kgraph traffic` edge weights come from **Hubble flow counts** over a sampled
window (`--last N`) — a sample, not a rate, dependent on Hubble retention. A
cluster usually already scrapes **Prometheus**, which can give true, smooth
**bytes/sec** rates. We want to enrich the traffic view with real throughput.

## 2. Design note — what the cluster actually exposes (decided 2026-06-20)

Probed the homelab Prometheus (`kube-prometheus-stack-prometheus:9090`):

- **Hubble flow metrics are NOT scraped** (`hubble_flows_processed_total` absent;
  `kgraph doctor` already warns there's no ServiceMonitor for `hubble-metrics`).
  → **Per-edge** (pod→pod) Prometheus rates are not possible here.
- **cAdvisor `container_network_{receive,transmit}_bytes_total` ARE present**,
  labelled `namespace,pod`. → **Per-workload throughput (rx/tx bytes/sec)** is
  reliably available.

So the honest, deliverable scope is **node throughput annotations** from
cAdvisor, not edge weights. (Per-edge rates remain possible later if the user
adds the hubble-metrics ServiceMonitor — tracked as a follow-up.) This is why the
flag is `--throughput`, not the originally-sketched `--rates`.

## 3. Goals

- `kgraph traffic <ns> --throughput` annotates each workload node in the traffic
  graph with its current **rx/tx bytes/sec** from Prometheus, and prints a
  throughput table.
- Auto-discover & port-forward Prometheus out of the box; `--prom <url|host:port>`
  override.
- Degrade gracefully: if Prometheus or the metrics are absent, warn with a clear
  remediation and render the traffic diagram without throughput (non-fatal).
- A small, dependency-free Prometheus HTTP client that is **offline-testable**.

## 4. Non-goals

- Per-edge (pod→pod) rates — needs hubble flow metrics (follow-up).
- Historical/range queries; this is instantaneous rate (`rate(...[5m])`).
- Replacing Hubble for verdicts/L7.

## 5. Design

**New `internal/metrics` package** (pure HTTP + JSON, no new deps):

- `type Sample struct { Labels map[string]string; Value float64 }`
- `type Client struct { base string; http *http.Client }`,
  `New(base string) *Client`, `(*Client).Query(ctx, promQL) ([]Sample, error)`
  hitting `GET {base}/api/v1/query`.
- `parseVector([]byte) ([]Sample, error)` — pure parser of the standard
  Prometheus `resultType:"vector"` response (unit-tested with a fixture).
- `type Rate struct { Rx, Tx float64 }` (bytes/sec).
- `(*Client).WorkloadRates(ctx, ns) (map[string]Rate, error)` — runs the rx and
  tx PromQL below, derives a workload key from each `pod` label via
  `WorkloadFromPod`, and sums pods into workloads.
- `WorkloadFromPod(pod string) string` — strips ReplicaSet+pod hashes
  (`pihole-7f69b95dc7-ccrf5`→`pihole`), StatefulSet ordinals
  (`…-prometheus-0`→`…-prometheus`) and DaemonSet/RS hashes. Pure, unit-tested.

PromQL (namespace-scoped, 5-minute rate):
```
sum by (namespace,pod) (rate(container_network_receive_bytes_total{namespace="<ns>"}[5m]))
sum by (namespace,pod) (rate(container_network_transmit_bytes_total{namespace="<ns>"}[5m]))
```

**Port-forward** (reuse `traffic`'s SPDY infra): refactor `portforward.go` to a
shared `forwardPod` helper; add `traffic.PortForwardPrometheus(ctx, cfg)` that
finds a running Prometheus pod in `monitoring` (selectors
`app.kubernetes.io/name=prometheus`, `app=prometheus`) and forwards `9090`.

**CLI wiring** (`kgraph traffic`): `--throughput` and `--prom`. After building the
flow graph, resolve the Prometheus address (`--prom` or port-forward), fetch
`WorkloadRates`, set `Node.Note` to `"↓<rate>/s ↑<rate>/s"` (human-formatted) for
matching nodes, and print a throughput table to stderr.

**Render**: `graph.Node` gains `Note string`; `render.D2` appends it to the node
label (only when set), so throughput shows on the diagram.

## 6. Contracts / interfaces

- `internal/metrics`: `Sample`, `Client`, `New`, `Query`, `Rate`,
  `WorkloadRates`, `WorkloadFromPod`, `parseVector` (unexported).
- `graph.Node` gains `Note string`.
- `traffic.PortForwardPrometheus(ctx, *rest.Config) (addr string, stop func(), err error)`.
- CLI flags on `traffic`: `--throughput` (bool), `--prom` (string).

## 7. Acceptance criteria

1. `metrics.parseVector` parses a recorded Prometheus vector JSON into samples
   with labels + float values.
2. `metrics.WorkloadFromPod` maps Deployment/StatefulSet/DaemonSet pod names to
   their workload (table-driven test).
3. `render.D2` includes a node's `Note` in its label when set, omits it otherwise.
4. `kgraph traffic <ns> --throughput` against the homelab annotates nodes with
   rx/tx and prints a throughput table.
5. With `--prom` pointing nowhere / metrics absent, the command warns and still
   renders the traffic diagram (non-fatal).
6. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Offline: `parseVector` fixture test; `WorkloadFromPod` table test;
  `render.D2` note test.
- Manual: `kgraph traffic pihole --throughput` (port-forwards Prometheus,
  pihole ≈127 B/s rx confirmed during the spike).

## 9. Rollout / docs

CHANGELOG, README "Traffic" section (`--throughput`), `docs/SPEC.md` §4, memory,
and a `doctor` note already exists. Spec status → done after verification.

## 10. Open questions

- Add the `hubble-metrics` ServiceMonitor path later to unlock per-edge rates
  (separate spec). Window `[5m]` fixed for now.
