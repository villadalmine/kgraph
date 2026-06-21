package graph

// Subgraph returns a new graph containing every node for which seed(node) is
// true, plus all nodes reachable within `hops` edges of a seed (treating edges
// as undirected). Seed nodes get their Layer set to layerName so the renderer
// can group them together. Edges between kept nodes are preserved.
func (g *Graph) Subgraph(seed func(*Node) bool, hops int, layerName string) *Graph {
	// Build undirected adjacency.
	adj := map[string][]string{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	keep := map[string]bool{}
	frontier := []string{}
	for id, n := range g.Nodes {
		if seed(n) {
			keep[id] = true
			frontier = append(frontier, id)
		}
	}

	for h := 0; h < hops; h++ {
		var next []string
		for _, id := range frontier {
			for _, nb := range adj[id] {
				if !keep[nb] {
					keep[nb] = true
					next = append(next, nb)
				}
			}
		}
		if len(next) == 0 {
			break
		}
		frontier = next
	}

	sub := New()
	for id := range keep {
		n := g.Nodes[id]
		copy := *n
		if seed(n) {
			copy.Layer = layerName
		}
		sub.Nodes[id] = &copy
	}
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			sub.AddEdge(e.From, e.To, e.Kind, e.Note)
		}
	}
	return sub
}
