package graph

import (
	"fmt"
	"sort"
	"strings"
)

// Describe renders a compact textual representation of the graph for use as LLM
// retrieval context (GraphRAG). It lists resources grouped by kind and then the
// relationships, keeping it deterministic and token-efficient.
func Describe(g *Graph) string {
	var b strings.Builder

	// Resources grouped by kind.
	byKind := map[string][]string{}
	for _, n := range g.SortedNodes() {
		byKind[n.Kind] = append(byKind[n.Kind], n.Name)
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	b.WriteString("RESOURCES (by kind):\n")
	for _, k := range kinds {
		names := byKind[k]
		fmt.Fprintf(&b, "- %s (%d): %s\n", k, len(names), strings.Join(names, ", "))
	}

	// Relationships.
	if len(g.Edges) > 0 {
		b.WriteString("\nRELATIONSHIPS:\n")
		lines := make([]string, 0, len(g.Edges))
		for _, e := range g.Edges {
			from, to := g.Nodes[e.From], g.Nodes[e.To]
			if from == nil || to == nil {
				continue
			}
			rel := string(e.Kind)
			if e.Note != "" {
				rel = e.Note
			}
			lines = append(lines, fmt.Sprintf("- %s/%s --%s--> %s/%s",
				from.Kind, from.Name, rel, to.Kind, to.Name))
		}
		sort.Strings(lines)
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}

	return b.String()
}
