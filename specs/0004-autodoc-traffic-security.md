# Spec 0004 — Observed traffic & security posture in the namespace doc

- **Status:** done (verified 2026-06-20: `doc pihole --traffic` added Observed
  traffic [344 flows, 4 nodes] + Security posture [0 policies, 219 gap flows,
  pihole unprotected]; `doc pihole` without the flag unchanged, no port-forward;
  build + unit tests green)
- **Feature ID:** F7+ (extends `kgraph doc`; builds on F11/F13)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

Spec 0003 made `kgraph doc` a strong *declarative* document (architecture +
tiered layers + inventory). But a namespace's real behaviour — who actually
talks to whom, and whether that traffic is governed by policy — lives in the
**observed** world (Hubble) and the **security overlay** (NetworkPolicy /
CiliumNetworkPolicy). Those already exist in `kgraph traffic`; this spec folds
an opportunistic summary of them into the doc so a single page tells the whole
story: *what is declared, what actually flows, and how exposed it is.*

## 2. Goals

- Optional **Observed traffic** section in the doc: a Hubble traffic diagram for
  the namespace.
- Optional **Security posture** section: policy coverage summary (policies found,
  denied flows, gap flows, unprotected workloads) from the existing analyzer.
- Strictly **opt-in and graceful**: enabled by a flag; if Hubble isn't available
  or no flows are seen, the doc still generates with an explanatory note instead
  of failing.

## 3. Non-goals

- L7 detail or `--suggest-policy` output in the doc (those stay on `traffic`).
- Streaming / live anything (docs are a static snapshot).
- Making traffic the default for `doc` (cost + Hubble dependency → opt-in).

## 4. Constraints

Constitution §2 (read-only — analyzer only reads policies), §5 (out-of-the-box:
auto port-forward + actionable note when Hubble missing, never crash), §7
(docs package stays decoupled from `traffic` — map types at the CLI boundary).

## 5. Design

`kgraph doc` gains `--traffic` (bool, default false). When set, after the layer
sections are built:

1. `preflight.RequireHubble(ctx, c)`. On error: log a warning, set
   `page.TrafficNote` to the remediation text, skip the rest (doc still written).
2. `traffic.PortForwardRelay` (defer stop) → `traffic.FetchFlows(ctx, addr, ns,
   docTrafficFlows=2000, false)`.
   - If no flow nodes: set `page.TrafficNote` ("no flows observed…"), skip.
   - Else render `<ns>-traffic.svg` and set `page.Traffic` (a `docs.Section`).
3. `traffic.AnalyzePolicies(ctx, c.Dynamic(), ns, flowGraph)` → `Summary`; map it
   into `page.Security` (a docs-local struct, to keep `docs` free of a `traffic`
   import).

Markdown order: Overview(AI) → Architecture → Layers → **Observed traffic** →
**Security posture** → Inventory.

## 6. Contracts / interfaces

- `docs.Page` gains:
  - `Traffic *Section`
  - `TrafficNote string`
  - `Security *Security` where
    `type Security struct { Policies, DeniedFlows, GapFlows int; Unprotected []string }`.
- `docs.Markdown` renders `## Observed traffic` (image or note) and
  `## Security posture` (counts + unprotected list, or "all workloads covered").
- CLI: new `--traffic` flag on `doc`. Reuses `preflight`, `traffic`. Constant
  `docTrafficFlows = 2000`.

## 7. Acceptance criteria

1. `docs.Markdown` with `Traffic` set renders `## Observed traffic` + its image.
2. `docs.Markdown` with `TrafficNote` (and nil `Traffic`) renders the note, no
   broken image.
3. `docs.Markdown` with `Security` renders the counts; an empty `Unprotected`
   shows "all workloads covered", a non-empty one lists the workloads.
4. `kgraph doc <ns>` (no `--traffic`) is unchanged (no traffic/security sections,
   no port-forward).
5. `kgraph doc <ns> --traffic` on a Hubble cluster adds both sections; on a
   cluster without Hubble it still writes the doc with the note (non-fatal).
6. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Unit (offline): `docs.Markdown` for Traffic / TrafficNote / Security variants.
- Manual: `kgraph doc pihole --traffic` (has DNS-to-world flows) → traffic +
  security sections; `kgraph doc <ns> --traffic` where Hubble disabled → note.

## 9. Rollout / docs

CHANGELOG (Unreleased → Added), README "Docs-as-code" section (the `--traffic`
flag), `docs/SPEC.md` §4, memory. Spec status → done after verification.

## 10. Open questions

- Expose `--last` for the doc's flow sample? Deferred — fixed 2000 is fine.
