# Publishing kgraph to GitHub

The Go module is `github.com/villadalmine/kgraph` and its **root is this
directory** (`graph-k8s-all/`, where `go.mod` and `.github/` live). So the GitHub
repo's root should be the contents of this directory.

> Run these yourself — they create/push a public repo and trigger CI. Nothing
> here is done automatically for you.

## 0. Pre-flight (safety)

```bash
cd graph-k8s-all
gh auth status                 # confirm you're logged in to GitHub
git init -b main
git add -A
git status                     # MUST NOT list .env  (it's gitignored — verify!)
```

`.gitignore` already excludes `.env`, the `kgraph` binary, generated `*.svg`/`*.d2`,
`kgraph-docs/` and `demo-out/`. The vendored `internal/web/static/cytoscape.min.js`
**is** committed (it's embedded at build time — keep it).

> Security: rotate the OpenRouter API key if it was ever pasted into a chat or
> shared. It is not in the repo (only in the gitignored `.env`), but treat any
> shared key as compromised.

## 1. First commit

```bash
git commit -m "Initial commit: kgraph — Kubernetes layer diagrams, docs & traffic"
```

## 2. Create the repo and push (one command)

```bash
gh repo create villadalmine/kgraph \
  --public \
  --source=. \
  --remote=origin \
  --description "Turn a Kubernetes cluster into layer-scoped diagrams, docs-as-code, observed-traffic maps and an interactive web UI." \
  --push
```

This creates `github.com/villadalmine/kgraph`, sets `origin`, and pushes `main`.
The **CI** workflow (`.github/workflows/ci.yml`) runs on the push: build + vet +
`go test ./...`.

## 3. (Optional) Cut a release + container image

Tagging `v*` triggers the **release** (goreleaser binaries) and **container
image** (multi-arch → ghcr.io) workflows:

```bash
git tag v0.1.0
git push origin v0.1.0
gh run watch          # follow the workflow runs
gh release view v0.1.0
```

The image is published to `ghcr.io/villadalmine/kgraph`. By default ghcr packages
are private — make it public once if you want anonymous pulls:
GitHub → your profile → Packages → `kgraph` → Package settings → Change visibility.

```bash
docker pull ghcr.io/villadalmine/kgraph:0.1.0
```

## 4. Day-to-day

```bash
git switch -c my-change      # work on a branch
# ... edits ...
go test ./... && git commit -am "…"
git push -u origin my-change
gh pr create --fill          # open a PR; CI runs on it
```

## Notes

- The release draft is created as a **draft** (`.goreleaser.yaml` `release.draft:
  true`) so you can review before publishing.
- If `go test ./...` fails in CI but passes locally, check the Go version: CI uses
  `go-version-file: go.mod`.
- To publish under a different owner/name, update the module path in `go.mod`
  (and imports) and the image name in `.github/workflows/docker.yml`.
