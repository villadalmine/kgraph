package graph

// noiseKinds are low-signal resource kinds excluded from diagrams by default;
// they explode the node count and slow down layout without aiding understanding.
var noiseKinds = map[string]bool{
	"Event":              true,
	"EndpointSlice":      true,
	"Endpoints":          true,
	"ControllerRevision": true,
	"Lease":              true,
	"PodMetrics":         true,
	"ReplicaSet":         true, // collapsed: Deployment -> Pod chain is enough
	// RBAC plumbing: rarely the point of an architecture diagram.
	"Role":               true,
	"RoleBinding":        true,
	"ClusterRole":        true,
	"ClusterRoleBinding": true,
	// Cluster-scoped plumbing that explodes the node count.
	"CustomResourceDefinition":       true,
	"APIService":                     true,
	"FlowSchema":                     true,
	"PriorityLevelConfiguration":     true,
	"ComponentStatus":                true,
	"RuntimeClass":                   true,
	"PriorityClass":                  true,
	"ValidatingWebhookConfiguration": true,
	"MutatingWebhookConfiguration":   true,
}

// refOnlyKinds are kept only when something references them (i.e. they have at
// least one edge); isolated instances are dropped as noise.
var refOnlyKinds = map[string]bool{
	"ConfigMap":      true,
	"Secret":         true,
	"ServiceAccount": true,
}

// Prune removes noise nodes (and their dangling edges) to keep diagrams legible.
// When keepAll is true it is a no-op. ReplicaSet removal rewires Deployment->Pod
// owner edges so the ownership chain is preserved.
func Prune(g *Graph, keepAll bool) {
	if keepAll {
		return
	}

	// Rewire owner edges through removed ReplicaSets so Deployments still point
	// at their Pods.
	rewireThrough(g, "ReplicaSet")

	// Count edges per node to decide ref-only retention.
	deg := map[string]int{}
	for _, e := range g.Edges {
		deg[e.From]++
		deg[e.To]++
	}

	drop := map[string]bool{}
	for id, n := range g.Nodes {
		if noiseKinds[n.Kind] {
			drop[id] = true
			continue
		}
		if refOnlyKinds[n.Kind] && deg[id] == 0 {
			drop[id] = true
		}
	}

	if len(drop) == 0 {
		return
	}
	for id := range drop {
		delete(g.Nodes, id)
	}
	kept := g.Edges[:0]
	for _, e := range g.Edges {
		if drop[e.From] || drop[e.To] {
			continue
		}
		kept = append(kept, e)
	}
	g.Edges = kept
}

// rewireThrough connects the parents of nodes of the given kind directly to
// those nodes' children (owner edges), so removing the intermediate kind keeps
// the chain connected.
func rewireThrough(g *Graph, kind string) {
	for id, n := range g.Nodes {
		if n.Kind != kind {
			continue
		}
		var parents, children []string
		for _, e := range g.Edges {
			if e.Kind != RelOwner {
				continue
			}
			if e.To == id {
				parents = append(parents, e.From)
			}
			if e.From == id {
				children = append(children, e.To)
			}
		}
		for _, p := range parents {
			for _, c := range children {
				g.AddEdge(p, c, RelOwner, "")
			}
		}
	}
}
