// Package traffic builds an observed-network-traffic graph from Cilium Hubble.
package traffic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observer "github.com/cilium/cilium/api/v1/observer"
	"github.com/villadalmine/kgraph/internal/graph"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// dialConfig holds optional transport settings for the Hubble relay connection.
type dialConfig struct {
	useTLS     bool
	skipVerify bool
	serverName string
	caPEM      []byte // trusted CA bundle (PEM)
	certPEM    []byte // client certificate for mTLS (PEM)
	keyPEM     []byte // client private key for mTLS (PEM)
}

// Option configures how FetchFlows dials the Hubble relay.
type Option func(*dialConfig)

// WithTLS makes FetchFlows dial the relay over TLS. Set skipVerify to accept any
// server certificate, serverName to override the SNI/verification name, and
// caFile to trust a custom CA bundle. Use this for a real (non-port-forwarded)
// TLS-enabled relay via --relay-addr.
func WithTLS(skipVerify bool, serverName, caFile string) Option {
	return func(d *dialConfig) {
		d.useTLS = true
		d.skipVerify = skipVerify
		d.serverName = serverName
		if caFile != "" {
			if pem, err := os.ReadFile(caFile); err == nil {
				d.caPEM = pem
			} else {
				d.caPEM = []byte("\x00invalid:" + err.Error()) // surfaced in transportCreds
			}
		}
	}
}

// WithClientCert adds a client certificate (mTLS) from files. Implies TLS.
func WithClientCert(certFile, keyFile string) Option {
	return func(d *dialConfig) {
		d.useTLS = true
		d.certPEM, _ = os.ReadFile(certFile)
		d.keyPEM, _ = os.ReadFile(keyFile)
	}
}

// WithMTLSPEM configures mTLS directly from PEM bytes (e.g. read from a
// Kubernetes secret). Implies TLS. ca may be nil to use the system roots.
func WithMTLSPEM(ca, cert, key []byte) Option {
	return func(d *dialConfig) {
		d.useTLS = true
		d.caPEM, d.certPEM, d.keyPEM = ca, cert, key
	}
}

