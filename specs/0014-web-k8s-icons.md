# Spec 0014 — Kubernetes resource icons in the web view

- **Status:** done (verified 2026-06-20: /api/graph nodes carry correct `icon`
  per kind [Deployment→deploy, CiliumEndpoint→crd], /static/icons/*.svg serve
  200, TestIconName green, build+test green; client toggle styles nodes with
  embedded SVG icons, offline.)
- **Feature ID:** F16 polish (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

In the interactive web view every node is a coloured circle — you can't tell a
Pod from a Service from a ConfigMap at a glance. The official **Kubernetes
community icon set** has a recognisable glyph per resource. kgraph already
references them by URL in the SVG (`--icons`); the web view should show them too,
**vendored & embedded** so it stays offline / single-binary.

## 2. Goals

- Each node shows its Kubernetes resource icon (Pod, Deployment, Service,
  ConfigMap, …); unknown CRDs get a generic CRD icon; external endpoints keep a
  plain marker.
- Icons are embedded (no CDN); one source of truth for the kind→icon mapping,
  shared with the existing SVG renderer.
- A UI toggle to switch icons on/off (default on).

## 3. Non-goals

- Per-CRD custom icons; labelled icon variants.

## 4. Constraints

§1 offline single binary: SVGs vendored under `internal/web/static/icons/`,
served from the existing embedded `/static/`. §3 agnostic.

## 5. Design

- Refactor `render`: extract `IconName(kind) string` (base name: `pod`, `deploy`,
  …, `crd` fallback; "" for `External`). `iconFor` (SVG URL path) reuses it.
- `/api/graph` node JSON gains `icon` = `IconName(kind)`.
- Vendored icons: `internal/web/static/icons/<name>.svg` (Kubernetes community
  set, CC-BY/Apache per upstream).
- Client: when icons enabled and a node has `icon`, style it with
  `background-image: /static/icons/<icon>.svg`, white fill, `background-fit:
  contain`, larger node, border = category stroke. Else colored circle.
  Toggle `Icons` (default on) re-styles without refetching.

## 6. Contracts / interfaces

- `render.IconName(kind string) string` (exported).
- `/api/graph` nodes gain `"icon"`.
- `/static/icons/*.svg` served (already via embedded `/static/`).

## 7. Acceptance criteria

1. `/api/graph` nodes include an `icon` (e.g. Deployment→`deploy`, a CRD→`crd`,
   External→""). 
2. `/static/icons/pod.svg` serves 200.
3. `render.IconName` is the single mapping; `iconFor` (SVG) uses it (unchanged
   output).
4. Browser: nodes render with K8s icons; toggle hides/shows them.
5. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Unit: `render.IconName` mapping (known kind, CRD fallback, External "").
  `graphJSON` includes icon.
- Manual: web view shows icons; toggle works; offline (no external calls).

## 9. Rollout / docs

CHANGELOG, README (icons in web), memory. Spec → done.
