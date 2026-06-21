package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/preflight"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check cluster capabilities and report what each feature needs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			c, err := collect.New(flagKubeconfig, flagContext)
			if err != nil {
				return err
			}
			checks := preflight.Run(ctx, c)

			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			for _, ch := range checks {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", ch.Status.Symbol(), ch.Name, ch.Detail)
			}
			tw.Flush()

			// Remediation lines for anything not OK.
			for _, ch := range checks {
				if ch.Status != preflight.OK && ch.Remediation != "" {
					fmt.Printf("\n  → %s: %s\n", ch.Name, ch.Remediation)
				}
			}
			return nil
		},
	}
	return cmd
}
