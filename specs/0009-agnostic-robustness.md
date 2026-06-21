# Spec 0009 — Agnostic robustness: Prometheus port & TLS Hubble Relay

- **Status:** done (verified 2026-06-20: throughput still finds Prometheus using
  the pod's declared port; non-TLS relay unchanged; TestTransportCreds +
  TestPrometheusPort green; build + go test green. TLS-relay live path
  unverifiable here — homelab relay is non-TLS — so build/unit-verified.)
- **Feature ID:** F11/F18 hardening (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

Two remaining cluster-specific assumptions ([[agnostic-design]]):

1. **Prometheus port assumed 9090.** Discovery (spec 0005) finds the pod
   anywhere, but hardcodes the container port.
2. **Hubble Relay assumed non-TLS.** `FetchFlows` dials with insecure
   credentials; clusters with a TLS-enabled relay can't be reached.

## 2. Goals

- Read the Prometheus port from the discovered pod's container spec (named
  `web`/`http`/`http-web`, else a 9090 port), falling back to 9090.
- Let `kgraph traffic` reach a **TLS** Hubble Relay via opt-in flags, without
  changing the default (insecure, port-forwarded) behaviour or other callers.

## 3. Non-goals

- Auto-fetching Hubble mTLS client certs from cluster secrets (future). The user
  supplies CA / server-name / skip-verify as needed.
- TLS for the auto-port-forwarded relay (a local forward is plain TCP).

## 4. Constraints

§3 agnostic, §5 graceful, backward compatible (existing call sites unchanged).

## 5. Design

### 5a. Prometheus port from pod spec
`findPod` returns the `*corev1.Pod` (not just ns/name). `PortForwardPrometheus`
inspects `pod.Spec.Containers[].Ports`: prefer a port named `web`/`http`/
`http-web`/`metrics`; else a `ContainerPort == 9090`; else fall back to 9090.

### 5b. TLS Hubble Relay (options pattern)
`FetchFlows(ctx, addr, ns, last, l7, opts ...Option)` — variadic, so existing
callers are untouched (default = insecure). `Option` configures a `dialConfig`:
`WithTLS(skipVerify bool, serverName, caFile string)`. When set, dial with
`credentials.NewTLS` (optional `RootCAs` from `caFile`, `ServerName`,
`InsecureSkipVerify`); otherwise insecure as today.

`kgraph traffic` flags: `--relay-tls`, `--relay-tls-skip-verify`,
`--relay-server-name`, `--relay-ca` (most useful together with `--relay-addr`
pointing at a real TLS relay).

## 6. Contracts / interfaces

- `traffic.FetchFlows(..., opts ...Option)`; `traffic.Option`,
  `traffic.WithTLS(skipVerify bool, serverName, caFile string) Option`.
- `traffic.PortForwardPrometheus` now uses the pod's actual port.
- New `traffic` CLI flags (above).

## 7. Acceptance criteria

1. `PortForwardPrometheus` connects using the pod's declared port (verified:
   Prometheus still found & queried on the homelab, where the port is named
   `web`/9090).
2. `FetchFlows` with no opts behaves exactly as before (insecure) — existing
   traffic verification still passes.
3. `FetchFlows` with `WithTLS(...)` builds TLS transport credentials (unit/build
   verified; CA file parse error surfaces clearly).
4. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Unit: `WithTLS` option sets the config; a bad `caFile` errors. Port-selection
  helper picked from a synthetic pod spec.
- Manual: `traffic pihole --throughput` still finds Prometheus (port from spec);
  non-TLS relay path unchanged.

## 9. Rollout / docs

CHANGELOG, README (TLS relay flags), `docs/IMPROVEMENTS.md` (tick), memory.
Spec → done. Note TLS-relay path is build/unit-verified (homelab relay is
non-TLS).

## 10. Open questions

- Auto-discover mTLS certs from `hubble-relay-client-certs`? Deferred.
