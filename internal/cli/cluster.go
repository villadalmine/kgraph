package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
)

func newClusterCmd() *cobra.Command {
	var (
		output  string
		format  string
		keepAll bool
		layer   string
		icons   bool
	)
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Build a diagram of cluster-scoped resources (Crossplane, CAPI, CRDs, ...)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			c, err := collect.New(flagKubeconfig, flagContext)
			if err != nil {
				return err
			}
			res, err := c.Cluster(ctx)
			if err != nil {
				return err
			}
			for _, w := range res.Warnings {
				fmt.Fprintln(os.Stderr, "warn:", w)
			}
			if len(res.Objects) == 0 {
				return fmt.Errorf("no readable cluster-scoped resources found")
			}
			fmt.Fprintf(os.Stderr, "collected %d cluster-scoped objects\n", len(res.Objects))

			return emit(ctx, res.Objects, "cluster", "Cluster-scoped", "cluster", layer, format, output, keepAll, icons)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default cluster.<format>)")
	cmd.Flags().StringVar(&format, "format", "svg", "output format: svg|d2")
	cmd.Flags().BoolVar(&keepAll, "all", false, "include noisy kinds (CRDs, APIServices, ClusterRoles, ...)")
	cmd.Flags().StringVar(&layer, "layer", "", "focus a single detected stack (e.g. crossplane, capi)")
	cmd.Flags().BoolVar(&icons, "icons", false, "attach Kubernetes resource icons (requires network at render)")
	return cmd
}
