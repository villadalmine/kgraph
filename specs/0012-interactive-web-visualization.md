# Spec 0012 — Interactive web visualization

- **Status:** done (verified 2026-06-20: `/api/graph` JSON with palette colours;
  monitoring graph in **1.6s** vs 69s for SVG [no dagre]; `/static/cytoscape.min.js`
  200; index references only the local lib [offline]; traffic JSON carries flow
  edges with weight/verdict for animation; SVG/D2 export still 200; TestGraphJSON
  green. Client-side interactivity verified by data contract + Cytoscape lib.)
- **Feature ID:** F16 evolution (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

The web UI renders a **static, server-laid-out SVG** (D2/dagre) that the browser
only pans/zooms — wasting the medium and paying the slow dagre layout per render
(monitoring ≈ 69s). The web should be a real **interactive** visualization:
draggable nodes, hover detail, neighbour highlight, and — the headline — **animated
traffic** on edges. SVG/D2 stay as **export** (already buttons from spec 0006).

User decisions (2026-06-20):
- **Vendored JS lib, no build step** (Cytoscape.js embedded via `go:embed`) —
  keeps the single offline binary; no Node/TS toolchain (honours constitution §1).
- **Primary focus: animated traffic.**

## 2. Goals

- Server exposes the graph as **JSON** (`/api/graph`), separate from SVG render.
  This also makes views **fast** (no dagre layout server-side).
- Client renders interactively with Cytoscape.js: category-coloured nodes,
  draggable, hover tooltip (kind/namespace/labels/throughput), click to highlight
  neighbours, a layout selector.
- **Animated traffic**: flow edges animate (moving dashes), width ∝ weight,
  colour by verdict (allowed/dropped/error/gap). Works for `traffic` and
  `combined` (live via the existing periodic refresh).
- Keep SVG/D2 **export** buttons.

## 3. Non-goals

- TypeScript build pipeline (decided against — vendored JS).
- True particle sprites (moving-dash animation is the chosen, dependency-free
  effect).
- Replacing the SVG pipeline (still used for export and docs).

## 4. Constraints

§1 single self-contained **offline** binary: Cytoscape.js is vendored under
`internal/web/static/` and embedded; no CDN, no build. §3 agnostic.

## 5. Design

### Server (`internal/web`)
- `embed.FS` for `static/` (cytoscape.min.js); served at `/static/`.
- Refactor `buildDiagram` to first call a new `buildGraph(ctx, view, ns, layer,
  l7) (*graph.Graph, title, error)` (the build/overlay/subgraph logic, no
  render). `buildDiagram` = `buildGraph` + `emitDiagram` (unchanged SVG/D2 path).
- New `GET /api/graph?view=&ns=&layer=&l7=` → JSON
  `{title, nodes:[{id,kind,name,namespace,layer,color,stroke,alert,note,labels}],
  edges:[{id,source,target,kind,note,weight,dropped,error,gap}]}`. Node colours
  reuse the render palette via a new exported `render.CategoryColor(kind)`.
- Reuse cache for declarative (ns/cluster) graph JSON; traffic/combined uncached.

### Client (`internal/web/index.html` + embedded app)
- Load `/static/cytoscape.min.js`. Replace the static-SVG stage with a Cytoscape
  container; on render fetch `/api/graph`, build elements, run a layout
  (cose/breadthfirst/grid selectable).
- Style: node bg = `color`, red border when `alert`; label = name (+ note).
  Edges: ref/owner/selector dashed/solid per kind; **flow** edges coloured by
  verdict, width by `log(weight)`, and **animated** (requestAnimationFrame
  decrements `line-dash-offset`).
- Interactions: drag, wheel-zoom (native), hover tooltip, click highlights
  neighbours (fade others). Live traffic reuses periodic refetch of `/api/graph`.
- Keep SVG/D2 download buttons (call `/render?...&format=`).

## 6. Contracts / interfaces

- `GET /api/graph?view=ns|cluster|traffic|combined&ns=&layer=&l7=` → JSON above.
- `GET /static/*` serves embedded assets.
- `render.CategoryColor(kind string) (fill, stroke string)` exported.

## 7. Acceptance criteria

1. `/api/graph?view=ns&ns=<x>` returns JSON with non-empty nodes/edges and a
   `color` per node; `/static/cytoscape.min.js` serves 200.
2. `/api/graph` for a large namespace returns quickly (no dagre layout).
3. Browser: interactive render (drag/hover/highlight), layout selector, animated
   flow edges on the traffic/combined views, SVG/D2 export still work.
4. Offline: no external network needed to load the page (lib embedded).
5. `go build ./...` and `go test ./...` pass; a web test covers the graph JSON.

## 8. Test plan

- Unit: `graphJSON` conversion (nodes/edges/colour) offline; `render.CategoryColor`.
- Manual: `kgraph serve`, render ns/traffic/combined, drag/hover, watch animated
  flows, export SVG; load with devtools offline to confirm no CDN calls.

## 9. Rollout / docs

CHANGELOG, README Web UI section (interactive + animated traffic), memory.
`internal/web/static/cytoscape.min.js` vendored (MIT). Spec → done.

## 10. Open questions

- Default layout? Start with `cose` (force-directed) for topology; allow switch.
