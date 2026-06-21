# kgraph — Specification

This is the living, top-level spec for **kgraph**. It is the source of truth for
*what the tool is and does*; the per-feature specs in `specs/` describe
*individual changes* before they are built (Spec-Driven Development — see
`.claude/skills/sdd/SKILL.md`).

## 1. Vision

Turn any Kubernetes cluster into clear, layer-scoped **diagrams**, committable
**docs-as-code**, **observed-traffic** maps with security analysis, and
**natural-language** explanations — from a single Go binary that connects via
kubeconfig and discovers everything dynamically (built-ins **and** CRDs).

Core principle: a real cluster is a **graph** (ownerRefs, selectors, refs,
flows), not a pile of YAML. kgraph builds a typed knowledge graph and renders
*slices* of it (by abstraction **layer**) so each diagram stays small and true.

## 2. Non-negotiable constraints (the "constitution")

1. **Single self-contained binary.** No external runtime deps to render
   (D2 embedded as a library, not the `d2` CLI). Heavy SDK deps stay *indirect*.
2. **Read-only by default.** kgraph observes and documents; it never mutates the
   cluster. (`--suggest-policy` only *prints* YAML.)
3. **Dynamic discovery, no hardcoded kinds.** Works with any CRD.
4. **RBAC-tolerant.** Skip what it can't read; report it; never crash.
5. **Out-of-the-box UX.** If a capability is missing, say exactly what to enable
   (`doctor`, `RequireHubble`). Auto port-forward instead of asking the user to.
6. **Secrets only from env / gitignored `.env`.** Never hardcode or commit keys.
7. **Offline-testable.** Unit tests use in-code fixtures; no cluster/network.
8. **BYO LLM.** AI is pluggable behind `llm.Provider`; never required for core.

## 3. Architecture

```
kubeconfig ─► collect ─► graph (build/prune/subgraph) ─► render (D2→SVG)
              client-go    typed nodes+edges               d2lib
              dynamic      owner/selector/ref/flow          │
              discovery                                     ├─► docs (Markdown)
                                                            ├─► llm  (GraphRAG)
              traffic ───► flows (Hubble gRPC) ────────────┤
              preflight ─► capability checks                └─► web  (serve + SPA)
```

Packages (`internal/`): `collect`, `graph`, `layers`, `render`, `docs`, `llm`,
`preflight`, `traffic`, `metrics`, `web`, `cli`.

## 4. Feature inventory (current capabilities)

| # | Capability | Command / API | Package(s) | Status |
|---|---|---|---|---|
| F1 | Namespace topology → SVG/D2 | `kgraph ns <ns>` | collect, graph, render | ✅ |
| F2 | Cluster-scoped topology | `kgraph cluster` | collect, graph, render | ✅ |
| F3 | Stack/layer detection | `kgraph layers <ns>` | layers | ✅ |
| F4 | Layer filtering | `--layer <name>` | layers, graph.Subgraph | ✅ |
| F5 | Noise pruning / `--all` | `kgraph ns --all` | graph.Prune | ✅ |
| F6 | Icons (external URLs) | `--icons` | render | ✅ |
| F7 | Docs-as-code (architecture-first, tiered, curated; `--traffic` adds observed traffic + security posture) | `kgraph doc <ns>` | docs, layers, traffic | ✅ |
| F8 | AI explain / ask | `kgraph explain`, `ask`, `doc --ai` | llm, graph.Describe | ✅ |
| F9 | `.env` auto-load | (startup) | cli.root | ✅ |
| F10 | Capability checks | `kgraph doctor` | preflight | ✅ |
| F11 | Observed traffic (Hubble) | `kgraph traffic <ns>` | traffic | ✅ |
| F12 | Auto relay port-forward | (implicit) | traffic.portforward | ✅ |
| F13 | Policy coverage overlay | `traffic --policy` | traffic.policy | ✅ |
| F14 | Policy generation | `traffic --suggest-policy k8s\|cilium` | traffic.suggest | ✅ |
| F15 | L7 detail | `traffic --l7` | traffic.hubble | ✅ |
| F16 | Web UI | `kgraph serve` | web | ✅ |
| F17 | **Live traffic streaming** | web `/api/traffic/stream` (SSE) | web, traffic | ✅ |
| F18 | Prometheus throughput rates (per-workload rx/tx) | `traffic --throughput` | metrics, traffic, render | ✅ |

## 5. Graph model (contract)

- **Node**: `ID, Group, Kind, Namespace, Name, Labels, Layer, Alert, Obj`.
- **Edge**: `From, To, Kind, Note, Weight, Dropped, Gap, Error`.
- **RelKind**: `owner` (solid), `selector` (green dashed), `ref` (amber dashed),
  `crd`, `flow` (weighted, blue allowed / red dropped / amber gap / red error).

## 6. Quality gates

- `go build ./cmd/kgraph` succeeds.
- `go test ./...` passes offline.
- New features: a spec in `specs/`, an entry in `CHANGELOG.md`, README updated,
  and memory (`kgraph-project.md`) updated.

## 7. Roadmap (see `docs/IMPROVEMENTS.md` for the full backlog)

Near-term: live traffic streaming (F17), Prometheus rates (F18), the
Pod-vs-Workload dedup fix, web loading feedback and tests for traffic/web/cli.
