package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/graph"
	"github.com/villadalmine/kgraph/internal/metrics"
	"github.com/villadalmine/kgraph/internal/preflight"
	"github.com/villadalmine/kgraph/internal/render"
	"github.com/villadalmine/kgraph/internal/traffic"
)

func newTrafficCmd() *cobra.Command {
	var (
		output       string
		format       string
		relayAddr    string
		last         int
		policy       bool
		suggestKind  string
		l7           bool
		throughput   bool
		promAddr     string
		relayTLS     bool
		relaySkip    bool
		relaySNI     string
		relayCA      string
		relayCert    string
		relayKey     string
		relayMTLS    bool
		relayMTLSSec string
	)
	cmd := &cobra.Command{
		Use:   "traffic <namespace>",
		Short: "Diagram observed network traffic for a namespace (via Cilium Hubble)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns := args[0]
			ctx := context.Background()

			c, err := collect.New(flagKubeconfig, flagContext)
			if err != nil {
				return err
			}

			addr := relayAddr
			if addr == "" {
				// Out-of-the-box path: verify Hubble, then port-forward the relay.
				if err := preflight.RequireHubble(ctx, c); err != nil {
					return err
				}
				fmt.Fprintln(os.Stderr, "port-forwarding hubble-relay...")
				a, stop, err := traffic.PortForwardRelay(ctx, c.RESTConfig())
				if err != nil {
					return err
				}
				defer stop()
				addr = a
			}

			var opts []traffic.Option
			if relayTLS {
				opts = append(opts, traffic.WithTLS(relaySkip, relaySNI, relayCA))
			}
			if relayCert != "" || relayKey != "" {
				opts = append(opts, traffic.WithClientCert(relayCert, relayKey))
			}
			if relayMTLS || relayMTLSSec != "" {
				opt, err := traffic.MTLSFromSecret(ctx, c.RESTConfig(), relayMTLSSec)
				if err != nil {
					return err
				}
				opts = append(opts, opt)
			}
			fmt.Fprintf(os.Stderr, "fetching last %d flows for namespace %q...\n", last, ns)
			g, n, l7edges, err := traffic.FetchFlows(ctx, addr, ns, last, l7, opts...)
			if err != nil {
				return err
			}
			if len(g.Nodes) == 0 {
				return fmt.Errorf("no flows observed for namespace %q (try a busier namespace or a larger --last)", ns)
			}
			fmt.Fprintf(os.Stderr, "aggregated %d flows into %d nodes, %d edges\n", n, len(g.Nodes), len(g.Edges))
			if l7 && l7edges == 0 {
				fmt.Fprintln(os.Stderr, "note: no L7 data observed — enable Hubble L7 visibility "+
					"(a CiliumNetworkPolicy with toPorts rules, or the io.cilium.proxy-visibility annotation) to see HTTP/DNS detail")
			}

			// Throughput overlay: annotate nodes with rx/tx rates from Prometheus.
			if throughput {
				annotateThroughput(ctx, c, ns, promAddr, g)
			}

			// Security overlay: mark unprotected workloads + gap/denied flows.
			if policy || suggestKind != "" {
				sum, err := traffic.AnalyzePolicies(ctx, c.Dynamic(), ns, g)
				if err != nil {
					return err
				}
				fmt.Fprint(os.Stderr, "\n--- policy overlay ---\n"+sum.String()+"\n")
			}

			// Emit suggested policies instead of a diagram, if requested.
			if suggestKind != "" {
				if suggestKind != "k8s" && suggestKind != "cilium" {
					return fmt.Errorf("--suggest-policy must be 'k8s' or 'cilium'")
				}
				y, err := traffic.SuggestPolicies(g, ns, suggestKind)
				if err != nil {
					return err
				}
				if output == "" {
					fmt.Print(string(y))
					return nil
				}
				return write(output, y)
			}

			title := fmt.Sprintf("Traffic: %s (last %d flows)", ns, last)
			d2src := render.D2(g, title, false)
			switch strings.ToLower(format) {
			case "d2":
				return write(defaultOut(output, ns+"-traffic", "d2"), []byte(d2src))
			case "svg", "":
				svg, err := render.SVG(ctx, d2src)
				if err != nil {
					return err
				}
				return write(defaultOut(output, ns+"-traffic", "svg"), svg)
			default:
				return fmt.Errorf("unknown format %q (use svg or d2)", format)
			}
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default <namespace>-traffic.<format>)")
	cmd.Flags().StringVar(&format, "format", "svg", "output format: svg|d2")
	cmd.Flags().StringVar(&relayAddr, "relay-addr", "", "Hubble relay address host:port (skips auto port-forward)")
	cmd.Flags().IntVar(&last, "last", 2000, "number of recent flows to sample")
	cmd.Flags().BoolVar(&policy, "policy", false, "overlay NetworkPolicy/CiliumNetworkPolicy coverage (flag unprotected workloads + gap/denied flows)")
	cmd.Flags().StringVar(&suggestKind, "suggest-policy", "", "instead of a diagram, emit suggested policies from observed flows: k8s|cilium")
	cmd.Flags().BoolVar(&l7, "l7", false, "label edges with L7 detail (HTTP method/path/status/latency, DNS) when available")
	cmd.Flags().BoolVar(&throughput, "throughput", false, "annotate workloads with rx/tx rates from Prometheus (cAdvisor)")
	cmd.Flags().StringVar(&promAddr, "prom", "", "Prometheus address host:port or URL (skips auto port-forward)")
	cmd.Flags().BoolVar(&relayTLS, "relay-tls", false, "dial the Hubble relay over TLS (for a real TLS relay via --relay-addr)")
	cmd.Flags().BoolVar(&relaySkip, "relay-tls-skip-verify", false, "with --relay-tls: do not verify the relay's certificate")
	cmd.Flags().StringVar(&relaySNI, "relay-server-name", "", "with --relay-tls: expected server name on the relay certificate")
	cmd.Flags().StringVar(&relayCA, "relay-ca", "", "with --relay-tls: PEM CA bundle to trust for the relay")
	cmd.Flags().StringVar(&relayCert, "relay-cert", "", "client certificate (PEM) for relay mTLS")
	cmd.Flags().StringVar(&relayKey, "relay-key", "", "client private key (PEM) for relay mTLS")
	cmd.Flags().BoolVar(&relayMTLS, "relay-mtls", false, "configure relay mTLS from the kube-system/hubble-relay-client-certs secret")
	cmd.Flags().StringVar(&relayMTLSSec, "relay-mtls-secret", "", "configure relay mTLS from a namespace/name TLS secret")
	return cmd
}

