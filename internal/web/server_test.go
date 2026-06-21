package web

import (
	"testing"
	"time"

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

// emitDiagram honours the d2 format (spec 0006, AC1).
func TestEmitDiagramD2(t *testing.T) {
	g := graph.Build([]*unstructured.Unstructured{mk("apps/v1", "Deployment", "web")})
	body, ctype, err := emitDiagram(t.Context(), g, "T", "d2")
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "text/plain; charset=utf-8" {
		t.Errorf("want text/plain, got %q", ctype)
	}
	if got := string(body); len(got) == 0 || got[:9] != "direction" {
		t.Errorf("d2 output should start with 'direction', got %.20q", got)
	}
}

// graphJSON converts nodes/edges and assigns colours from the render palette
// (spec 0012, AC1/AC5).
func TestGraphJSON(t *testing.T) {
	g := graph.New()
	a := g.AddObject(mk("apps/v1", "Deployment", "web"))
	b := g.AddObject(mk("v1", "Pod", "web-1"))
	g.AddEdge(a.ID, b.ID, graph.RelOwner, "")
	doc := graphJSON(g, "T")
	if doc.Title != "T" || len(doc.Nodes) != 2 || len(doc.Edges) != 1 {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	for _, n := range doc.Nodes {
		if n.Color == "" || n.Stroke == "" {
			t.Errorf("node %s missing palette colour", n.Name)
		}
	}
	if doc.Edges[0].Source != a.ID || doc.Edges[0].Target != b.ID {
		t.Errorf("edge endpoints wrong: %+v", doc.Edges[0])
	}
}

// The cache serves within TTL and recomputes after expiry (spec 0006, AC3).
func TestCache(t *testing.T) {
	s := NewServer("", "")
	calls := 0
	build := func() (cacheEntry, error) { calls++; return cacheEntry{nodes: calls}, nil }

	if _, err := s.cached("k", build); err != nil {
		t.Fatal(err)
	}
	if _, err := s.cached("k", build); err != nil { // hit
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 build (cache hit), got %d", calls)
	}

	// Force expiry and confirm recompute.
	s.mu.Lock()
	e := s.cache["k"]
	e.at = time.Now().Add(-2 * cacheTTL)
	s.cache["k"] = e
	s.mu.Unlock()
	if _, err := s.cached("k", build); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected recompute after expiry, got %d builds", calls)
	}
}
