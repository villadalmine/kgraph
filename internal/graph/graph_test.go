package graph

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// obj builds an unstructured object with a unique UID (= name) and labels.
func obj(apiVersion, kind, ns, name string, labels map[string]string, mods ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]interface{}{}}
	o.SetAPIVersion(apiVersion)
	o.SetKind(kind)
	o.SetNamespace(ns)
	o.SetName(name)
	o.SetUID(types.UID("uid-" + name))
	if labels != nil {
		o.SetLabels(labels)
	}
	for _, m := range mods {
		m(o)
	}
	return o
}

func ownedBy(apiVersion, kind, name string) func(*unstructured.Unstructured) {
	return func(o *unstructured.Unstructured) {
		o.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: name}})
	}
}

func hasEdge(g *Graph, fromKind, fromName, toKind, toName string, kind RelKind) bool {
	for _, e := range g.Edges {
		f, t := g.Nodes[e.From], g.Nodes[e.To]
		if f == nil || t == nil {
			continue
		}
		if f.Kind == fromKind && f.Name == fromName && t.Kind == toKind && t.Name == toName && e.Kind == kind {
			return true
		}
	}
	return false
}

func sampleObjects() []*unstructured.Unstructured {
	lbl := map[string]string{"app": "web"}
	dep := obj("apps/v1", "Deployment", "ns1", "web", lbl, func(o *unstructured.Unstructured) {
		unstructured.SetNestedStringMap(o.Object, lbl, "spec", "selector", "matchLabels")
	})
	rs := obj("apps/v1", "ReplicaSet", "ns1", "web-abc", lbl, ownedBy("apps/v1", "Deployment", "web"))
	pod := obj("v1", "Pod", "ns1", "web-abc-1", lbl, ownedBy("apps/v1", "ReplicaSet", "web-abc"),
		func(o *unstructured.Unstructured) {
			unstructured.SetNestedField(o.Object, "sa1", "spec", "serviceAccountName")
			unstructured.SetNestedSlice(o.Object, []interface{}{
				map[string]interface{}{"name": "v", "configMap": map[string]interface{}{"name": "cfg"}},
			}, "spec", "volumes")
		})
	svc := obj("v1", "Service", "ns1", "web-svc", nil, func(o *unstructured.Unstructured) {
		unstructured.SetNestedStringMap(o.Object, lbl, "spec", "selector")
	})
	cfg := obj("v1", "ConfigMap", "ns1", "cfg", nil)
	sa := obj("v1", "ServiceAccount", "ns1", "sa1", nil)
	orphan := obj("v1", "ConfigMap", "ns1", "orphan", nil)
	return []*unstructured.Unstructured{dep, rs, pod, svc, cfg, sa, orphan}
}

func TestBuildEdges(t *testing.T) {
	g := Build(sampleObjects())

	cases := []struct {
		fk, fn, tk, tn string
		rk             RelKind
	}{
		{"Deployment", "web", "ReplicaSet", "web-abc", RelOwner},
		{"ReplicaSet", "web-abc", "Pod", "web-abc-1", RelOwner},
		{"Service", "web-svc", "Pod", "web-abc-1", RelSelector},
		{"Pod", "web-abc-1", "ConfigMap", "cfg", RelRef},
		{"Pod", "web-abc-1", "ServiceAccount", "sa1", RelRef},
	}
	for _, c := range cases {
		if !hasEdge(g, c.fk, c.fn, c.tk, c.tn, c.rk) {
			t.Errorf("missing edge %s/%s --%s--> %s/%s", c.fk, c.fn, c.rk, c.tk, c.tn)
		}
	}
}

func TestPrune(t *testing.T) {
	g := Build(sampleObjects())
	Prune(g, false)

	// ReplicaSet is noise and should be removed...
	for _, n := range g.Nodes {
		if n.Kind == "ReplicaSet" {
			t.Errorf("ReplicaSet should have been pruned")
		}
		if n.Kind == "ConfigMap" && n.Name == "orphan" {
			t.Errorf("unreferenced ConfigMap should have been pruned")
		}
	}
	// ...with the owner chain rewired Deployment -> Pod.
	if !hasEdge(g, "Deployment", "web", "Pod", "web-abc-1", RelOwner) {
		t.Errorf("expected rewired Deployment -> Pod owner edge after pruning ReplicaSet")
	}
	// Referenced ConfigMap is kept.
	found := false
	for _, n := range g.Nodes {
		if n.Kind == "ConfigMap" && n.Name == "cfg" {
			found = true
		}
	}
	if !found {
		t.Errorf("referenced ConfigMap cfg should be kept")
	}
}

func TestPruneKeepAll(t *testing.T) {
	g := Build(sampleObjects())
	before := len(g.Nodes)
	Prune(g, true)
	if len(g.Nodes) != before {
		t.Errorf("keepAll should not drop nodes: before=%d after=%d", before, len(g.Nodes))
	}
}

func TestSubgraph(t *testing.T) {
	objs := append(sampleObjects(),
		obj("argoproj.io/v1alpha1", "Application", "ns1", "app1", nil))
	g := Build(objs)

	seed := func(n *Node) bool { return n.Group == "argoproj.io" }
	sub := g.Subgraph(seed, 1, "argocd")

	var app *Node
	for _, n := range sub.Nodes {
		if n.Kind == "Application" {
			app = n
		}
	}
	if app == nil {
		t.Fatalf("Application not in subgraph")
	}
	if app.Layer != "argocd" {
		t.Errorf("seed node should have Layer=argocd, got %q", app.Layer)
	}
}

func TestDescribe(t *testing.T) {
	g := Build(sampleObjects())
	d := Describe(g)
	if !strings.Contains(d, "RESOURCES") {
		t.Errorf("describe should list resources")
	}
	if !strings.Contains(d, "Deployment") {
		t.Errorf("describe should mention Deployment")
	}
}
