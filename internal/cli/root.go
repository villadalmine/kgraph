// Package cli wires the kgraph command-line interface.
package cli

import (
	"bufio"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// persistent flags shared by subcommands.
var (
	flagKubeconfig string
	flagContext    string
)

// NewRoot builds the root cobra command.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "kgraph",
		Short:         "Visualize and document Kubernetes namespaces as diagrams",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Load a local .env (e.g. OPENROUTER_API_KEY) before any command runs.
		PersistentPreRun: func(*cobra.Command, []string) { loadDotEnv(".env") },
	}
	root.PersistentFlags().StringVar(&flagKubeconfig, "kubeconfig", "", "path to kubeconfig (defaults to standard rules)")
	root.PersistentFlags().StringVar(&flagContext, "context", "", "kubeconfig context to use")

	root.AddCommand(newNSCmd())
	root.AddCommand(newClusterCmd())
	root.AddCommand(newLayersCmd())
	root.AddCommand(newDocCmd())
	root.AddCommand(newExplainCmd())
	root.AddCommand(newAskCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newTrafficCmd())
	root.AddCommand(newServeCmd())
	return root
}

// loadDotEnv reads KEY=VALUE lines from path into the environment, without
// overriding variables already set. Supports `export ` prefixes, # comments and
// quoted values. Missing file is a no-op.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
