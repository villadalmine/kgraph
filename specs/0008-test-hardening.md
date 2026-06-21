# Spec 0008 — Test hardening

- **Status:** done (verified 2026-06-20: all 9 internal packages have passing
  tests; `go test ./...` green)
- **Feature ID:** quality (`docs/SPEC.md` §6)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

`traffic`, `web` and `cli` had no unit tests, and `render` had no stability
guard. Backlog: `docs/IMPROVEMENTS.md` → Testing/hardening.

## 2. Goals

- Offline unit tests (fixtures, no cluster/network) for the pure, high-value
  logic in `traffic`, `web`, `cli`, plus a determinism guard for `render`.

## 3. Non-goals

- End-to-end cluster tests; cobra command execution tests.

## 4. Constraints

Constitution §7 (offline-testable).

## 5. Design / coverage added

- `traffic`: `SuggestPolicies` (k8s+cilium from a synthetic flow graph),
  `selector.matches`, `parsePort`; plus the aggregator reconcile tests from 0005.
- `web`: render cache hit/expiry (`cached`), `emitDiagram` d2 format.
- `cli`: `defaultOut`, `loadDotEnv` (export/quotes/comments, real env wins,
  missing file no-op).
- `render`: `D2` determinism (guards against map-iteration nondeterminism).

## 6. Acceptance criteria

1. New tests pass offline. ✅
2. `traffic`, `web`, `cli` packages all have tests. ✅
3. `go build ./...` and `go test ./...` pass. ✅

## 7. Rollout / docs

CHANGELOG, `docs/IMPROVEMENTS.md` (tick), memory.
