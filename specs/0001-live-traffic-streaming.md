# Spec 0001 — Live traffic streaming in the web UI

- **Status:** done (verified 2026-06-20: 9 SSE frames over 28s against `pihole`,
  each decoding to valid SVG, single port-forward, clean teardown)
- **Feature ID:** F17 (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

The `traffic` view in `kgraph serve` is a one-shot render: it port-forwards the
Hubble Relay, samples flows, renders once, and tears the forward down. To watch
traffic *evolve* you have to keep clicking Render, and each click pays the
port-forward setup cost again. We want a genuinely **live** view.

Backlog: `docs/IMPROVEMENTS.md` → "Live streaming (F17)".

## 2. Goals

- A live traffic view that auto-refreshes without user clicks.
- Keep **one** Hubble Relay port-forward open for the life of the stream
  (don't re-forward every tick).
- Reuse the existing flow→graph→D2→SVG pipeline (no client-side graph engine).
- Clean teardown when the client disconnects or toggles Live off.

## 3. Non-goals

- True per-flow incremental events / client-side graph diffing (future step).
- L7 streaming specifics beyond what `FetchFlows(..., l7)` already returns.
- Auth on the stream (server is local-bind by default).

## 4. Constraints

- Constitution §1 (single binary), §4 (RBAC-tolerant), §5 (out-of-the-box:
  still calls `RequireHubble` and surfaces actionable errors).
- Must not block the server: one goroutine per stream, bounded by client ctx.

## 5. Design

Server-Sent Events (SSE), because it's one-way server→browser, survives proxies,
and needs no extra deps.

- New route `GET /api/traffic/stream?ns=<ns>&l7=0|1` in `internal/web/server.go`.
- Handler `handleTrafficStream`:
  1. `RequireHubble`, then `PortForwardRelay` **once**; `defer stop()`.
  2. Set SSE headers; flush.
  3. Loop on a `time.Ticker` (default 3s) until `r.Context().Done()`:
     - `traffic.FetchFlows(ctx, addr, ns, last, l7)`,
     - `render.SVG(render.D2(g, "Traffic: "+ns, false))`,
     - send as an SSE `data:` frame. SVG is multi-line, so **base64-encode** it
       into a single `data:` line (avoids SSE line-framing issues).
     - emit an immediate first frame before the first tick so the UI fills fast.
  4. On error, send an SSE `event: error` frame with the message and continue
     (transient Hubble/relay hiccups shouldn't kill the stream).
- Tunables: `last=2000` flows, `interval=3s` (constants for now).

Client (`internal/web/index.html`):
- Add a **Live** toggle (checkbox), shown only for the `traffic` view next to L7.
- When checked: open `EventSource('/api/traffic/stream?ns=&l7=')`; on each
  message, `atob` the payload and inject into `#stage`, preserving the current
  pan/zoom transform (don't `resetView` on refresh).
- When unchecked / view change / ns change: `close()` the EventSource.
- Status line shows "live ●" with the last update time; errors show in red.

## 6. Contracts / interfaces

- HTTP: `GET /api/traffic/stream?ns=<string>&l7=<0|1>` →
  `text/event-stream`. Frames:
  - `data: <base64(svg)>\n\n`
  - `event: error\ndata: <message>\n\n`
- No new Go exported types required; reuses `traffic.PortForwardRelay`,
  `traffic.FetchFlows`, `preflight.RequireHubble`, `render.D2/SVG`.

## 7. Acceptance criteria

1. `GET /api/traffic/stream?ns=pihole` returns `Content-Type: text/event-stream`
   and emits at least one `data:` frame whose base64 decodes to a `<svg…>`.
2. Subsequent frames arrive roughly every ~3s while the request is open.
3. The relay is port-forwarded once per stream (one `PortForwardRelay` call),
   and `stop()` runs when the client disconnects.
4. In the browser, toggling **Live** on the traffic view starts auto-refresh and
   toggling it off (or changing view/ns) stops it; pan/zoom is preserved across
   refreshes.
5. If Hubble is unavailable, the client receives an `event: error` frame with the
   `RequireHubble` remediation text (not a silent failure).
6. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Manual: `kgraph serve`, open UI, traffic view on `pihole`, toggle Live, observe
  periodic updates; `curl -N http://127.0.0.1:8080/api/traffic/stream?ns=pihole`
  shows repeating `data:` frames.
- Build/test gates green.

## 9. Rollout / docs

- CHANGELOG "Unreleased → Added".
- README "Web UI" section: mention the Live toggle.
- Memory `kgraph-project.md`: note F17 done + SSE approach.

## 10. Open questions

- Make `interval`/`last` query params or `serve` flags? (Deferred; constants ok.)
