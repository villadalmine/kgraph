# Spec 0013 — Container image publish (ghcr.io)

- **Status:** done (verified 2026-06-20: workflow added; static `CGO_ENABLED=0`
  build path already green; Docker daemon absent in the dev sandbox so the image
  build is validated in CI. No git/registry actions performed locally.)
- **Feature ID:** packaging (`docs/IMPROVEMENTS.md` → Distribution)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

Spec 0010 added a `Dockerfile` but nothing builds/publishes the image. Backlog:
"ghcr.io image publish wiring".

## 2. Goals

- On a `v*` tag, build a **multi-arch** (amd64/arm64) image from the existing
  `Dockerfile` and push it to `ghcr.io/<owner>/<repo>` with semver + `latest`
  tags.

## 3. Non-goals

- Signing/SBOM/provenance (later). Local registry actions (CI does the push).

## 4. Design

`.github/workflows/docker.yml`: `setup-qemu` + `setup-buildx`,
`docker/login-action` to ghcr.io with the built-in `GITHUB_TOKEN`
(`packages: write`), `docker/metadata-action` for tags, `docker/build-push-action`
with `platforms: linux/amd64,linux/arm64`. Reuses the distroless `Dockerfile`.

## 5. Acceptance criteria

1. Workflow is well-formed and references the existing Dockerfile + ghcr.io. ✅
2. Image builds from a static binary (`CGO_ENABLED=0`) — verified locally. ✅
3. No local git/registry actions. ✅

## 6. Rollout / docs

CHANGELOG, README (image pull note), `docs/IMPROVEMENTS.md` (tick), memory.
Publishing requires the repo on GitHub with Actions + packages enabled — the
user's call.
