// Package layers detects which higher-level "stacks" (ArgoCD, Crossplane, Cilium,
// the monitoring stack, etc.) are present in a set of resources, so diagrams can
// be scoped to one meaningful abstraction layer instead of the whole namespace.
package layers

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Rule matches resources belonging to a stack by API group suffix and,
// optionally, a restricted set of kinds (used to split shared groups such as
// argoproj.io into ArgoCD vs Argo Workflows).
type Rule struct {
	Name        string
	GroupSuffix string   // matches group == suffix or *.suffix
	Kinds       []string // optional; empty means any kind in the group
	Desc        string
	Tier        int // significance for docs ordering; lower = higher altitude, shown first
}

// defaultTier is used for unlisted/unknown stacks so they sort last.
const defaultTier = 9

// Matches reports whether a group/kind belongs to this stack.
func (r Rule) Matches(group, kind string) bool {
	if !(group == r.GroupSuffix || strings.HasSuffix(group, "."+r.GroupSuffix)) {
		return false
	}
	if len(r.Kinds) == 0 {
		return true
	}
	for _, k := range r.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Builtin is the catalog of known stacks, ordered for stable listing.
// Tiers (lower = higher abstraction, documented first): 1 control/GitOps/cluster
// lifecycle, 2 platform networking/ingress/PKI, 3 observability, 4
// storage/virtualization, 5 apps/agents. See specs/0003-namespace-autodoc.md.
var Builtin = []Rule{
	{Name: "argocd", GroupSuffix: "argoproj.io", Kinds: []string{"Application", "ApplicationSet", "AppProject"}, Desc: "ArgoCD GitOps apps", Tier: 1},
	{Name: "argo-workflows", GroupSuffix: "argoproj.io", Kinds: []string{"Workflow", "CronWorkflow", "WorkflowTemplate", "ClusterWorkflowTemplate", "WorkflowTaskSet", "WorkflowEventBinding"}, Desc: "Argo Workflows", Tier: 1},
	{Name: "crossplane", GroupSuffix: "crossplane.io", Desc: "Crossplane compositions & managed resources", Tier: 1},
	{Name: "capi", GroupSuffix: "cluster.x-k8s.io", Desc: "Cluster API", Tier: 1},
	{Name: "cilium", GroupSuffix: "cilium.io", Desc: "Cilium networking", Tier: 2},
	{Name: "gateway", GroupSuffix: "gateway.networking.k8s.io", Desc: "Gateway API", Tier: 2},
	{Name: "cert-manager", GroupSuffix: "cert-manager.io", Desc: "cert-manager certificates", Tier: 2},
	{Name: "monitoring", GroupSuffix: "monitoring.coreos.com", Desc: "Prometheus/Alertmanager monitoring stack", Tier: 3},
	{Name: "longhorn", GroupSuffix: "longhorn.io", Desc: "Longhorn storage", Tier: 4},
	{Name: "kubevirt", GroupSuffix: "kubevirt.io", Desc: "KubeVirt/CDI virtualization", Tier: 4},
	{Name: "kagent", GroupSuffix: "kagent.dev", Desc: "kagent AI agents", Tier: 5},
	{Name: "holmesgpt", GroupSuffix: "holmesgpt.dev", Desc: "HolmesGPT", Tier: 5},
}

// Find returns the rule with the given name.
func Find(name string) (Rule, bool) {
	for _, r := range Builtin {
		if r.Name == name {
			return r, true
		}
	}
	return Rule{}, false
}

// Detected is a stack found in a namespace with a count of its resources.
type Detected struct {
	Name  string
	Desc  string
	Count int
	Tier  int
}

// Detect scans objects and returns the stacks present, sorted by count desc.
func Detect(objs []*unstructured.Unstructured) []Detected {
	counts := map[string]int{}
	desc := map[string]string{}
	for _, o := range objs {
		group := o.GroupVersionKind().Group
		kind := o.GetKind()
		for _, r := range Builtin {
			if r.Matches(group, kind) {
				counts[r.Name]++
				desc[r.Name] = r.Desc
			}
		}
	}
	out := make([]Detected, 0, len(counts))
	for name, c := range counts {
		t := defaultTier
		if r, ok := Find(name); ok {
			t = r.Tier
		}
		out = append(out, Detected{Name: name, Desc: desc[name], Count: c, Tier: t})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Rank reorders detected stacks for documentation: by significance tier
// (ascending — higher abstraction first), then by count desc, then name. It does
// not mutate the input. See specs/0003-namespace-autodoc.md (D2/D3).
func Rank(d []Detected) []Detected {
	out := append([]Detected(nil), d...)
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].Tier, out[j].Tier
		if ti == 0 {
			ti = defaultTier
		}
		if tj == 0 {
			tj = defaultTier
		}
		if ti != tj {
			return ti < tj
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}
