# Improvement backlog

Running list of what we've learned and what to improve, ordered roughly by
value/effort. Promote an item to a `specs/NNNN-*.md` before building it.

## Correctness / known artifacts

- ~~**Pod-vs-Workload duplicate node.**~~ Fixed (spec 0005), incl. StatefulSet
  ordinal pods (spec 0011).
- ~~**TLS Hubble Relay not supported.**~~ Done (spec 0009 TLS; spec 0011 adds
  mTLS via `--relay-cert/--relay-key`, `--relay-mtls[-secret]` from a TLS secret).
- ~~**Prometheus port assumed 9090.**~~ Done (spec 0009): read from pod spec.

## UX

- ~~**Web loading feedback.**~~ Done (spec 0006): spinner + disabled Render.
- ~~**Combined topology + traffic view.**~~ Done (spec 0007): `ns --traffic` +
  web `topology+traffic`, via `graph.Overlay`.
- ~~**Layer auto-suggest.**~~ Done (spec 0006): hint via `X-Kgraph-Nodes`.
- ~~**Download buttons in the web UI** (SVG / D2).~~ Done (spec 0006). Markdown
  download still possible later.

## Performance

- ~~**Render cache.**~~ Done (spec 0006): 60s TTL in `serve`.
- **dagre layout slow past ~200 nodes** (affects CLI SVG and the SVG export).
  Largely sidestepped in the web UI by the interactive client layout (spec 0012,
  no server dagre). For CLI/large exports, an ELK-style engine is still open —
  **deferred**: the embedded `d2lib` only ships dagre/ELK via its own pipeline and
  swapping layout engines is a research item; `--layer`/`--format d2` mitigate.

## Security

- **Optional web auth (OIDC / GitHub)** for exposed `serve` — design ready in
  `specs/0015-web-oidc-auth.md` (draft; not built). Default stays open; in-cluster
  `kubectl port-forward` RBAC is a zero-config secure model. Phase 2: per-user
  Kubernetes impersonation.

## Web UI follow-ups

- **Markdown export** in the web UI (the `doc` page) — not yet wired.
- **HTTP-handler/SSE tests** for `web` (have cache/emitDiagram/graphJSON tests).
- Vendored `internal/web/static/cytoscape.min.js` (MIT) — keep version current.

## Observability sources

- **Prometheus rate source (F18).** Use PromQL (e.g. Cilium/Hubble metrics, or
  `container_network_*`) for edge weights as bytes/sec, independent of Hubble
  flow sampling. Spec: `specs/0002-prometheus-rates.md`.
- **Live streaming (F17).** Done as periodic re-render over SSE; a future step is
  true incremental flow events with client-side graph diffing.

## Testing / hardening

- ~~Unit tests for `traffic` (aggregation, policy matching, suggest output),
  `web` (cache, emitDiagram), and `cli` (defaultOut, loadDotEnv).~~ Done
  (spec 0008). Still open: web HTTP-handler/SSE tests.
- ~~A stability test for `render.D2` output.~~ Done (spec 0008,
  `TestD2Deterministic`).

## Distribution

- ~~`goreleaser` config, GitHub Actions CI, container image.~~ Done (spec 0010) —
  config files added (`.github/workflows/`, `.goreleaser.yaml`, `Dockerfile`).
  Repo is still greenfield (not under git); `git init` + first commit + enabling
  Actions pending the user's go-ahead.
- ~~ghcr.io image publish wiring.~~ Done (spec 0013): `.github/workflows/docker.yml`
  builds a multi-arch image and pushes to ghcr.io on tag.

## Deferred (with rationale)

- **Per-edge Prometheus rates (F18 extension).** Would weight pod→pod edges from
  `hubble_flows_processed_total`. Deferred: this cluster doesn't scrape Hubble
  flow metrics (no `hubble-metrics` ServiceMonitor → unverifiable here), and the
  label schema varies by config. Per-workload throughput (`--throughput`) already
  ships. Revisit once the ServiceMonitor exists.
- **ELK / alternative layout** for big CLI/SVG exports — research item; the
  interactive web view already avoids server layout, and `--layer`/`--format d2`
  mitigate the CLI case.
- **Markdown export in the web UI** — low value now: the interactive UI plus the
  CLI `kgraph doc` already cover docs-as-code; the web `doc` page would need
  multi-file (md+SVGs) packaging. Revisit if requested.
- **Incremental live streaming** (client-side flow diffing) — the interactive
  Live mode (poll `/api/graph` + animation) covers the need; true per-flow
  events are a future optimisation.
