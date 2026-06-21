# Spec 0005 — Cluster-agnostic traffic & throughput

- **Status:** done (verified 2026-06-20: aggregator unit tests merge Pod+Workload
  / keep distinct; `traffic pihole --throughput` now 1 pihole node [was 2],
  4→3 nodes; Prometheus auto-found cluster-wide without hardcoded namespace;
  build + `go test ./...` green)
- **Feature ID:** F11/F18 hardening (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

kgraph must work on **any** cluster, not the author's homelab. Two recent things
break that promise:

1. **Pod-vs-Workload duplicate nodes.** When Hubble has workload info on *some*
   flows of a workload but not others, the same workload appears twice in the
   traffic graph: once as `<WorkloadKind>/<name>` and once as `Pod/<name>`
   (`internal/traffic/hubble.go` `classify`). Pure correctness bug — affects
   every cluster.
2. **Non-agnostic Prometheus discovery.** `traffic.PortForwardPrometheus`
   hardcodes namespace `monitoring` and kube-prometheus-stack selectors
   (`specs/0002`). Clusters using a different namespace (`prometheus`,
   `observability`, …) or chart won't be found.

Constitution §3 (dynamic, works anywhere) and §5 (out-of-the-box) demand both be
fixed.

## 2. Goals

- One node per workload in the traffic graph: reconcile `Pod/<name>` endpoints
  with `<WorkloadKind>/<name>` endpoints sharing the same `(namespace, name)`.
- Discover Prometheus **anywhere** in the cluster via standard, chart-neutral
  label selectors, regardless of namespace; keep `--prom` override.
- No hardcoded cluster-specific names. Purely heuristic/standard signals.

## 3. Non-goals

- Unifying the two pod→workload name heuristics (`hubble.rsHash` vs
  `metrics.WorkloadFromPod`) — separate cleanup.
- StatefulSet ordinal reconciliation when *no* flow carries workload info
  (best-effort; the common Deployment case is covered).

## 4. Constraints

Constitution §3 (agnostic/dynamic), §5 (graceful, actionable), §7 (offline tests).

## 5. Design

### 5a. Endpoint reconciliation (dedup)

In `aggregator.build()`, before emitting graph nodes:

- Group endpoints by `(ns, name)` (external endpoints, `ns==""`, keyed by name).
- Pick a **canonical** endpoint per group by kind preference:
  a concrete workload kind (anything other than `Pod`/`Workload`) > `Workload` >
  `Pod`. Merge label maps (union; canonical wins on conflict).
- Build `idMap: oldID → canonicalID`. Translate every edge's `from`/`to` through
  it, **summing** flow counts and merging port/L7 stats for edges that collapse.

This is naming-agnostic: it only relies on namespace + the workload name that
Hubble itself reports / the existing rsHash-stripped pod name.

### 5b. Agnostic Prometheus discovery

Refactor `findPod` to optionally search **all namespaces** (`namespace==""` →
`Pods("").List`) and to return the pod's actual namespace. `PortForwardPrometheus`
searches cluster-wide with chart-neutral selectors, in order:

- `app.kubernetes.io/name=prometheus`
- `app.kubernetes.io/component=prometheus`
- `app=prometheus`

First running pod wins; forward its container port `9090` (Prometheus default).
`findRelayPod` keeps targeting its known namespace explicitly (relay is a fixed
Cilium component) but goes through the same generalized `findPod`.

## 6. Contracts / interfaces

- `internal/traffic/hubble.go`: internal reconciliation only; `FetchFlows`
  signature unchanged. (Refactor `aggregator.build()`; add `reconcile()` helper.)
- `internal/traffic/portforward.go`: `findPod(ctx, cs, namespace, selectors…)
  → (ns, name, error)` (namespace "" = all). `PortForwardPrometheus` unchanged
  signature; now cluster-wide.

## 7. Acceptance criteria

1. Given flows where a workload appears both with and without Hubble workload
   info, the built graph has **one** node for it (kind = the workload kind), with
   merged labels, and edge weights summed — verified by an offline unit test on
   the aggregator.
2. External endpoints and genuinely distinct workloads are not merged.
3. `PortForwardPrometheus` finds Prometheus regardless of namespace (selectors
   are namespace-agnostic; `Pods("")` search) — verified against the homelab
   (Prometheus is in `monitoring`, found without hardcoding it).
4. `kgraph traffic <ns>` shows one node per workload (manual: `pihole` no longer
   doubled).
5. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Offline: new `internal/traffic` aggregator test feeding synthetic
  `flowpb.Flow`s (one with `Workloads`, one without, same pod) → assert single
  node + summed edges. Plus a non-merge case.
- Manual: `kgraph traffic pihole --throughput` → single `pihole` node;
  `--prom` omitted still finds Prometheus in `monitoring`.

## 9. Rollout / docs

CHANGELOG (Unreleased → Fixed/Changed), `docs/IMPROVEMENTS.md` (drop the
Pod-vs-Workload item), README note if needed, memory. Spec → done.

## 10. Open questions

- Should Prometheus port be auto-detected from the container spec instead of
  assuming 9090? Deferred — 9090 is the universal default.
