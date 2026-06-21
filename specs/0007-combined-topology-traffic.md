# Spec 0007 — Combined topology + traffic view

- **Status:** done (verified 2026-06-20: `ns pihole --traffic` → 21 objects +
  398 flows overlaid = 12 nodes/16 edges; web `view=combined` → valid SVG, 12
  nodes; graph.TestOverlay green; build + go test green)
- **Feature ID:** F1+F11 (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

Today the *declarative* topology (`ns`) and the *observed* traffic (`traffic`)
are separate diagrams. Reading them together — "this is what's deployed, and
this is what actually talks" — means flipping between two pictures. Overlaying
observed flows onto the topology in one diagram is far more insightful.
Backlog: `docs/IMPROVEMENTS.md` → "Combined topology + traffic view".

## 2. Goals

- `kgraph ns <ns> --traffic` overlays observed Hubble flow edges onto the
  namespace topology in a single diagram.
- A web view that does the same.
- Reuse existing pieces: `graph.Build/Prune`, `traffic.FetchFlows`, the flow edge
  styling already in `render`.

## 3. Non-goals

- L7 / policy overlays in the combined view (use `traffic` for those).
- Caching the combined view (it needs live flows).

## 4. Constraints

§3 agnostic, §5 graceful (Hubble missing → clear error). Matching flows to
topology nodes must be naming-agnostic.

## 5. Design

New pure helper `graph.Overlay(base, flows *Graph)`:

- Index `base` nodes by `(namespace, name)`, preferring a workload-ish kind
  (Deployment/StatefulSet/DaemonSet/…/Pod) over others (so a flow for "web"
  attaches to Deployment `web`, not Service `web`).
- For each flow edge, resolve both endpoints to a base node via the index; if an
  endpoint has no match (e.g. an external/world endpoint), add it to `base`.
  Then add the flow edge (weight/dropped/error/note preserved) to `base`.

CLI: `kgraph ns --traffic [--last N]` — after building the (optionally
layer-scoped) topology, `RequireHubble` + port-forward + `FetchFlows`, then
`graph.Overlay`, then render. `emit` gains an optional `overlay` hook.

Web: a new `combined` view in `buildDiagram` (topology + overlay, not cached),
plus a UI option.

## 6. Contracts / interfaces

- `graph.Overlay(base, flows *Graph)`.
- `kgraph ns` flags: `--traffic` (bool), `--last` (int, default 2000).
- Web `/render?view=combined&ns=&layer=` (not cached).

## 7. Acceptance criteria

1. `graph.Overlay` adds flow edges between matching topology nodes and pulls in
   unmatched (external) flow endpoints — offline unit test.
2. A flow for a workload name attaches to its controller node, not a same-named
   Service (kind preference) — unit test.
3. `kgraph ns <ns> --traffic` renders one diagram containing both ownership/ref
   edges and weighted flow edges (manual).
4. Web `combined` view renders topology with overlaid flows (manual).
5. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Unit: `graph.Overlay` (merge + external add + kind preference).
- Manual: `kgraph ns pihole --traffic`; web combined view.

## 9. Rollout / docs

CHANGELOG, README, IMPROVEMENTS (tick), memory. Spec → done.

## 10. Open questions

- Distinguish flow edges visually from ref edges when both connect the same
  pair? They already differ by colour/weight; revisit if cluttered.
