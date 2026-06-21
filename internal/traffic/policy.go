package traffic

import (
	"context"
	"fmt"
	"strings"

	"github.com/villadalmine/kgraph/internal/graph"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	npGVR   = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}
	cnpGVR  = schema.GroupVersionResource{Group: "cilium.io", Version: "v2", Resource: "ciliumnetworkpolicies"}
	ccnpGVR = schema.GroupVersionResource{Group: "cilium.io", Version: "v2", Resource: "ciliumclusterwidenetworkpolicies"}
)

// selector is a normalized matchLabels selector. An empty labels map matches all.
type selector struct{ labels map[string]string }

func (s selector) matches(nodeLabels map[string]string) bool {
	if len(s.labels) == 0 {
		return true // selects everything (e.g. NetworkPolicy with empty podSelector)
	}
	for k, v := range s.labels {
		if nodeLabels[k] != v {
			return false
		}
	}
	return true
}

// Summary reports the security findings of the policy overlay.
type Summary struct {
	Policies    int
	Unprotected []string // in-namespace workloads selected by no policy
	DeniedFlows int      // total flows dropped (sum of weights)
	GapFlows    int      // allowed flows reaching an unprotected workload
}

func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "policies found: %d\n", s.Policies)
	fmt.Fprintf(&b, "denied flows (red): %d\n", s.DeniedFlows)
	fmt.Fprintf(&b, "gap flows to unprotected workloads (amber): %d\n", s.GapFlows)
	if len(s.Unprotected) > 0 {
		fmt.Fprintf(&b, "⚠ workloads with NO NetworkPolicy/CiliumNetworkPolicy (%d): %s\n",
			len(s.Unprotected), strings.Join(s.Unprotected, ", "))
	} else {
		b.WriteString("✔ every in-namespace workload is selected by at least one policy\n")
	}
	return b.String()
}

// AnalyzePolicies collects policies for ns, marks unprotected in-namespace nodes
// and gap/denied flow edges in g, and returns a summary.
func AnalyzePolicies(ctx context.Context, dyn dynamic.Interface, ns string, g *graph.Graph) (Summary, error) {
	sels, count := collectSelectors(ctx, dyn, ns)
	sum := Summary{Policies: count}

	protected := func(n *graph.Node) bool {
		for _, s := range sels {
			if s.matches(n.Labels) {
				return true
			}
		}
		return false
	}

	// Mark unprotected in-namespace workloads.
	unprotectedIDs := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Namespace != ns || n.Kind == "External" {
			continue
		}
		if !protected(n) {
			n.Alert = true
			unprotectedIDs[n.ID] = true
			sum.Unprotected = append(sum.Unprotected, n.Name)
		}
	}
	sum.Unprotected = dedupStr(sum.Unprotected)

	// Classify edges: denied (already dropped) vs gap (allowed -> unprotected).
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind != graph.RelFlow {
			continue
		}
		if e.Dropped {
			sum.DeniedFlows += e.Weight
			continue
		}
		if unprotectedIDs[e.To] {
			e.Gap = true
			sum.GapFlows += e.Weight
		}
	}
	return sum, nil
}

// collectSelectors lists NetworkPolicies, CiliumNetworkPolicies and
// CiliumClusterwideNetworkPolicies and extracts their target selectors.
func collectSelectors(ctx context.Context, dyn dynamic.Interface, ns string) ([]selector, int) {
	var sels []selector
	count := 0

	if list, err := dyn.Resource(npGVR).Namespace(ns).List(ctx, metav1.ListOptions{}); err == nil {
		for i := range list.Items {
			count++
			sels = append(sels, selector{labels: nestedLabels(list.Items[i].Object, "spec", "podSelector", "matchLabels")})
		}
	}
	for _, gvr := range []schema.GroupVersionResource{cnpGVR, ccnpGVR} {
		var lister dynamic.ResourceInterface = dyn.Resource(gvr)
		if gvr == cnpGVR {
			lister = dyn.Resource(gvr).Namespace(ns)
		}
		list, err := lister.List(ctx, metav1.ListOptions{})
		if err != nil {
			continue // CRD may be absent
		}
		for i := range list.Items {
			count++
			sels = append(sels, ciliumSelectors(list.Items[i].Object)...)
		}
	}
	return sels, count
}

// ciliumSelectors extracts endpointSelector.matchLabels from both spec and the
// specs[] array form of a Cilium policy.
func ciliumSelectors(obj map[string]interface{}) []selector {
	var out []selector
	if l := nestedLabels(obj, "spec", "endpointSelector", "matchLabels"); l != nil {
		out = append(out, selector{labels: l})
	}
	if specs, ok, _ := unstructured.NestedSlice(obj, "specs"); ok {
		for _, s := range specs {
			sm, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, selector{labels: nestedLabels(sm, "endpointSelector", "matchLabels")})
		}
	}
	if len(out) == 0 {
		out = append(out, selector{}) // selects all endpoints in scope
	}
	return out
}

// nestedLabels reads a matchLabels map and normalizes keys (strips the "k8s:"
// source prefix used by Cilium selectors).
func nestedLabels(obj map[string]interface{}, fields ...string) map[string]string {
	raw, ok, _ := unstructured.NestedStringMap(obj, fields...)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimPrefix(k, "k8s:")
		k = strings.TrimPrefix(k, "any:")
		out[k] = v
	}
	return out
}
