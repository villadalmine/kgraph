# Spec 0006 — Web UI UX & performance

- **Status:** done (verified 2026-06-20: format=d2 → text/plain "direction:…";
  X-Kgraph-Nodes header present; monitoring render 69s→0.001s on cache hit; web
  cache unit test + emitDiagram d2 test green; build + go test green)
- **Feature ID:** F16 hardening (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

`kgraph serve` works but is bare: renders block with no feedback, there's no way
to save a diagram, repeated renders re-do the (slow) layout, and there's no nudge
toward `--layer` on big namespaces. Backlog: `docs/IMPROVEMENTS.md` UX + perf.

## 2. Goals

- **Render cache**: memoize declarative renders (ns/cluster) by
  `(view, ns, layer, format)` with a short TTL so repeat views are instant.
  Traffic is *not* cached (it's time-sensitive).
- **Loading feedback**: a spinner + disabled Render button while a render is in
  flight.
- **Downloads**: save the current diagram as **SVG** and as **D2** source.
- **Layer nudge**: when a namespace render is large and no layer is selected,
  hint the user to pick a layer.

## 3. Non-goals

- Alternative layout engines (ELK) — separate perf item.
- Persisting the cache across restarts.

## 4. Constraints

Constitution §1 (single binary), §5 (graceful). Cache must be concurrency-safe.

## 5. Design

### Server (`internal/web/server.go`)
- Refactor `buildSVG` → `buildDiagram(ctx, view, ns, layer, l7, format)
  ([]byte, contentType string, nodes int, err error)`; `format` ∈ {svg,d2}.
  For `d2` return the D2 source (`text/plain`); else the compiled SVG.
- `/render` reads `format` (default svg), sets `Content-Type` accordingly and an
  `X-Kgraph-Nodes` header with the graph node count.
- A `renderCache` (map + `sync.Mutex`, entries `{body, ctype, nodes, at}`,
  `cacheTTL = 60s`) wraps `buildDiagram` for non-traffic views. Key:
  `view|ns|layer|l7|format`.

### Client (`internal/web/index.html`)
- Spinner overlay on `#stage`; disable `#render` and show it during fetch.
- **SVG** download: serialize the SVG already in the DOM (no server round-trip).
- **D2** download: fetch `/render?…&format=d2` and save as `<name>.d2`.
- After a successful render, read `X-Kgraph-Nodes`; if `view==ns`, no layer, and
  nodes are above a threshold (~150), append a non-error hint to the status.

## 6. Contracts / interfaces

- `/render` gains `&format=svg|d2` and an `X-Kgraph-Nodes` response header.
- No change to existing default behaviour (format defaults to svg).

## 7. Acceptance criteria

1. `/render?...&format=d2` returns `text/plain` D2 that starts with `direction:`.
2. `/render` responses carry an `X-Kgraph-Nodes` header with a positive integer.
3. A second identical non-traffic `/render` within the TTL is served from cache
   (verified by a unit test on the cache wrapper, or by timing).
4. Traffic renders are never cached.
5. UI: Render shows a spinner and is disabled during a render; SVG and D2
   download buttons produce the current diagram; a layer hint appears for a large
   namespace with no layer selected.
6. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Unit: `internal/web` test for the cache (hit/expiry) and `buildDiagram` d2
  format using a fake/over-the-wire-free path where possible; `format=d2` shape.
- Manual: `kgraph serve`, render a ns twice (2nd instant), download SVG/D2, see
  spinner, see the layer hint on `monitoring`.

## 9. Rollout / docs

CHANGELOG, README Web UI section, `docs/IMPROVEMENTS.md` (tick items), memory.

## 10. Open questions

- TTL/threshold as flags? Deferred — 60s / 150 nodes are sensible defaults.
