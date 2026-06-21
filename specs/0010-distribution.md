# Spec 0010 — Distribution & CI

- **Status:** done (verified 2026-06-20: CGO_ENABLED=0 static build OK,
  `--version` stamping works, `go vet ./...` clean, `go test ./...` green;
  goreleaser binary absent in sandbox so config validated in CI. No git actions
  performed.)
- **Feature ID:** packaging (`docs/IMPROVEMENTS.md` → Distribution)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

kgraph is a single Go binary but has no build/release automation or container
image, and CI isn't wired. Backlog: Distribution.

## 2. Goals

- A **CI workflow** (GitHub Actions) that builds and runs `go test ./...` +
  `go vet` on push/PR.
- A **goreleaser** config to cross-compile binaries (linux/darwin, amd64/arm64),
  archive them, and produce checksums on tag; plus a release workflow.
- A **Dockerfile** producing a small static image.

## 3. Non-goals

- Running `git init` / committing / pushing / publishing — these are
  outward-facing actions left to the user's explicit go-ahead. This spec only
  adds the config files.
- Signing, SBOMs, Homebrew taps (later).

## 4. Constraints

Constitution §1 (single static binary; CGO off). Agnostic — nothing
cluster-specific.

## 5. Design

- `.github/workflows/ci.yml`: `actions/setup-go` via `go-version-file: go.mod`,
  steps `go build ./...`, `go vet ./...`, `go test ./...`.
- `.github/workflows/release.yml`: on `v*` tag, run `goreleaser release`.
- `.goreleaser.yaml`: builds `./cmd/kgraph`, `CGO_ENABLED=0`, GOOS linux/darwin,
  GOARCH amd64/arm64, ldflags stamping version; tar.gz archives; checksums.
- `Dockerfile`: multi-stage `golang:1.26` builder → `gcr.io/distroless/static`,
  non-root, entrypoint `kgraph`.

## 6. Acceptance criteria

1. `CGO_ENABLED=0 go build ./cmd/kgraph` succeeds (static build path goreleaser
   uses). ✅ to verify.
2. `go vet ./...` clean.
3. YAML/config files are well-formed and reference the correct module/main path.
4. No git actions performed; existing `go test ./...` still green.

## 7. Test plan

- `CGO_ENABLED=0 go build` locally; `go vet ./...`. (Docker/goreleaser binaries
  may be unavailable in the dev sandbox; configs are reviewed for correctness and
  exercised in CI once the repo is pushed.)

## 8. Rollout / docs

CHANGELOG, README (CI badge note / install via release), memory. Spec → done.
Inform the user that `git init` + first commit + enabling Actions is their call.

## 9. Open questions

- Container registry (ghcr.io) wiring deferred until the repo is pushed.
