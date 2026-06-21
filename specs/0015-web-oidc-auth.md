# Spec 0015 — Optional web authentication (OIDC / GitHub)

- **Status:** draft (design only — no implementation yet, by user decision
  2026-06-20)
- **Feature ID:** F16 security (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

`kgraph serve` is open: anyone who can reach the URL sees whatever the server's
kubeconfig can read. That's fine for local/dev use, but exposing the UI (ingress)
needs a login gate, the way Grafana/ArgoCD do it (Azure AD / generic OIDC,
GitHub). The default must stay **open**.

## 2. Access models (important context)

kgraph runs in several ways; auth needs differ:

1. **Local (default).** You run `kgraph serve` on your machine with your
   kubeconfig and open `127.0.0.1`. No auth needed — it's your machine, your
   kubeconfig, your RBAC. **This stays the default.**
2. **In-cluster, reached via `kubectl port-forward`.** kgraph is deployed into a
   namespace (its own ServiceAccount = in-cluster config) and users reach it with
   `kubectl port-forward`. Access is already gated by **Kubernetes RBAC on the
   `pods/portforward` subresource** — only people who can port-forward that pod
   get in, and kgraph sees the cluster as its ServiceAccount. No extra auth
   required; this is a legitimate, zero-config secure mode.
3. **Exposed (ingress/LoadBalancer).** Here a real login gate is needed → OIDC /
   GitHub (this spec).

So: bind to `127.0.0.1` by default; auth is **opt-in** and only relevant for
model 3.

## 3. Goals

- Opt-in authentication gate for `kgraph serve`:
  - **Generic OIDC** (issuer URL + client id/secret) — covers Azure AD, Keycloak,
    Google, Okta, Dex…
  - **GitHub** OAuth (optionally restricted to an org/team).
- Default remains **no auth** (models 1 & 2 unchanged).
- Clear separation of **authn** (this spec) from **per-user authz**
  (impersonation — a deferred Phase 2, §8).

## 4. Non-goals (this iteration)

- Per-user Kubernetes **impersonation** (each user sees only their RBAC) — Phase 2.
- Multi-tenant session storage / external session DB (in-memory is fine).
- SAML/LDAP.

## 5. Constraints

- §1 single binary: deps `golang.org/x/oauth2` + `github.com/coreos/go-oidc/v3`
  are small/pure-Go (acceptable; flag in go.mod). No CGO.
- §2 read-only. §5 out-of-the-box: with no auth flags, behave exactly as today.
- Security: cookies `HttpOnly`, `Secure` (when not localhost), `SameSite=Lax`;
  signed/encrypted session; CSRF state on the OAuth flow; never log tokens.

## 6. Design

### Configuration (flags / env)
- `--auth none|oidc|github` (default `none`).
- OIDC: `--auth-oidc-issuer`, `--auth-oidc-client-id`,
  `--auth-oidc-client-secret` (env `KGRAPH_OIDC_CLIENT_SECRET`),
  `--auth-redirect-url`, optional `--auth-allowed-domains` /
  `--auth-allowed-groups` (claim-based allowlist).
- GitHub: `--auth-github-client-id/-secret`, optional `--auth-github-org`,
  `--auth-github-team`.
- `--auth-cookie-key` (random if unset; rotating it logs everyone out).

### Flow (standard authorization-code)
- New `internal/web/auth` package: an `http.Handler` middleware wrapping the mux.
  Unauthenticated request → 302 to the IdP (`/auth/login`), with a random `state`
  cookie. Callback `/auth/callback` verifies `state`, exchanges code, validates
  the ID token (OIDC) / fetches the user + org/team (GitHub), applies the
  allowlist, sets a signed session cookie, redirects back.
- `/auth/logout` clears the session. `/static/*` and `/healthz` stay public.
- When `--auth none`: middleware is a no-op (today's behaviour).

### Session
- Signed (HMAC) cookie carrying `sub`, `email`/`login`, `name`, `exp`. In-memory
  nonce/state store with TTL. No server-side session table needed for v1.

### UI
- Header shows the signed-in user + a **Logout** link when auth is on.

## 7. Acceptance criteria (when implemented)

1. `--auth none` (default): UI fully open, no redirects — identical to today.
2. `--auth oidc` with a configured issuer: unauthenticated requests redirect to
   the IdP; after login the UI loads; allowlist (domain/group) is enforced.
3. `--auth github --auth-github-org X`: only members of org X can log in.
4. `/static/*` served without auth; API/render require a session when auth is on.
5. Cookies are HttpOnly/SameSite; state/CSRF validated; tokens never logged.
6. `go build ./...` and `go test ./...` pass; offline unit tests cover config
   parsing, the allowlist, session sign/verify, and the no-auth pass-through.
   (Full IdP handshake verified manually against a real provider.)

## 8. Phase 2 (future) — per-user RBAC via impersonation

To make each user "see only what they're allowed", kgraph would call the cluster
**as** the authenticated user using Kubernetes impersonation
(`Impersonate-User`/`-Group` from the OIDC `sub`/`groups`), which requires the
server's ServiceAccount to hold the `impersonate` RBAC verb. Per-request
collectors would be built with an impersonating rest.Config. Bigger security
surface — separate spec when wanted.

## 9. Rollout / docs

When built: CHANGELOG, README (an "Authentication" section: default open,
in-cluster port-forward model, opt-in OIDC/GitHub), `docs/PUBLISHING.md` (ingress
+ auth note), memory. Until then this stays a draft.

## 10. Open questions

- Bundle a tiny login landing page, or redirect straight to the IdP? (Lean:
  straight redirect.)
- Encrypt the session cookie (JWE) or sign-only (HMAC)? (Lean: HMAC sign; no
  secrets stored in the cookie.)
