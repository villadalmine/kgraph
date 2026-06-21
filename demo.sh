#!/usr/bin/env bash
#
# demo.sh — generate a sample set of kgraph diagrams, docs and security artifacts.
#
# Usage:   ./demo.sh [output-dir]      (default: demo-out)
# Edit the lists below to target your own namespaces.
#
set -uo pipefail
cd "$(dirname "$0")"

OUT="${1:-demo-out}"
BIN=./kgraph

# --- what to generate (edit these) ---
NS_TOPO=(argocd pihole cert-manager)                 # full namespace topology
NS_LAYER=("monitoring:monitoring" "argocd:argocd" "monitoring:cilium")  # ns:layer
NS_TRAFFIC=(pihole monitoring)                        # observed traffic (needs Hubble)
NS_DOCS=(argocd monitoring pihole)                    # docs-as-code

# run a command, printing it, and never abort the whole script on failure.
run() { echo "+ $*"; if "$@"; then :; else echo "  ! skipped (command failed)"; fi; }

# --- build if needed ---
if [ ! -x "$BIN" ]; then
  echo "== building kgraph =="
  go build -o kgraph ./cmd/kgraph || { echo "build failed"; exit 1; }
fi

mkdir -p "$OUT" "$OUT/docs"

echo "== capabilities =="
run $BIN doctor

echo "== namespace topology =="
for ns in "${NS_TOPO[@]}"; do
  run $BIN ns "$ns" -o "$OUT/$ns.svg"
done

echo "== layer views =="
for pair in "${NS_LAYER[@]}"; do
  ns="${pair%%:*}"; layer="${pair##*:}"
  run $BIN ns "$ns" --layer "$layer" -o "$OUT/$ns-$layer.svg"
done

echo "== cluster-scoped =="
run $BIN cluster --layer crossplane -o "$OUT/cluster-crossplane.svg"

echo "== traffic (Cilium Hubble) =="
for ns in "${NS_TRAFFIC[@]}"; do
  run $BIN traffic "$ns" -o "$OUT/$ns-traffic.svg"
done

echo "== security overlay + suggested policies =="
run $BIN traffic kagent --policy -o "$OUT/kagent-traffic-policy.svg"
run $BIN traffic monitoring --suggest-policy cilium -o "$OUT/monitoring-suggested-cnp.yaml"
run $BIN traffic monitoring --suggest-policy k8s -o "$OUT/monitoring-suggested-netpol.yaml"

echo "== docs-as-code =="
for ns in "${NS_DOCS[@]}"; do
  run $BIN doc "$ns" -o "$OUT/docs/$ns"
done

echo "== AI (only if OPENROUTER_API_KEY is set) =="
if [ -n "${OPENROUTER_API_KEY:-}" ]; then
  run $BIN explain argocd --layer argocd -o "$OUT/argocd-explain.md"
else
  echo "  skipped: export OPENROUTER_API_KEY to enable"
fi

echo
echo "== done =="
echo "Output in: $OUT/"
find "$OUT" -type f | sort | sed 's/^/  /'
echo
echo "Open an SVG:        xdg-open $OUT/argocd.svg"
echo "Read the docs:      xdg-open $OUT/docs/monitoring/monitoring.md"
echo "Interactive UI:     $BIN serve   # then open http://127.0.0.1:8080"
