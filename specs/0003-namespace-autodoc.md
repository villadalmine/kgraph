# Spec 0003 — Opinionated namespace auto-documentation

- **Status:** done (verified 2026-06-20: `doc monitoring` → architecture +
  tiered cilium/gateway/monitoring diagrams; `doc pihole` → single-resource
  cilium/gateway folded into overview; all unit tests + build green)
- **Feature ID:** F7+ (enhances `kgraph doc`, `docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

`kgraph doc <ns>` already auto-detects stacks and emits one diagram per layer +
an inventory. But the output is **flat and unordered**: layers come out by raw
count, there's no big-picture architecture diagram when layers exist, and tiny
single-node layers each get their own near-empty diagram. A good auto-doc should
make *editorial decisions*: lead with the architecture, present layers
**top-down by abstraction altitude**, and not waste a diagram on trivia.

The user delegated those editorial decisions to kgraph ("decidí vos cuál sería lo
mejor"). This spec records them.

## 2. Goals

- Lead every namespace doc with an **Architecture overview** diagram (the whole
  pruned namespace) regardless of detected layers.
- Order layer sections by a curated **significance tier** (altitude), then by
  size — not alphabetically or by raw count alone.
- **Curate**: only layers with enough substance get a dedicated diagram; smaller
  detected layers are still acknowledged in the summary table (nothing hidden),
  but don't each spawn a one-box diagram.
- Stay deterministic and offline-testable; AI overview remains optional.

## 3. Non-goals

- Observed traffic / security posture in the doc (Hubble) — future spec 0004.
- A separate `autodoc` command. We **enhance `doc`**; these become its defaults.
- Multi-namespace / whole-cluster doc rollups.

## 4. Constraints

Constitution §2 (read-only), §3 (dynamic, works with unknown CRDs — unlisted
layers fall to the lowest tier, never dropped), §7 (offline-testable).

## 5. Design — editorial decisions (the "best options")

**D1. Architecture first.** Render the full pruned namespace graph as
`<ns>-architecture.svg` and place it under `## Architecture` before the layer
breakdown. It's the map; layers are the zoom-ins.

**D2. Significance tiers.** Add `Tier int` to `layers.Rule`. Lower tier = higher
abstraction = shown first. Assigned in the Builtin catalog:

| Tier | Theme | Layers |
|---|---|---|
| 1 | Control / GitOps / cluster lifecycle | argocd, argo-workflows, crossplane, capi |
| 2 | Platform: networking, ingress, PKI | cilium, gateway, cert-manager |
| 3 | Observability | monitoring |
| 4 | Storage / virtualization | longhorn, kubevirt |
| 5 | Apps / agents | kagent, holmesgpt |
| 9 | Anything unlisted (unknown CRD stacks) | (default) |

Rationale: a reader wants the "what governs this namespace" story first
(GitOps/control), then the platform it runs on, then observability, then
storage/runtime, then the apps themselves.

**D3. Ordering.** New `layers.Rank([]Detected) []Detected` sorts by
`(Tier asc, Count desc, Name asc)`. `Detect` keeps its existing count-desc
contract (used by the web layer picker); `Rank` is the doc's ordering.

**D4. Curation threshold.** A layer earns a dedicated `## section` + diagram only
if it has **≥ 2 of its own resources** (`Detected.Count`, `minLayerResources`).
A single-resource layer's 1-hop subgraph almost always pulls in neighbours, so
thresholding on subgraph node count never folds anything — keying on the layer's
own resource count (the value shown in the table's "Resources" column) is the
intuitive, effective rule. Below the threshold the layer still appears in the
Layers summary table (coverage stays complete) but with no diagram; a note
explains those small layers live inside the Architecture overview.

## 6. Contracts / interfaces

- `layers.Rule` gains `Tier int`; `layers.Detected` gains `Tier int`.
- New `func layers.Rank(d []Detected) []Detected`.
- `docs.Page` gains `Architecture *Section` (nil → not rendered, back-compat).
- `docs.Markdown` renders `## Architecture` (if set) before `## Layers`, and
  skips the per-section block for sections with an empty `Image`.
- `kgraph doc` behavior changes (no new flags): architecture diagram + ranked,
  curated layers. Existing flags (`--all`, `--icons`, `--ai`, `--model`,
  `-o`) unchanged.

## 7. Acceptance criteria

1. `layers.Rank` orders a mixed set as tier-then-count (e.g. `argocd` before
   `monitoring` even when monitoring has more resources).
2. Unknown/unlisted layer gets tier 9 (sorts last) and is never dropped.
3. `docs.Markdown` with `Architecture` set emits an `## Architecture` section
   with its image before `## Layers`.
4. A `Section` with empty `Image` appears in the Layers table but produces no
   `## <name>` diagram block.
5. `kgraph doc <ns>` writes `<ns>-architecture.svg` plus diagrams only for layers
   with ≥2 own resources; the Markdown lists all detected layers in tiered order.
6. `go build ./...` and `go test ./...` pass.

## 8. Test plan

- Unit: `layers.Rank` ordering + tier default (offline).
- Unit: `docs.Markdown` Architecture rendering + empty-image section skipping.
- Manual: `kgraph doc monitoring` and `kgraph doc argocd`, inspect ordering,
  architecture diagram, and that tiny layers have no standalone diagram.

## 9. Rollout / docs

CHANGELOG (Unreleased → Added), README `doc` section, `docs/SPEC.md` §4 note,
memory update. Spec status → done after verification.

## 10. Open questions

- Should `minLayerNodes` be a flag? Deferred — 2 is a sensible fixed default.
