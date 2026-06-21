package render

import (
	"strings"
	"testing"

	"github.com/villadalmine/kgraph/internal/graph"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mk(apiVersion, kind, name string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]interface{}{}}
	o.SetAPIVersion(apiVersion)
	o.SetKind(kind)
	o.SetNamespace("ns1")
	o.SetName(name)
	return o
}

func sampleGraph() *graph.Graph {
	g := graph.New()
	dep := g.AddObject(mk("apps/v1", "Deployment", "web"))
	pod := g.AddObject(mk("v1", "Pod", "web-1"))
	g.AddEdge(dep.ID, pod.ID, graph.RelOwner, "")
	return g
}

func TestD2Structure(t *testing.T) {
	out := D2(sampleGraph(), "Test", false)

	for _, want := range []string{"direction: right", "# Test", "Deployment", "Pod", "->", "style.fill"} {
		if !strings.Contains(out, want) {
			t.Errorf("D2 output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "icon:") {
		t.Errorf("icons should be absent when icons=false")
	}
}

func TestD2Icons(t *testing.T) {
	out := D2(sampleGraph(), "Test", true)
	if !strings.Contains(out, "icon:") {
		t.Errorf("expected icon lines when icons=true")
	}
}

// A node's Note is appended to its rendered label when set (spec 0002, AC3).
func TestD2NodeNote(t *testing.T) {
	g := graph.New()
	n := g.AddObject(mk("v1", "Pod", "web-1"))
	n.Note = "↓1.2 kB/s ↑3 B/s"
	out := D2(g, "Test", false)
	if !strings.Contains(out, "1.2 kB/s") {
		t.Errorf("expected node note in label:\n%s", out)
	}

	g2 := graph.New()
	g2.AddObject(mk("v1", "Pod", "web-2"))
	if strings.Contains(D2(g2, "Test", false), "kB/s") {
		t.Errorf("note text should be absent when Note is empty")
	}
}

// D2 output is deterministic for a fixed graph — a golden-style stability check
// guarding against accidental nondeterminism (map iteration) in render (spec 0008).
func TestD2Deterministic(t *testing.T) {
	g := func() *graph.Graph {
		gg := graph.New()
		for _, n := range []string{"web", "api", "cache"} {
			gg.AddObject(mk("apps/v1", "Deployment", n))
		}
		return gg
	}
	a, b := D2(g(), "T", false), D2(g(), "T", false)
	if a != b {
		t.Errorf("D2 output must be deterministic across runs\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

// IconName maps known kinds, falls back to crd, and is empty for synthetic
// nodes (spec 0014).
func TestIconName(t *testing.T) {
	cases := map[string]string{
		"Deployment": "deploy", "Pod": "pod", "Service": "svc",
		"SomeCustomThing": "crd", "External": "", "": "",
	}
	for kind, want := range cases {
		if got := IconName(kind); got != want {
			t.Errorf("IconName(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestCategoryColorStable(t *testing.T) {
	f1, s1 := categoryColor("Pod")
	f2, s2 := categoryColor("Pod")
	if f1 != f2 || s1 != s2 {
		t.Errorf("category color must be deterministic")
	}
	if f1 == "" || s1 == "" {
		t.Errorf("category color must be non-empty")
	}
}