// annotateThroughput enriches workload nodes with rx/tx rates from Prometheus.
// Graceful: any failure prints a warning and leaves the diagram unannotated.
func annotateThroughput(ctx context.Context, c *collect.Collector, ns, promAddr string, g *graph.Graph) {
	base := promAddr
	if base == "" {
		fmt.Fprintln(os.Stderr, "port-forwarding prometheus...")
		addr, stop, err := traffic.PortForwardPrometheus(ctx, c.RESTConfig())
		if err != nil {
			fmt.Fprintln(os.Stderr, "warn: throughput unavailable:", err)
			return
		}
		defer stop()
		base = addr
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}

	rates, err := metrics.New(base).WorkloadRates(ctx, ns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: throughput query failed:", err)
		return
	}
	if len(rates) == 0 {
		fmt.Fprintln(os.Stderr, "note: no container_network_*_bytes_total metrics for", ns,
			"(is cAdvisor/kubelet scraped?)")
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n--- throughput (%s) ---\n", ns)
	hits := 0
	for _, n := range g.SortedNodes() {
		r, ok := rates[n.Name]
		if !ok || n.Namespace != ns {
			continue
		}
		n.Note = fmt.Sprintf("↓%s ↑%s", metrics.HumanRate(r.Rx), metrics.HumanRate(r.Tx))
		fmt.Fprintf(&b, "%-30s rx %-12s tx %s\n", n.Name, metrics.HumanRate(r.Rx), metrics.HumanRate(r.Tx))
		hits++
	}
	if hits == 0 {
		fmt.Fprintln(os.Stderr, "note: throughput metrics found but none matched observed workloads")
		return
	}
	fmt.Fprint(os.Stderr, b.String())
}
