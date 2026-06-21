package docs

import (
	"strings"
	"testing"
	"time"
)

func TestMarkdown(t *testing.T) {
	p := Page{
		Title:       "Namespace: demo",
		ObjectCount: 10,
		GraphNodes:  6,
		Overview:    "This is the AI overview.",
		Sections: []Section{
			{Name: "argocd", Desc: "ArgoCD apps", Count: 3, Nodes: 4, Image: "demo-argocd.svg"},
		},
		Inventory: []KindCount{{Kind: "Pod", Count: 4}},
		Generated: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC),
	}
	md := Markdown(p)

	for _, want := range []string{
		"# Namespace: demo",
		"## Overview",
		"This is the AI overview.",
		"## Layers",
		"![argocd diagram](demo-argocd.svg)",
		"## Inventory",
		"| Pod | 4 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestMarkdownNoOverview(t *testing.T) {
	md := Markdown(Page{Title: "Namespace: x", Generated: time.Now()})
	if strings.Contains(md, "## Overview") {
		t.Errorf("overview section should be omitted when empty")
	}
}

// Architecture overview renders before the layer breakdown (spec 0003, AC3).
func TestMarkdownArchitecture(t *testing.T) {
	p := Page{
		Title:        "Namespace: demo",
		Architecture: &Section{Name: "architecture", Desc: "Whole namespace", Count: 10, Nodes: 6, Image: "demo-architecture.svg"},
		Sections:     []Section{{Name: "argocd", Desc: "ArgoCD apps", Count: 3, Nodes: 4, Image: "demo-argocd.svg"}},
		Generated:    time.Now(),
	}
	md := Markdown(p)
	if !strings.Contains(md, "## Architecture") || !strings.Contains(md, "![architecture diagram](demo-architecture.svg)") {
		t.Errorf("architecture section/image missing:\n%s", md)
	}
	if strings.Index(md, "## Architecture") > strings.Index(md, "## Layers") {
		t.Errorf("architecture must render before layers")
	}
}

// A section with an empty Image is listed in the table but gets no diagram block
// (spec 0003, AC4).
func TestMarkdownFoldedSection(t *testing.T) {
	p := Page{
		Title: "Namespace: demo",
		Sections: []Section{
			{Name: "monitoring", Desc: "stack", Count: 5, Nodes: 4, Image: "demo-monitoring.svg"},
			{Name: "gateway", Desc: "Gateway API", Count: 1, Nodes: 1}, // folded, no image
		},
		Generated: time.Now(),
	}
	md := Markdown(p)
	if !strings.Contains(md, "| gateway | 1 |") {
		t.Errorf("folded layer should appear in the table:\n%s", md)
	}
	if strings.Contains(md, "## gateway") {
		t.Errorf("folded layer should not get its own diagram block:\n%s", md)
	}
}

// Observed-traffic + security sections render when set (spec 0004, AC1/AC3).
func TestMarkdownTrafficSecurity(t *testing.T) {
	p := Page{
		Title:     "Namespace: demo",
		Traffic:   &Section{Name: "traffic", Desc: "Observed flows", Count: 100, Nodes: 8, Image: "demo-traffic.svg"},
		Security:  &Security{Policies: 2, DeniedFlows: 3, GapFlows: 1, Unprotected: []string{"web", "db"}},
		Generated: time.Now(),
	}
	md := Markdown(p)
	for _, want := range []string{
		"## Observed traffic", "![observed traffic](demo-traffic.svg)",
		"## Security posture", "| Policies in effect | 2 |", "| Unprotected workloads | 2 |",
		"- `web`", "- `db`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q:\n%s", want, md)
		}
	}
}

// When traffic was requested but unavailable, a note renders and no image (AC2).
func TestMarkdownTrafficNote(t *testing.T) {
	md := Markdown(Page{Title: "x", TrafficNote: "Observed traffic unavailable: Hubble off", Generated: time.Now()})
	if !strings.Contains(md, "## Observed traffic") || !strings.Contains(md, "Hubble off") {
		t.Errorf("note not rendered:\n%s", md)
	}
	if strings.Contains(md, "![observed traffic]") {
		t.Errorf("no image should render without Traffic:\n%s", md)
	}
}

// Empty Unprotected shows the all-covered line (AC3).
func TestMarkdownSecurityAllCovered(t *testing.T) {
	md := Markdown(Page{Title: "x", Security: &Security{Policies: 5}, Generated: time.Now()})
	if !strings.Contains(md, "All observed workloads are selected by at least one policy") {
		t.Errorf("all-covered line missing:\n%s", md)
	}
}
