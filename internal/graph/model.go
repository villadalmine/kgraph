// Package graph defines the in-memory typed graph model of Kubernetes
// resources and the builders that infer relationships (edges) between them.
package graph

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RelKind classifies why two nodes are connected.
type RelKind string

const (
	RelOwner    RelKind = "owner"    // ownerReference (controller -> child)
	RelSelector RelKind = "selector" // label selector (Service -> Pod, etc.)
	RelRef      RelKind = "ref"      // explicit object reference (Pod -> ConfigMap)
	RelCRD      RelKind = "crd"      // relation declared by a CRD rule
	RelFlow     RelKind = "flow"     // observed network traffic (Hubble)
)

// Node is a single Kubernetes object in the graph.
type Node struct {
	ID        string // stable unique id: group/kind/namespace/name
	Group     string
	Kind      string
	Namespace string
	Name      string
	Labels    map[string]string
	Layer     string // assigned stack/layer (e.g. "argocd"), empty if none
	Alert     bool   // flagged by analysis (e.g. workload with no NetworkPolicy)
	Note      string // optional annotation appended to the rendered label (e.g. throughput)
	Obj       *unstructured.Unstructured
}

// Edge connects two nodes with a typed relationship.
type Edge struct {
	From    string
	To      string
	Kind    RelKind
	Note    string // optional human label (e.g. "configMap")
	Weight  int    // flow edges: number of observed flows
	Dropped bool   // flow edges: traffic was denied/dropped
	Gap     bool   // flow edges: allowed traffic to an unprotected workload
	Error   bool   // flow edges: L7 server errors (5xx) observed
}

// Graph holds nodes and edges, deduplicated by id.
type Graph struct {
	Nodes map[string]*Node
	Edges []Edge

	edgeSet map[string]struct{}
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		Nodes:   map[string]*Node{},
		edgeSet: map[string]struct{}{},
	}
}

// NodeID builds the canonical node id for an object's coordinates.
func NodeID(group, kind, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s", group, kind, namespace, name)
}

// AddObject inserts (or returns the existing) node for an unstructured object.
func (g *Graph) AddObject(o *unstructured.Unstructured) *Node {
	group := o.GroupVersionKind().Group
	kind := o.GetKind()
	id := NodeID(group, kind, o.GetNamespace(), o.GetName())
	if n, ok := g.Nodes[id]; ok {
		return n
	}
	n := &Node{
		ID:        id,
		Group:     group,
		Kind:      kind,
		Namespace: o.GetNamespace(),
		Name:      o.GetName(),
		Labels:    o.GetLabels(),
		Obj:       o,
	}
	g.Nodes[id] = n
	return n
}

// AddNode inserts (or returns the existing) synthetic node identified by its
// coordinates, without an underlying object. Used for traffic endpoints that
// have no corresponding collected resource.
func (g *Graph) AddNode(group, kind, namespace, name string) *Node {
	id := NodeID(group, kind, namespace, name)
	if n, ok := g.Nodes[id]; ok {
		return n
	}
	n := &Node{ID: id, Group: group, Kind: kind, Namespace: namespace, Name: name}
	g.Nodes[id] = n
	return n
}

// AddFlowEdge records an observed-traffic edge with a weight and verdict.
func (g *Graph) AddFlowEdge(from, to string, weight int, dropped bool, note string) {
	g.AddFlowEdgeFull(from, to, weight, dropped, false, note)
}

// AddFlowEdgeFull records an observed-traffic edge, also flagging L7 errors.
func (g *Graph) AddFlowEdgeFull(from, to string, weight int, dropped, errored bool, note string) {
	if from == "" || to == "" || from == to {
		return
	}
	if _, ok := g.Nodes[from]; !ok {
		return
	}
	if _, ok := g.Nodes[to]; !ok {
		return
	}
	g.Edges = append(g.Edges, Edge{From: from, To: to, Kind: RelFlow, Note: note, Weight: weight, Dropped: dropped, Error: errored})
}

// AddEdge records a relationship, ignoring duplicates and dangling endpoints.
func (g *Graph) AddEdge(from, to string, kind RelKind, note string) {
	if from == "" || to == "" || from == to {
		return
	}
	if _, ok := g.Nodes[from]; !ok {
		return
	}
	if _, ok := g.Nodes[to]; !ok {
		return
	}
	key := from + "|" + to + "|" + string(kind) + "|" + note
	if _, ok := g.edgeSet[key]; ok {
		return
	}
	g.edgeSet[key] = struct{}{}
	g.Edges = append(g.Edges, Edge{From: from, To: to, Kind: kind, Note: note})
}

// SortedNodes returns nodes in a stable order (by id) for deterministic output.
func (g *Graph) SortedNodes() []*Node {
	out := make([]*Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
