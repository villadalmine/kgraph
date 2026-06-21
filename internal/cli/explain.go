package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/graph"
	"github.com/villadalmine/kgraph/internal/layers"
	"github.com/villadalmine/kgraph/internal/llm"
)

const explainSystem = `You are a senior Kubernetes / platform engineer. You are given an
inventory of resources and their relationships from a single Kubernetes namespace
(optionally a single abstraction layer). Explain, in clear and concise Markdown:
1. What this namespace/stack does at a high level.
2. The main components and their roles (refer to the real resource names).
3. How they connect — control and data flow.
4. Anything notable, risky, or worth documenting.
Only use resources that appear in the input; never invent resources.`

const askSystem = `You are a Kubernetes expert. Answer the user's question using ONLY the
provided resource graph of a namespace. Be concise and concrete, citing real
resource names. If the answer is not determinable from the graph, say so.`

// scopeContext collects a namespace, builds the (optionally layer-focused) graph
// and returns its textual description plus the count of nodes.
func scopeContext(ctx context.Context, ns, layer string) (string, int, error) {
	c, err := collect.New(flagKubeconfig, flagContext)
	if err != nil {
		return "", 0, err
	}
	res, err := c.Namespace(ctx, ns)
	if err != nil {
		return "", 0, err
	}
	if len(res.Objects) == 0 {
		return "", 0, fmt.Errorf("no readable resources found in namespace %q", ns)
	}
	g := graph.Build(res.Objects)
	graph.Prune(g, false)
	if layer != "" {
		rule, ok := layers.Find(layer)
		if !ok {
			return "", 0, fmt.Errorf("unknown layer %q", layer)
		}
		seed := func(n *graph.Node) bool { return rule.Matches(n.Group, n.Kind) }
		g = g.Subgraph(seed, 1, layer)
		if len(g.Nodes) == 0 {
			return "", 0, fmt.Errorf("layer %q not present in namespace %q", layer, ns)
		}
	}
	header := fmt.Sprintf("NAMESPACE: %s\n", ns)
	if layer != "" {
		header += fmt.Sprintf("LAYER: %s\n", layer)
	}
	return header + "\n" + graph.Describe(g), len(g.Nodes), nil
}

func newExplainCmd() *cobra.Command {
	var (
		layer  string
		model  string
		output string
	)
	cmd := &cobra.Command{
		Use:   "explain <namespace>",
		Short: "Use an LLM to explain a namespace's architecture (needs OPENROUTER_API_KEY)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			provider, err := llm.New(model)
			if err != nil {
				return err
			}
			desc, n, err := scopeContext(ctx, args[0], layer)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "asking %s about %d resources...\n", provider.Model(), n)
			out, err := provider.Complete(ctx, explainSystem, desc)
			if err != nil {
				return err
			}
			return emitText(output, out)
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "", "focus a single detected stack")
	cmd.Flags().StringVar(&model, "model", "", "OpenRouter model (default qwen3-next free / $OPENROUTER_MODEL)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write explanation to a file (default stdout)")
	return cmd
}

func newAskCmd() *cobra.Command {
	var (
		layer string
		model string
	)
	cmd := &cobra.Command{
		Use:   "ask <namespace> <question>",
		Short: "Ask a natural-language question about a namespace (needs OPENROUTER_API_KEY)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ns := args[0]
			question := joinArgs(args[1:])
			provider, err := llm.New(model)
			if err != nil {
				return err
			}
			desc, n, err := scopeContext(ctx, ns, layer)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "asking %s about %d resources...\n", provider.Model(), n)
			out, err := provider.Complete(ctx, askSystem, desc+"\n\nQUESTION: "+question)
			if err != nil {
				return err
			}
			return emitText("", out)
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "", "focus a single detected stack")
	cmd.Flags().StringVar(&model, "model", "", "OpenRouter model (default qwen3-next free / $OPENROUTER_MODEL)")
	return cmd
}

func emitText(output, text string) error {
	if output == "" {
		fmt.Println(text)
		return nil
	}
	if err := os.WriteFile(output, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", output)
	return nil
}

func joinArgs(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += " "
		}
		s += p
	}
	return s
}