// transportCreds builds the gRPC transport credentials for the dial config.
func (d dialConfig) transportCreds() (credentials.TransportCredentials, error) {
	if !d.useTLS {
		return insecure.NewCredentials(), nil
	}
	cfg := &tls.Config{InsecureSkipVerify: d.skipVerify, ServerName: d.serverName}
	if len(d.caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(d.caPEM) {
			return nil, fmt.Errorf("no valid certificates in relay CA bundle")
		}
		cfg.RootCAs = pool
	}
	if len(d.certPEM) > 0 || len(d.keyPEM) > 0 {
		pair, err := tls.X509KeyPair(d.certPEM, d.keyPEM)
		if err != nil {
			return nil, fmt.Errorf("loading relay client certificate (mTLS): %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return credentials.NewTLS(cfg), nil
}

// FetchFlows connects to a Hubble Relay at addr (host:port, assumed non-TLS, as
// exposed by a port-forward), pulls the last `last` flows touching namespace ns,
// and returns an aggregated traffic graph (workload-level, edges weighted by
// flow count and coloured by verdict). When l7 is true, edges are labelled with
// observed L7 (HTTP/DNS) detail instead of L4 ports.
func FetchFlows(ctx context.Context, addr, ns string, last int, l7 bool, opts ...Option) (g *graph.Graph, flows, l7edges int, err error) {
	var dc dialConfig
	for _, o := range opts {
		o(&dc)
	}
	creds, err := dc.transportCreds()
	if err != nil {
		return nil, 0, 0, err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("dial hubble relay %s: %w", addr, err)
	}
	defer conn.Close()

	client := observer.NewObserverClient(conn)
	req := &observer.GetFlowsRequest{
		Number: uint64(last),
		Follow: false,
		Whitelist: []*flowpb.FlowFilter{
			{SourcePod: []string{ns + "/"}},
			{DestinationPod: []string{ns + "/"}},
		},
	}
	stream, err := client.GetFlows(ctx, req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("GetFlows: %w (is this a Hubble relay? TLS relays are not supported yet — try --relay-addr to a non-TLS port)", err)
	}

	agg := newAggregator(l7)
	count := 0
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, count, 0, fmt.Errorf("receiving flows: %w", err)
		}
		f := resp.GetFlow()
		if f == nil {
			continue
		}
		agg.add(f)
		count++
	}
	l7edges = 0
	for _, st := range agg.l7 {
		if st.http > 0 || st.dns > 0 {
			l7edges++
		}
	}
	return agg.build(), count, l7edges, nil
}

// endpoint is an aggregation key for one side of a flow.
type endpoint struct {
	group, kind, ns, name string
	labels                map[string]string
}

func (e endpoint) id() string { return graph.NodeID(e.group, e.kind, e.ns, e.name) }

type edgeKey struct {
	from, to string
	dropped  bool
}

// l7stat accumulates observed L7 detail for one edge.
type l7stat struct {
	http, dns        int
	c2xx, c4xx, c5xx int
	sampleReq        string // e.g. "GET /api"
	sampleDNS        string
	maxLatencyNs     uint64
}

// merge folds another l7stat into this one (used when reconciling collapses
// two edges onto the same canonical endpoints).
func (st *l7stat) merge(o *l7stat) {
	st.http += o.http
	st.dns += o.dns
	st.c2xx += o.c2xx
	st.c4xx += o.c4xx
	st.c5xx += o.c5xx
	if st.sampleReq == "" {
		st.sampleReq = o.sampleReq
	}
	if st.sampleDNS == "" {
		st.sampleDNS = o.sampleDNS
	}
	if o.maxLatencyNs > st.maxLatencyNs {
		st.maxLatencyNs = o.maxLatencyNs
	}
}

type aggregator struct {
	endpoints map[string]endpoint // id -> endpoint
	edges     map[edgeKey]int     // key -> flow count
	ports     map[edgeKey]string  // key -> a sample protocol:port label
	l7        map[edgeKey]*l7stat // key -> L7 detail (when enabled)
	useL7     bool
}

func newAggregator(useL7 bool) *aggregator {
	return &aggregator{
		endpoints: map[string]endpoint{},
		edges:     map[edgeKey]int{},
		ports:     map[edgeKey]string{},
		l7:        map[edgeKey]*l7stat{},
		useL7:     useL7,
	}
}

func (a *aggregator) add(f *flowpb.Flow) {
	src := classify(f.GetSource())
	dst := classify(f.GetDestination())
	if src.name == "" || dst.name == "" {
		return
	}
	a.endpoints[src.id()] = src
	a.endpoints[dst.id()] = dst

	dropped := f.GetVerdict() == flowpb.Verdict_DROPPED
	k := edgeKey{from: src.id(), to: dst.id(), dropped: dropped}
	a.edges[k]++
	if _, ok := a.ports[k]; !ok {
		a.ports[k] = l4Label(f.GetL4())
	}
	if a.useL7 {
		a.addL7(k, f.GetL7())
	}
}

func (a *aggregator) addL7(k edgeKey, l7 *flowpb.Layer7) {
	if l7 == nil {
		return
	}
	st := a.l7[k]
	if st == nil {
		st = &l7stat{}
		a.l7[k] = st
	}
	if lat := l7.GetLatencyNs(); lat > st.maxLatencyNs {
		st.maxLatencyNs = lat
	}
	if h := l7.GetHttp(); h != nil {
		st.http++
		switch code := h.GetCode(); {
		case code >= 500:
			st.c5xx++
		case code >= 400:
			st.c4xx++
		case code >= 200:
			st.c2xx++
		}
		if st.sampleReq == "" && h.GetMethod() != "" {
			st.sampleReq = h.GetMethod() + " " + urlPath(h.GetUrl())
		}
	} else if d := l7.GetDns(); d != nil {
		st.dns++
		if st.sampleDNS == "" {
			st.sampleDNS = d.GetQuery()
		}
	}
}

func (a *aggregator) build() *graph.Graph {
	canon, idMap := a.reconcile()

	g := graph.New()
	for _, e := range canon {
		n := g.AddNode(e.group, e.kind, e.ns, e.name)
		n.Labels = e.labels
		// Group by namespace (external traffic in its own container).
		if e.ns != "" {
			n.Layer = e.ns
		} else {
			n.Layer = "external"
		}
	}

	// Translate edges through idMap, summing weights and merging stats for any
	// edges that collapse onto the same canonical endpoints.
	counts := map[edgeKey]int{}
	ports := map[edgeKey]string{}
	l7 := map[edgeKey]*l7stat{}
	for k, n := range a.edges {
		ck := edgeKey{from: idMap[k.from], to: idMap[k.to], dropped: k.dropped}
		counts[ck] += n
		if ports[ck] == "" {
			ports[ck] = a.ports[k]
		}
		if a.useL7 {
			if src := a.l7[k]; src != nil {
				if l7[ck] == nil {
					l7[ck] = &l7stat{}
				}
				l7[ck].merge(src)
			}
		}
	}

	for k, n := range counts {
		label := fmt.Sprintf("%d", n)
		if p := ports[k]; p != "" {
			label = fmt.Sprintf("%d %s", n, p)
		}
		errors := false
		if a.useL7 {
			if st := l7[k]; st != nil {
				if l := l7Label(st); l != "" {
					label = l
				}
				errors = st.c5xx > 0
			}
		}
		if k.dropped {
			label += " ✖"
		}
		g.AddFlowEdgeFull(k.from, k.to, n, k.dropped, errors, label)
	}
	return g
}

// reconcile collapses endpoints that refer to the same workload — typically a
// Pod/<name> node (from flows lacking Hubble workload info) and a
// <WorkloadKind>/<name> node (from flows that have it) sharing (ns, name). It
// returns the canonical endpoints and a map from every original endpoint id to
// its canonical id. Naming-agnostic: keyed only on namespace + reported name.
func (a *aggregator) reconcile() (map[string]endpoint, map[string]string) {
	type nk struct{ ns, name string }
	canonByKey := map[nk]endpoint{}
	for _, e := range a.endpoints {
		key := nk{e.ns, e.name}
		cur, ok := canonByKey[key]
		if !ok {
			canonByKey[key] = e
			continue
		}
		// Keep the richer kind; merge labels.
		winner, loser := cur, e
		if kindRank(e.kind) > kindRank(cur.kind) {
			winner, loser = e, cur
		}
		winner.labels = mergeLabels(winner.labels, loser.labels)
		canonByKey[key] = winner
	}

	canon := map[string]endpoint{}
	for _, e := range canonByKey {
		canon[e.id()] = e
	}
	idMap := map[string]string{}
	for _, e := range a.endpoints {
		idMap[e.id()] = canonByKey[nk{e.ns, e.name}].id()
	}
	return canon, idMap
}

// kindRank prefers a concrete workload kind over the generic "Workload", and
// both over a bare "Pod", when collapsing duplicate endpoints.
func kindRank(kind string) int {
	switch kind {
	case "", "Pod":
		return 0
	case "Workload":
		return 1
	default:
		return 2 // concrete workload kind (Deployment, StatefulSet, ...) or External
	}
}

func mergeLabels(a, b map[string]string) map[string]string {
	if len(b) == 0 {
		return a
	}
	if a == nil {
		a = map[string]string{}
	}
	for k, v := range b {
		if _, ok := a[k]; !ok {
			a[k] = v
		}
	}
	return a
}

// l7Label renders a compact L7 summary for an edge, or "" if no L7 was seen.
func l7Label(st *l7stat) string {
	if st.http > 0 {
		s := fmt.Sprintf("%s ×%d", st.sampleReq, st.http)
		if st.c5xx > 0 {
			s += fmt.Sprintf(" 5xx:%d", st.c5xx)
		} else if st.c4xx > 0 {
			s += fmt.Sprintf(" 4xx:%d", st.c4xx)
		}
		if st.maxLatencyNs > 0 {
			s += fmt.Sprintf(" ~%.0fms", float64(st.maxLatencyNs)/1e6)
		}
		return s
	}
	if st.dns > 0 {
		if st.sampleDNS != "" {
			return fmt.Sprintf("DNS ×%d (%s)", st.dns, st.sampleDNS)
		}
		return fmt.Sprintf("DNS ×%d", st.dns)
	}
	return ""
}

// urlPath extracts the path from a possibly-full URL.
func urlPath(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.IndexByte(u, '/'); j >= 0 {
			u = u[j:]
		} else {
			u = "/"
		}
	}
	if q := strings.IndexByte(u, '?'); q >= 0 {
		u = u[:q]
	}
	return u
}

