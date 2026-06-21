package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/layers"
)

func newLayersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "layers <namespace>",
		Short: "List the abstraction layers (stacks) detected in a namespace",
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

			found := layers.Detect(res.Objects)
			if len(found) == 0 {
				fmt.Printf("No known stacks detected in namespace %q (%d objects).\n", ns, len(res.Objects))
				return nil
			}
			fmt.Printf("Stacks detected in namespace %q:\n\n", ns)
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "LAYER\tRESOURCES\tDESCRIPTION")
			for _, d := range found {
				fmt.Fprintf(tw, "%s\t%d\t%s\n", d.Name, d.Count, d.Desc)
			}
			tw.Flush()
			fmt.Printf("\nRender one with:  kgraph ns %s --layer <LAYER> -o out.svg\n", ns)
			return nil
		},
	}
	return cmd
}
