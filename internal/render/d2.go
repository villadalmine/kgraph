// Package render turns a graph into D2 source ("diagram as code") and renders
// it to SVG using the embedded d2 library (no external binaries required).
package render

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/villadalmine/kgraph/internal/graph"
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

var idSanitizer = regexp.MustCompile(`[^A-Za-z0-9_]`)

// D2 builds styled D2 source from a graph. Nodes are grouped into containers by
// their assigned Layer, or by Kind when no layer is set. Nodes are coloured by
// resource category and edges styled by relation type. When icons is true,
// Kubernetes resource icons are attached (requires network at render time).
func D2(g *graph.Graph, title string, icons bool) string {
	nodes := g.SortedNodes()

	type d2node struct {
		key       string
		container string
		path      string
		label     string
		kind      string
		layered   bool
	}
	d2nodes := map[string]d2node{}
	usedLeaf := map[string]bool{}

	for i, n := range nodes {
		container := sanitize(groupKey(n))
		leaf := sanitize(n.Kind + "_" + n.Name)
		full := container + "." + leaf
		for usedLeaf[full] {
			leaf = fmt.Sprintf("%s_%d", leaf, i)
			full = container + "." + leaf
		}
		usedLeaf[full] = true

		layered := n.Layer != ""
		// In kind-grouped containers the kind is already the title, so the node
		// label is just the name; in layer containers we keep kind + name.
		label := n.Name
		if layered {
			label = fmt.Sprintf("%s\\n%s", n.Kind, n.Name)
		}
		d2nodes[n.ID] = d2node{
			key: leaf, container: container, path: full,
			label: label, kind: n.Kind, layered: layered,
		}
	}

	byContainer := map[string][]string{}
	containerLabel := map[string]string{}
	for _, n := range nodes {
		dn := d2nodes[n.ID]
		containerLabel[dn.container] = groupKey(n)
		fill, stroke := categoryColor(n.Kind)
		strokeWidth := 1
		label := dn.label
		if n.Alert {
			stroke = "#DC2626" // unprotected: red border
			strokeWidth = 3
			label += " ⚠"
		}
		if n.Note != "" {
			label += "\\n" + n.Note // e.g. throughput annotation
		}
		var iconLine string
		if icons {
			if url := iconFor(n.Kind); url != "" {
				iconLine = fmt.Sprintf("    icon: %s\n", url)
			}
		}
		decl := fmt.Sprintf("  %s: \"%s\" {\n    shape: %s\n%s    style.fill: \"%s\"\n    style.stroke: \"%s\"\n    style.stroke-width: %d\n    style.border-radius: 6\n  }",
			dn.key, label, shapeFor(n.Kind), iconLine, fill, stroke, strokeWidth)
		byContainer[dn.container] = append(byContainer[dn.container], decl)
	}

	var b strings.Builder
	b.WriteString("direction: right\n")
	if title != "" {
		fmt.Fprintf(&b, "title: |md\n  # %s\n| { near: top-center; style.font-size: 24 }\n\n", title)
	}

	containers := make([]string, 0, len(byContainer))
	for c := range byContainer {
		containers = append(containers, c)
	}
	sort.Strings(containers)
	for _, c := range containers {
		fmt.Fprintf(&b, "%s: \"%s\" {\n", c, containerLabel[c])
		b.WriteString("  style.fill: \"#FAFAFA\"\n  style.stroke: \"#D4D4D8\"\n  style.font-size: 18\n  style.border-radius: 8\n")
		for _, decl := range byContainer[c] {
			b.WriteString(decl)
			b.WriteString("\n")
		}
		b.WriteString("}\n\n")
	}

	for _, e := range g.Edges {
		from, ok1 := d2nodes[e.From]
		to, ok2 := d2nodes[e.To]
		if !ok1 || !ok2 {
			continue
		}
		label := e.Note
		if label == "" && e.Kind != graph.RelOwner {
			label = string(e.Kind)
		}
		style := edgeStyle(e.Kind)
		if e.Kind == graph.RelFlow {
			style = flowEdgeStyle(e)
		}
		fmt.Fprintf(&b, "%s -> %s: \"%s\" {\n%s}\n", from.path, to.path, label, style)
	}

	return b.String()
}

// SVG compiles D2 source and renders it to an SVG byte slice.
func SVG(ctx context.Context, d2src string) ([]byte, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("ruler: %w", err)
	}
	layoutResolver := func(string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(30)),
		ThemeID: &d2themescatalog.NeutralDefault.ID,
	}
	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}
	diagram, _, err := d2lib.Compile(log.WithDefault(ctx), d2src, compileOpts, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("compile d2: %w", err)
	}
	out, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("render svg: %w", err)
	}
	return out, nil
}

func groupKey(n *graph.Node) string {
	if n.Layer != "" {
		return n.Layer
	}
	return n.Kind
}

func sanitize(s string) string {
	s = idSanitizer.ReplaceAllString(s, "_")
	if s == "" {
		return "x"
	}
	return s
}

