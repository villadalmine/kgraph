package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/docs"
	"github.com/villadalmine/kgraph/internal/graph"
	"github.com/villadalmine/kgraph/internal/layers"
	"github.com/villadalmine/kgraph/internal/llm"
	"github.com/villadalmine/kgraph/internal/preflight"
	"github.com/villadalmine/kgraph/internal/render"
	"github.com/villadalmine/kgraph/internal/traffic"
)

func newDocCmd() *cobra.Command {
	var (
		outDir      string
		keepAll     bool
		icons       bool
		useAI       bool
		model       string
		showTraffic bool
	)
	cmd := &cobra.Command{
		Use:   "doc <namespace>",
		Short: "Generate a Markdown page with one diagram per detected layer (docs-as-code)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns := args[0]
			ctx := context.Background()

			c, err := collect.New(flagKubeconfig, flagContext)
			if err != nil {
				return err
			}
			res, err := c.Namespace(ctx, ns)
			if err != nil {
				return err
			}
			for _, w := range res.Warnings {
				fmt.Fprintln(os.Stderr, "warn:", w)
			}
			if len(res.Objects) == 0 {
				return fmt.Errorf("no readable resources found in namespace %q", ns)
			}

			if outDir == "" {
				outDir = filepath.Join("kgraph-docs", ns)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", outDir, err)
			}

			g := graph.Build(res.Objects)
			graph.Prune(g, keepAll)

			page := docs.Page{
				Title:       "Namespace: " + ns,
				ObjectCount: len(res.Objects),
				GraphNodes:  len(g.Nodes),
				Inventory:   inventory(g),
				Generated:   time.Now(),
			}

			if useAI {
				provider, err := llm.New(model)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "generating AI overview with %s...\n", provider.Model())
				overview, err := provider.Complete(ctx, explainSystem, "NAMESPACE: "+ns+"\n\n"+graph.Describe(g))
				if err != nil {
					return fmt.Errorf("AI overview: %w", err)
				}
				page.Overview = overview
			}

			// Lead with a whole-namespace architecture diagram (D1).
			archImg := ns + "-architecture.svg"
			if err := renderToFile(ctx, g, "Namespace: "+ns+" — architecture", filepath.Join(outDir, archImg), icons); err != nil {
				return err
			}
			page.Architecture = &docs.Section{
				Name: "architecture", Desc: "Whole namespace", Count: len(res.Objects), Nodes: len(g.Nodes), Image: archImg,
			}
			fmt.Fprintf(os.Stderr, "rendered architecture overview (%d nodes)\n", len(g.Nodes))

			// Layer sections, ordered by significance tier (D2/D3) and curated by
			// size (D4): a layer earns its own diagram only when it has >=
			// minLayerResources of its own resources; smaller ones are folded
			// (table mention only — they're already in the architecture overview).
			const minLayerResources = 2
			for _, d := range layers.Rank(layers.Detect(res.Objects)) {
				rule, _ := layers.Find(d.Name)
				seed := func(n *graph.Node) bool { return rule.Matches(n.Group, n.Kind) }
				sub := g.Subgraph(seed, 1, d.Name)
				if len(sub.Nodes) == 0 {
					continue
				}
				sec := docs.Section{Name: d.Name, Desc: d.Desc, Count: d.Count, Nodes: len(sub.Nodes)}
				if d.Count >= minLayerResources {
					img := fmt.Sprintf("%s-%s.svg", ns, d.Name)
					title := fmt.Sprintf("Namespace: %s — layer: %s", ns, d.Name)
					if err := renderToFile(ctx, sub, title, filepath.Join(outDir, img), icons); err != nil {
						return err
					}
					sec.Image = img
					fmt.Fprintf(os.Stderr, "rendered layer %s (%d resources, %d nodes)\n", d.Name, d.Count, len(sub.Nodes))
				} else {
					fmt.Fprintf(os.Stderr, "folded layer %s (%d resource) into overview\n", d.Name, d.Count)
				}
				page.Sections = append(page.Sections, sec)
			}

			// Optional observed-traffic + security posture (spec 0004). Opt-in and
			// graceful: never fail the doc if Hubble is unavailable or quiet.
			if showTraffic {
				addTrafficSections(ctx, c, ns, outDir, icons, &page)
			}

			mdPath := filepath.Join(outDir, ns+".md")
			if err := os.WriteFile(mdPath, []byte(docs.Markdown(page)), 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", mdPath, err)
			}
			fmt.Fprintf(os.Stderr, "wrote %s (+%d diagrams)\n", mdPath, len(page.Sections))
			return nil
		},
	}
	cmd.Flags().StringVarP(&outDir, "out", "o", "", "output directory (default kgraph-docs/<namespace>)")
	cmd.Flags().BoolVar(&keepAll, "all", false, "include noisy kinds")
	cmd.Flags().BoolVar(&icons, "icons", false, "attach Kubernetes resource icons (requires network at render)")
	cmd.Flags().BoolVar(&useAI, "ai", false, "add an AI-generated overview (needs OPENROUTER_API_KEY)")
	cmd.Flags().StringVar(&model, "model", "", "OpenRouter model for --ai")
	cmd.Flags().BoolVar(&showTraffic, "traffic", false, "add observed-traffic + security-posture sections (needs Cilium Hubble)")
	return cmd
}

