package layers

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMatches(t *testing.T) {
	argocd, _ := Find("argocd")
	if !argocd.Matches("argoproj.io", "Application") {
		t.Errorf("argocd should match argoproj.io/Application")
	}
	if argocd.Matches("argoproj.io", "Workflow") {
		t.Errorf("argocd should NOT match argoproj.io/Workflow (that is argo-workflows)")
	}

	crossplane, _ := Find("crossplane")
	// suffix match across sub-groups
	if !crossplane.Matches("pkg.crossplane.io", "Provider") {
		t.Errorf("crossplane should match *.crossplane.io")
	}
	if crossplane.Matches("apps", "Deployment") {
		t.Errorf("crossplane should not match core apps group")
	}
}

func TestDetect(t *testing.T) {
	mk := func(apiVersion, kind string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{Object: map[string]interface{}{}}
		o.SetAPIVersion(apiVersion)
		o.SetKind(kind)
		return o
	}
	objs := []*unstructured.Unstructured{
		mk("argoproj.io/v1alpha1", "Application"),
		mk("argoproj.io/v1alpha1", "Application"),
		mk("cilium.io/v2", "CiliumNetworkPolicy"),
		mk("apps/v1", "Deployment"), // not a known stack
	}
	got := Detect(objs)
	counts := map[string]int{}
	for _, d := range got {
		counts[d.Name] = d.Count
	}
	if counts["argocd"] != 2 {
		t.Errorf("expected 2 argocd resources, got %d", counts["argocd"])
	}
	if counts["cilium"] != 1 {
		t.Errorf("expected 1 cilium resource, got %d", counts["cilium"])
	}
	// Detect should sort by count desc: argocd first.
	if len(got) == 0 || got[0].Name != "argocd" {
		t.Errorf("expected argocd first (highest count), got %+v", got)
	}
}

// Rank orders by significance tier first, even when a lower tier has more
// resources (spec 0003, AC1); unknown stacks fall to the default tier (AC2).
func TestRank(t *testing.T) {
	in := []Detected{
		{Name: "monitoring", Count: 50, Tier: 3},
		{Name: "argocd", Count: 2, Tier: 1},
		{Name: "mystery", Count: 99, Tier: 0}, // unlisted -> default tier 9
		{Name: "cilium", Count: 10, Tier: 2},
	}
	got := Rank(in)
	want := []string{"argocd", "cilium", "monitoring", "mystery"}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("rank position %d: want %s, got %s (full: %+v)", i, name, got[i].Name, got)
		}
	}
}
