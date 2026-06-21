package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/villadalmine/kgraph/internal/graph"
	"github.com/villadalmine/kgraph/internal/layers"
	"github.com/villadalmine/kgraph/internal/render"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// emit builds the graph from objs, optionally focuses a layer, renders it in the
// requested format and writes it out. baseName is used for the default output
// filename and baseTitle for the diagram title; scopeKind is "namespace" or
// "cluster" for messages.
func emit(ctx context.Context, objs []*unstructured.Unstructured, baseName, baseTitle, scopeKind, layer, format, output string, keepAll, icons bool, overlay ...func(*graph.Graph) error) error {
	g := graph.Build(objs)
	graph.Prune(g, keepAll)

	title := baseTitle
	if layer != "" {
		rule, ok := layers.Find(layer)
		if !ok {
			return fmt.Errorf("unknown layer %q; run 'kgraph layers' to see options", layer)
		}
		seed := func(n *graph.Node) bool { return rule.Matches(n.Group, n.Kind) }
		g = g.Subgraph(seed, 1, layer)
		if len(g.Nodes) == 0 {
			return fmt.Errorf("layer %q not present in this %s", layer, scopeKind)
		}
		title = baseTitle + " — layer: " + layer
	} else if found := layers.Detect(objs); len(found) > 0 {
		names := make([]string, 0, len(found))
		for _, d := range found {
			names = append(names, d.Name)
		}
		fmt.Fprintf(os.Stderr, "detected layers: %s (use --layer to focus one)\n", strings.Join(names, ", "))
	}

	for _, ov := range overlay {
		if ov == nil {
			continue
		}
		if err := ov(g); err != nil {
			return err
		}
	}

	if n := len(g.Nodes); n > 200 {
		fmt.Fprintf(os.Stderr, "note: %d nodes; SVG layout may be slow. Use --layer to focus a stack, or --format d2.\n", n)
	}
	fmt.Fprintf(os.Stderr, "graph: %d nodes, %d edges\n", len(g.Nodes), len(g.Edges))

	d2src := render.D2(g, title, icons)
	switch strings.ToLower(format) {
	case "d2":
		return write(defaultOut(output, baseName, "d2"), []byte(d2src))
	case "svg", "":
		svg, err := render.SVG(ctx, d2src)
		if err != nil {
			return err
		}
		return write(defaultOut(output, baseName, "svg"), svg)
	default:
		return fmt.Errorf("unknown format %q (use svg or d2)", format)
	}
}

func defaultOut(output, base, ext string) string {
	if output != "" {
		return output
	}
	return fmt.Sprintf("%s.%s", base, ext)
}

func write(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Fprintln(os.Stderr, "wrote", path)
	return nil
}