// shapeFor maps a kind to a D2 shape for quick visual differentiation.
func shapeFor(kind string) string {
	switch kind {
	case "Pod":
		return "circle"
	case "Service":
		return "hexagon"
	case "ConfigMap", "Secret":
		return "page"
	case "PersistentVolumeClaim", "PersistentVolume":
		return "cylinder"
	case "Ingress", "HTTPRoute", "GRPCRoute", "TCPRoute", "Gateway", "External":
		return "cloud"
	default:
		return "rectangle"
	}
}

// categoryColor returns (fill, stroke) hex colours grouping kinds by role, so
// the same kind of thing always reads the same colour across diagrams.
// CategoryColor returns the (fill, stroke) palette for a kind, so other
// renderers (e.g. the interactive web view) share one source of truth.
func CategoryColor(kind string) (fill, stroke string) { return categoryColor(kind) }

func categoryColor(kind string) (string, string) {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob":
		return "#DBEAFE", "#2563EB" // workloads — blue
	case "Pod":
		return "#EFF6FF", "#3B82F6" // pods — light blue
	case "Service":
		return "#DCFCE7", "#16A34A" // services — green
	case "Ingress", "HTTPRoute", "GRPCRoute", "TCPRoute", "Gateway", "Endpoints", "EndpointSlice":
		return "#D1FAE5", "#059669" // networking — emerald
	case "ConfigMap", "Secret":
		return "#FEF3C7", "#D97706" // config — amber
	case "PersistentVolumeClaim", "PersistentVolume", "StorageClass":
		return "#EDE9FE", "#7C3AED" // storage — purple
	case "ServiceAccount", "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding":
		return "#F3F4F6", "#6B7280" // rbac — grey
	case "External":
		return "#E5E7EB", "#374151" // external / world — dark grey
	default:
		return "#FCE7F3", "#DB2777" // custom resources — pink
	}
}

// iconBase is the Kubernetes community icon set (unlabeled SVGs).
const iconBase = "https://raw.githubusercontent.com/kubernetes/community/master/icons/svg/resources/unlabeled/"

// IconName maps a kind to the Kubernetes community icon base name (e.g.
// "deploy"), "crd" for unknown custom resources, or "" for synthetic nodes
// (External). It is the single source of truth shared by the SVG renderer and
// the interactive web view.
func IconName(kind string) string {
	if kind == "External" || kind == "" {
		return ""
	}
	name := map[string]string{
		"Pod":                   "pod",
		"Deployment":            "deploy",
		"StatefulSet":           "sts",
		"DaemonSet":             "ds",
		"ReplicaSet":            "rs",
		"Job":                   "job",
		"CronJob":               "cronjob",
		"Service":               "svc",
		"Ingress":               "ing",
		"NetworkPolicy":         "netpol",
		"ConfigMap":             "cm",
		"Secret":                "secret",
		"PersistentVolumeClaim": "pvc",
		"PersistentVolume":      "pv",
		"ServiceAccount":        "sa",
		"Role":                  "role",
		"RoleBinding":           "rb",
		"Namespace":             "ns",
		"Endpoints":             "ep",
	}[kind]
	if name == "" {
		name = "crd" // generic icon for custom resources
	}
	return name
}

// iconFor maps a kind to a Kubernetes icon URL, or "" if none is known.
func iconFor(kind string) string {
	name := IconName(kind)
	if name == "" {
		return ""
	}
	return iconBase + name + ".svg"
}

// flowEdgeStyle styles an observed-traffic edge: stroke width scales with
// volume (log), colour is red for dropped traffic and blue for allowed.
func flowEdgeStyle(e graph.Edge) string {
	width := 1 + int(math.Log10(float64(e.Weight+1))*2)
	if width > 8 {
		width = 8
	}
	color := "#2563EB" // allowed & governed
	dash := ""
	switch {
	case e.Dropped:
		color = "#DC2626" // dropped / denied by policy
	case e.Error:
		color = "#B91C1C" // L7 server errors (5xx)
	case e.Gap:
		color = "#D97706" // allowed but to an unprotected workload (gap)
		dash = "  style.stroke-dash: 4\n"
	}
	return fmt.Sprintf("  style.stroke: \"%s\"\n  style.stroke-width: %d\n%s  style.font-size: 11\n", color, width, dash)
}

func edgeStyle(k graph.RelKind) string {
	switch k {
	case graph.RelOwner:
		return "  style.stroke: \"#1F2937\"\n  style.stroke-width: 2\n"
	case graph.RelSelector:
		return "  style.stroke: \"#16A34A\"\n  style.stroke-dash: 3\n  style.font-size: 11\n"
	case graph.RelRef:
		return "  style.stroke: \"#D97706\"\n  style.stroke-dash: 5\n  style.font-size: 11\n"
	default:
		return "  style.stroke: \"#9CA3AF\"\n  style.stroke-dash: 2\n  style.font-size: 11\n"
	}
}
