package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/graph"
	"github.com/villadalmine/kgraph/internal/preflight"
	"github.com/villadalmine/kgraph/internal/traffic"
)

func newNSCmd() *cobra.Command {
	var (
		output      string
		format      string
		keepAll     bool
		layer       string
		icons       bool
		withTraffic bool
		last        int
	)
	cmd := &cobra.Command{
		Use:   "ns <namespace>",
		Short: "Build a diagram of all resources in a namespace",
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
			fmt.Fprintf(os.Stderr, "collected %d objects from namespace %q\n", len(res.Objects), ns)

			var overlay func(*graph.Graph) error
			if withTraffic {
				overlay = trafficOverlay(ctx, c, ns, last)
			}
			return emit(ctx, res.Objects, ns, "Namespace: "+ns, "namespace", layer, format, output, keepAll, icons, overlay)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default <namespace>.<format>)")
	cmd.Flags().StringVar(&format, "format", "svg", "output format: svg|d2")
	cmd.Flags().BoolVar(&keepAll, "all", false, "include noisy kinds (Secrets, EndpointSlices, ReplicaSets, ...)")
	cmd.Flags().StringVar(&layer, "layer", "", "focus a single detected stack (see 'kgraph layers <ns>')")
	cmd.Flags().BoolVar(&icons, "icons", false, "attach Kubernetes resource icons (requires network at render)")
	cmd.Flags().BoolVar(&withTraffic, "traffic", false, "overlay observed Hubble traffic onto the topology")
	cmd.Flags().IntVar(&last, "last", 2000, "with --traffic: number of recent flows to sample")
	return cmd
}

// trafficOverlay returns an emit() overlay hook that fetches Hubble flows for ns
// and merges them onto the topology graph.
func trafficOverlay(ctx context.Context, c *collect.Collector, ns string, last int) func(*graph.Graph) error {
	return func(g *graph.Graph) error {
		if err := preflight.RequireHubble(ctx, c); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "port-forwarding hubble-relay for traffic overlay...")
		addr, stop, err := traffic.PortForwardRelay(ctx, c.RESTConfig())
		if err != nil {
			return err
		}
		defer stop()
		fg, n, _, err := traffic.FetchFlows(ctx, addr, ns, last, false)
		if err != nil {
			return err
		}
		graph.Overlay(g, fg)
		fmt.Fprintf(os.Stderr, "overlaid %d flows\n", n)
		return nil
	}
}
