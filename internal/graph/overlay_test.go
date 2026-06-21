package graph

import "testing"

// Overlay attaches flow edges to matching topology nodes (preferring the
// workload over a same-named Service) and adds unmatched external endpoints
// (spec 0007, AC1/AC2).
func TestOverlay(t *testing.T) {
	base := New()
	dep := base.AddNode("apps", "Deployment", "ns1", "web")
	svc := base.AddNode("", "Service", "ns1", "web") // same name, different kind
	_ = svc

	flows := New()
	wl := flows.AddNode("apps", "Deployment", "ns1", "web")
	ext := flows.AddNode("", "External", "", "world")
	flows.AddFlowEdgeFull(ext.ID, wl.ID, 10, false, false, "443")

	before := len(base.Edges)
	Overlay(base, flows)

	if len(base.Edges) != before+1 {
		t.Fatalf("expected one flow edge added, got %d new", len(base.Edges)-before)
	}
	e := base.Edges[len(base.Edges)-1]
	if e.To != dep.ID {
		t.Errorf("flow should attach to the Deployment, got %s", e.To)
	}
	if e.Weight != 10 {
		t.Errorf("weight not preserved: %d", e.Weight)
	}
	// External endpoint must have been pulled into the base graph.
	if _, ok := base.Nodes[ext.ID]; !ok {
		t.Errorf("external endpoint not added to base graph")
	}
}
