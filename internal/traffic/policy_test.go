package traffic

import (
	"strings"
	"testing"

	"github.com/villadalmine/kgraph/internal/graph"
)

func TestSelectorMatches(t *testing.T) {
	s := selector{labels: map[string]string{"app": "web"}}
	if !s.matches(map[string]string{"app": "web", "tier": "fe"}) {
		t.Error("selector should match a superset of its labels")
	}
	if s.matches(map[string]string{"app": "db"}) {
		t.Error("selector should not match different labels")
	}
	// An empty selector (matches everything) must match a non-empty label set.
	if !(selector{labels: map[string]string{}}).matches(map[string]string{"x": "y"}) {
		t.Error("empty selector should match all")
	}
}

func TestParsePort(t *testing.T) {
	cases := map[string][2]string{
		"TCP:8080":   {"TCP", "8080"},
		"UDP:53":     {"UDP", "53"},
		"100 TCP:80": {"TCP", "80"}, // weight prefix present
		"ICMP":       {"TCP", ""},   // no proto:port -> defaults to TCP, empty port
	}
	for in, want := range cases {
		p, port := parsePort(in)
		if p != want[0] || port != want[1] {
			t.Errorf("parsePort(%q) = (%q,%q), want (%q,%q)", in, p, port, want[0], want[1])
		}
	}
}

// SuggestPolicies turns observed in-cluster flows into a least-privilege policy
// per destination workload (spec 0008).
func TestSuggestPolicies(t *testing.T) {
	g := graph.New()
	dst := g.AddNode("apps", "Deployment", "shop", "api")
	dst.Labels = map[string]string{"app": "api"}
	src := g.AddNode("apps", "Deployment", "shop", "web")
	src.Labels = map[string]string{"app": "web"}
	ext := g.AddNode("", "External", "", "world")
	g.AddFlowEdgeFull(src.ID, dst.ID, 12, false, false, "TCP:8080")
	g.AddFlowEdgeFull(ext.ID, dst.ID, 3, false, false, "TCP:443") // external -> commented out

	for _, kind := range []string{"k8s", "cilium"} {
		out, err := SuggestPolicies(g, "shop", kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		s := string(out)
		if !strings.Contains(s, "allow-observed-api") {
			t.Errorf("%s: missing policy for api:\n%s", kind, s)
		}
		if !strings.Contains(s, "app: web") {
			t.Errorf("%s: missing observed source web:\n%s", kind, s)
		}
		if !strings.Contains(s, "8080") {
			t.Errorf("%s: missing observed port:\n%s", kind, s)
		}
		if !strings.Contains(s, "skipped:") || !strings.Contains(s, "external") {
			t.Errorf("%s: external source should be noted as skipped:\n%s", kind, s)
		}
	}

	if k8s, _ := SuggestPolicies(g, "shop", "k8s"); !strings.Contains(string(k8s), "networking.k8s.io/v1") {
		t.Error("k8s policy should use networking.k8s.io/v1")
	}
	if cil, _ := SuggestPolicies(g, "shop", "cilium"); !strings.Contains(string(cil), "cilium.io") {
		t.Error("cilium policy should use a cilium.io apiVersion")
	}
}
