// Package preflight performs read-only capability checks against a cluster and
// reports, for anything missing, exactly what the user needs to do. It powers
// `kgraph doctor` and the per-command preflight guards.
package preflight

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/layers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Status is the outcome of a single check.
type Status int

const (
	OK Status = iota
	Warn
	Fail
)

func (s Status) Symbol() string {
	switch s {
	case OK:
		return "✔"
	case Warn:
		return "⚠"
	default:
		return "✖"
	}
}

// Check is one capability probe result.
type Check struct {
	Name        string
	Status      Status
	Detail      string // what was found
	Remediation string // what to do, when not OK
}

var (
	cmGVR  = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	svcGVR = schema.GroupVersionResource{Version: "v1", Resource: "services"}
)

// Run executes all checks in order and returns their results.
func Run(ctx context.Context, c *collect.Collector) []Check {
	groups := serverGroups(c)
	return []Check{
		checkCluster(c),
		checkCilium(groups),
		checkHubble(ctx, c),
		checkRelay(ctx, c),
		checkPrometheus(ctx, c, groups),
		checkStacks(groups),
		checkLLM(),
	}
}

func checkCluster(c *collect.Collector) Check {
	v, err := c.Discovery().ServerVersion()
	if err != nil {
		return Check{"cluster", Fail, "cannot reach the API server",
			"check your kubeconfig / --context / network: " + err.Error()}
	}
	return Check{"cluster", OK, "reachable, " + v.GitVersion, ""}
}

func checkCilium(groups map[string]bool) Check {
	if groups["cilium.io"] {
		return Check{"Cilium", OK, "cilium.io API group present", ""}
	}
	return Check{"Cilium", Warn, "no cilium.io API group",
		"traffic visualization (kgraph traffic) needs Cilium + Hubble"}
}

func checkHubble(ctx context.Context, c *collect.Collector) Check {
	cm, err := c.Dynamic().Resource(cmGVR).Namespace("kube-system").Get(ctx, "cilium-config", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return Check{"Hubble", Warn, "cannot read cm/cilium-config (RBAC)",
				"grant get on configmaps in kube-system, or rely on the relay check"}
		}
		return Check{"Hubble", Warn, "cilium-config not found", "is Cilium installed?"}
	}
	if v, _, _ := unstructured.NestedString(cm.Object, "data", "enable-hubble"); v == "true" {
		return Check{"Hubble", OK, "enable-hubble=true", ""}
	}
	return Check{"Hubble", Fail, "Hubble is disabled",
		"enable it with: cilium hubble enable --relay"}
}

func checkRelay(ctx context.Context, c *collect.Collector) Check {
	_, err := c.Dynamic().Resource(svcGVR).Namespace("kube-system").Get(ctx, "hubble-relay", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Check{"hubble-relay", Fail, "svc/hubble-relay not found in kube-system",
				"deploy it with: cilium hubble enable --relay"}
		}
		return Check{"hubble-relay", Warn, "could not query svc/hubble-relay: " + err.Error(),
			"check RBAC for services in kube-system"}
	}
	return Check{"hubble-relay", OK, "svc/hubble-relay present (kgraph can port-forward it)", ""}
}

func checkPrometheus(ctx context.Context, c *collect.Collector, groups map[string]bool) Check {
	if !groups["monitoring.coreos.com"] {
		return Check{"Prometheus", Warn, "no Prometheus operator (monitoring.coreos.com) detected",
			"rate-based traffic edges need Prometheus; deploy kube-prometheus-stack"}
	}
	// Hubble metrics are exposed but need a ServiceMonitor to be scraped.
	if _, err := c.Dynamic().Resource(svcGVR).Namespace("kube-system").Get(ctx, "hubble-metrics", metav1.GetOptions{}); err == nil {
		return Check{"Prometheus", Warn, "Prometheus present; hubble-metrics exposed but may not be scraped",
			"add a ServiceMonitor for svc/hubble-metrics:9965 to enable rate-based edges"}
	}
	return Check{"Prometheus", OK, "Prometheus operator present", ""}
}

func checkStacks(groups map[string]bool) Check {
	var present []string
	for _, r := range layers.Builtin {
		if groups[r.GroupSuffix] || hasSuffixGroup(groups, r.GroupSuffix) {
			present = append(present, r.Name)
		}
	}
	if len(present) == 0 {
		return Check{"stacks", Warn, "no known stacks detected", ""}
	}
	return Check{"stacks", OK, strings.Join(dedup(present), ", "), ""}
}

func checkLLM() Check {
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		return Check{"LLM", OK, "OPENROUTER_API_KEY set (explain/ask enabled)", ""}
	}
	return Check{"LLM", Warn, "OPENROUTER_API_KEY not set (explain/ask disabled)",
		"get a free key at openrouter.ai and: export OPENROUTER_API_KEY=sk-or-..."}
}

// RequireHubble is the guard a traffic command calls before doing work; it
// returns an actionable error if the Hubble path is not usable.
func RequireHubble(ctx context.Context, c *collect.Collector) error {
	groups := serverGroups(c)
	if !groups["cilium.io"] {
		return fmt.Errorf("'traffic' needs Cilium Hubble, but this cluster has no cilium.io API group")
	}
	r := checkRelay(ctx, c)
	if r.Status == Fail {
		return fmt.Errorf("'traffic' needs Cilium Hubble.\n  ✖ %s\n  %s\n  Or point kgraph at an existing relay: --relay-addr host:port",
			r.Detail, r.Remediation)
	}
	return nil
}

// --- helpers ---

func serverGroups(c *collect.Collector) map[string]bool {
	out := map[string]bool{}
	gl, err := c.Discovery().ServerGroups()
	if err != nil || gl == nil {
		return out
	}
	for _, g := range gl.Groups {
		out[g.Name] = true
	}
	return out
}

func hasSuffixGroup(groups map[string]bool, suffix string) bool {
	for g := range groups {
		if g == suffix || strings.HasSuffix(g, "."+suffix) {
			return true
		}
	}
	return false
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