// docTrafficFlows is the flow sample size used for the doc's traffic section.
const docTrafficFlows = 2000

// addTrafficSections folds observed traffic (Hubble) and the policy-coverage
// posture into the page. It is opt-in and graceful: any problem (Hubble absent,
// no flows) becomes a note on the page rather than a failed doc. See
// specs/0004-autodoc-traffic-security.md.
func addTrafficSections(ctx context.Context, c *collect.Collector, ns, outDir string, icons bool, page *docs.Page) {
	if err := preflight.RequireHubble(ctx, c); err != nil {
		page.TrafficNote = "Observed traffic unavailable: " + err.Error()
		fmt.Fprintln(os.Stderr, "warn: skipping traffic section:", err)
		return
	}
	fmt.Fprintln(os.Stderr, "port-forwarding hubble-relay for traffic section...")
	addr, stop, err := traffic.PortForwardRelay(ctx, c.RESTConfig())
	if err != nil {
		page.TrafficNote = "Observed traffic unavailable: " + err.Error()
		return
	}
	defer stop()

	g, n, _, err := traffic.FetchFlows(ctx, addr, ns, docTrafficFlows, false)
	if err != nil {
		page.TrafficNote = "Observed traffic unavailable: " + err.Error()
		return
	}
	if len(g.Nodes) == 0 {
		page.TrafficNote = fmt.Sprintf("No flows observed for %q in the last %d samples.", ns, docTrafficFlows)
		return
	}

	// Security posture: analyze policy coverage over the observed flows (marks
	// unprotected workloads and gap/denied edges on g as a side effect).
	if sum, err := traffic.AnalyzePolicies(ctx, c.Dynamic(), ns, g); err == nil {
		page.Security = &docs.Security{
			Policies:    sum.Policies,
			DeniedFlows: sum.DeniedFlows,
			GapFlows:    sum.GapFlows,
			Unprotected: sum.Unprotected,
		}
	} else {
		fmt.Fprintln(os.Stderr, "warn: policy analysis failed:", err)
	}

	img := ns + "-traffic.svg"
	title := fmt.Sprintf("Traffic: %s (last %d flows)", ns, docTrafficFlows)
	if err := renderToFile(ctx, g, title, filepath.Join(outDir, img), icons); err != nil {
		page.TrafficNote = "Traffic diagram render failed: " + err.Error()
		return
	}
	page.Traffic = &docs.Section{Name: "traffic", Desc: "Observed flows", Count: n, Nodes: len(g.Nodes), Image: img}
	fmt.Fprintf(os.Stderr, "rendered traffic section (%d flows, %d nodes)\n", n, len(g.Nodes))
}

func renderToFile(ctx context.Context, g *graph.Graph, title, path string, icons bool) error {
	svg, err := render.SVG(ctx, render.D2(g, title, icons))
	if err != nil {
		return fmt.Errorf("rendering %s: %w", path, err)
	}
	return os.WriteFile(path, svg, 0o644)
}

// inventory counts nodes per kind, sorted by count desc.
func inventory(g *graph.Graph) []docs.KindCount {
	counts := map[string]int{}
	for _, n := range g.Nodes {
		counts[n.Kind]++
	}
	out := make([]docs.KindCount, 0, len(counts))
	for k, c := range counts {
		out = append(out, docs.KindCount{Kind: k, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