var (
	rsHash  = regexp.MustCompile(`-[a-f0-9]{8,10}(-[a-z0-9]{5})?$`)
	ordinal = regexp.MustCompile(`-\d+$`) // StatefulSet pod ordinal suffix
)

// classify reduces a Hubble endpoint to a workload-level node.
func classify(ep *flowpb.Endpoint) endpoint {
	if ep == nil {
		return endpoint{}
	}
	labels := parseLabels(ep.GetLabels())
	ns := ep.GetNamespace()
	if ns == "" {
		// External / reserved identity (world, host, kube-apiserver, ...).
		return endpoint{kind: "External", name: reservedName(ep.GetLabels()), labels: labels}
	}
	// Prefer the workload name/kind if Hubble provides it.
	if wls := ep.GetWorkloads(); len(wls) > 0 {
		w := wls[0]
		kind := w.GetKind()
		if kind == "" {
			kind = "Workload"
		}
		return endpoint{kind: kind, ns: ns, name: w.GetName(), labels: labels}
	}
	// Fall back to the pod name reduced toward its workload: strip the
	// Deployment ReplicaSet+pod hash, or (failing that) a StatefulSet ordinal,
	// so it reconciles with workload-level nodes from flows that did carry
	// workload info (see reconcile + specs/0011).
	pod := ep.GetPodName()
	name := rsHash.ReplaceAllString(pod, "")
	if name == pod {
		name = ordinal.ReplaceAllString(pod, "")
	}
	if name == "" {
		name = pod
	}
	return endpoint{kind: "Pod", ns: ns, name: name, labels: labels}
}

// parseLabels turns Hubble's source-prefixed labels ("k8s:app=web") into a plain
// label map, keeping only Kubernetes labels.
func parseLabels(in []string) map[string]string {
	out := map[string]string{}
	for _, l := range in {
		if strings.HasPrefix(l, "k8s:") {
			l = strings.TrimPrefix(l, "k8s:")
		} else if strings.Contains(l, ":") {
			continue // reserved:, unspec:, etc.
		}
		if k, v, ok := strings.Cut(l, "="); ok {
			out[k] = v
		}
	}
	return out
}

func reservedName(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "reserved:") {
			return strings.TrimPrefix(l, "reserved:")
		}
	}
	return "world"
}

func l4Label(l4 *flowpb.Layer4) string {
	if l4 == nil {
		return ""
	}
	if t := l4.GetTCP(); t != nil {
		return fmt.Sprintf("TCP:%d", t.GetDestinationPort())
	}
	if u := l4.GetUDP(); u != nil {
		return fmt.Sprintf("UDP:%d", u.GetDestinationPort())
	}
	if l4.GetICMPv4() != nil || l4.GetICMPv6() != nil {
		return "ICMP"
	}
	return ""
}
