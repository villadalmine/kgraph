# Spec 0011 — Traffic correctness & relay mTLS

- **Status:** done (verified 2026-06-20: StatefulSet ordinal pods reconcile to one
  node [TestReconcileStatefulSetOrdinal]; mTLS option/creds + secret loader build
  & unit verified; `go test ./...` green. Live mTLS handshake unverifiable here —
  homelab relay is non-TLS.)
- **Feature ID:** F11 hardening (`docs/SPEC.md` §4)
- **Author / date:** kgraph — 2026-06-20

## 1. Problem / motivation

Two remaining traffic items ([[agnostic-design]]): a StatefulSet seen *only*
without Hubble workload info kept its ordinal-suffixed pod name and wouldn't
merge with its workload node; and TLS-relay support (spec 0009) lacked **mTLS**
client certificates, which a hardened relay requires.

## 2. Goals

- StatefulSet pods reconcile to their workload (strip the `-<ordinal>` suffix in
  the pod-name fallback when no ReplicaSet hash is present).
- mTLS support for the relay: client cert/key via flags, and auto-load from a
  Kubernetes TLS secret (default `kube-system/hubble-relay-client-certs`).

## 3. Non-goals

- Verifying a live TLS/mTLS relay handshake (no such relay in the dev cluster).

## 4. Constraints

§3 agnostic (secret namespace/name overridable; ordinal heuristic naming-neutral),
backward compatible (no opts = insecure).

## 5. Design

- `classify` fallback: `name = rsHash.strip(pod)`; if unchanged, `name =
  ordinal.strip(pod)` (`-\d+$`). Reconcile (spec 0005) then merges by (ns,name).
- `dialConfig` gains `caPEM/certPEM/keyPEM`; `WithClientCert(certFile,keyFile)`
  and `WithMTLSPEM(ca,cert,key)` options; `transportCreds` loads an
  `tls.X509KeyPair` when a client cert is set.
- `traffic.MTLSFromSecret(ctx, cfg, ref)` reads `ca.crt/tls.crt/tls.key` from the
  secret (ref "namespace/name", default kube-system/hubble-relay-client-certs)
  and returns a `WithMTLSPEM` option.
- CLI `traffic`: `--relay-cert`, `--relay-key`, `--relay-mtls` (default secret),
  `--relay-mtls-secret ns/name`.

## 6. Acceptance criteria

1. StatefulSet pod-only flow merges with its workload node (unit). ✅
2. `WithMTLSPEM` sets config; bad CA/keypair error in `transportCreds` (unit). ✅
3. `go build ./...` and `go test ./...` pass. ✅

## 7. Rollout / docs

CHANGELOG, README (mTLS flags), `docs/IMPROVEMENTS.md` (tick), memory.
