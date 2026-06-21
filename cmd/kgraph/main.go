package main

import (
	"fmt"
	"os"

	"github.com/villadalmine/kgraph/internal/cli"
)

// Stamped at build time by goreleaser (see .goreleaser.yaml); "dev" otherwise.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := cli.NewRoot()
	root.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
