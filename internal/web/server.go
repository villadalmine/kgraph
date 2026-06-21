// Package web serves an interactive browser UI for kgraph: pick a namespace,
// layer or traffic view and render the diagram live. It reuses the same graph
// and render pipeline as the CLI.
package web

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/villadalmine/kgraph/internal/collect"
	"github.com/villadalmine/kgraph/internal/graph"
	"github.com/villadalmine/kgraph/internal/layers"
	"github.com/villadalmine/kgraph/internal/preflight"
	"github.com/villadalmine/kgraph/internal/render"
	"github.com/villadalmine/kgraph/internal/traffic"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

//go:embed index.html
var indexHTML []byte

// staticFS holds vendored client assets (e.g. cytoscape.min.js), served offline.
//
//go:embed static
var staticFS embed.FS

// Server holds the cluster connection settings used to build a Collector per
// request, plus a small render cache for declarative views.
type Server struct {
	kubeconfig string
	context    string

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body  []byte
	ctype string
	nodes int
	at    time.Time
}

const cacheTTL = 60 * time.Second

// NewServer returns a Server targeting the given kubeconfig/context.
func NewServer(kubeconfig, context string) *Server {
	return &Server{kubeconfig: kubeconfig, context: context, cache: map[string]cacheEntry{}}
}

// Handler returns the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/namespaces", s.handleNamespaces)
	mux.HandleFunc("/api/layers", s.handleLayers)
	mux.HandleFunc("/render", s.handleRender)
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/traffic/stream", s.handleTrafficStream)
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	return mux
}

// graphDoc is the JSON shape consumed by the interactive client.
type graphDoc struct {
	Title string     `json:"title"`
	Nodes []nodeJSON `json:"nodes"`
	Edges []edgeJSON `json:"edges"`
}

type nodeJSON struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Layer     string            `json:"layer"`
	Color     string            `json:"color"`
	Stroke    string            `json:"stroke"`
	Icon      string            `json:"icon,omitempty"` // K8s icon base name, "" for none
	Alert     bool              `json:"alert"`
	Note      string            `json:"note,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type edgeJSON struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	Kind    string `json:"kind"`
	Note    string `json:"note,omitempty"`
	Weight  int    `json:"weight"`
	Dropped bool   `json:"dropped"`
	Error   bool   `json:"error"`
	Gap     bool   `json:"gap"`
}

// graphJSON converts a graph to the client document, reusing the render palette.
func graphJSON(g *graph.Graph, title string) graphDoc {
	doc := graphDoc{Title: title}
	for _, n := range g.SortedNodes() {
		fill, stroke := render.CategoryColor(n.Kind)
		doc.Nodes = append(doc.Nodes, nodeJSON{
			ID: n.ID, Kind: n.Kind, Name: n.Name, Namespace: n.Namespace,
			Layer: n.Layer, Color: fill, Stroke: stroke, Icon: render.IconName(n.Kind),
			Alert: n.Alert, Note: n.Note, Labels: n.Labels,
		})
	}
	for i, e := range g.Edges {
		doc.Edges = append(doc.Edges, edgeJSON{
			ID: fmt.Sprintf("e%d", i), Source: e.From, Target: e.To,
			Kind: string(e.Kind), Note: e.Note, Weight: e.Weight,
			Dropped: e.Dropped, Error: e.Error, Gap: e.Gap,
		})
	}
	return doc
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	g, title, err := s.buildGraph(r.Context(), q.Get("view"), q.Get("ns"), q.Get("layer"), q.Get("l7") == "1")
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, graphJSON(g, title))
}

// streaming tunables (see specs/0001-live-traffic-streaming.md).
const (
	streamInterval = 3 * time.Second
	streamFlows    = 2000
)

func (s *Server) collector() (*collect.Collector, error) {
	return collect.New(s.kubeconfig, s.context)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	c, err := s.collector()
	if err != nil {
		httpError(w, err)
		return
	}
	names, err := c.Namespaces(r.Context())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, names)
}

func (s *Server) handleLayers(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		http.Error(w, "missing ns", http.StatusBadRequest)
		return
	}
	c, err := s.collector()
	if err != nil {
		httpError(w, err)
		return
	}
	res, err := c.Namespace(r.Context(), ns)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, layers.Detect(res.Objects))
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	view, ns, layer := q.Get("view"), q.Get("ns"), q.Get("layer")
	l7 := q.Get("l7") == "1"
	format := q.Get("format")
	if format == "" {
		format = "svg"
	}

	body, ctype, nodes, err := s.cachedDiagram(r.Context(), view, ns, layer, l7, format)
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Kgraph-Nodes", strconv.Itoa(nodes))
	w.Write(body)
}

// cachedDiagram serves declarative (ns/cluster) renders from a short-TTL cache;
// traffic and combined are never cached because they need live flows.
func (s *Server) cachedDiagram(ctx context.Context, view, ns, layer string, l7 bool, format string) ([]byte, string, int, error) {
	if view == "traffic" || view == "combined" {
		return s.buildDiagram(ctx, view, ns, layer, l7, format)
	}
	key := strings.Join([]string{view, ns, layer, fmt.Sprint(l7), format}, "|")
	e, err := s.cached(key, func() (cacheEntry, error) {
		body, ctype, nodes, err := s.buildDiagram(ctx, view, ns, layer, l7, format)
		return cacheEntry{body: body, ctype: ctype, nodes: nodes}, err
	})
	if err != nil {
		return nil, "", 0, err
	}
	return e.body, e.ctype, e.nodes, nil
}

// cached returns a fresh cache entry for key, computing it via build on a miss or
// expiry. Concurrency-safe and independent of the cluster (testable in isolation).
func (s *Server) cached(key string, build func() (cacheEntry, error)) (cacheEntry, error) {
	s.mu.Lock()
	if e, ok := s.cache[key]; ok && time.Since(e.at) < cacheTTL {
		s.mu.Unlock()
		return e, nil
	}
	s.mu.Unlock()

	e, err := build()
	if err != nil {
		return cacheEntry{}, err
	}
	e.at = time.Now()
	s.mu.Lock()
	s.cache[key] = e
	s.mu.Unlock()
	return e, nil
}

// handleTrafficStream serves a live traffic view over Server-Sent Events: it
// keeps one Hubble Relay port-forward open and re-renders a rolling window of
// recent flows every streamInterval, pushing each SVG as a base64 data frame.
func (s *Server) handleTrafficStream(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		http.Error(w, "missing ns", http.StatusBadRequest)
		return
	}
	l7 := r.URL.Query().Get("l7") == "1"

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sendErr := func(err error) {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", sseEscape(err.Error()))
		flusher.Flush()
	}

	c, err := s.collector()
	if err != nil {
		sendErr(err)
		return
	}
	ctx := r.Context()
	if err := preflight.RequireHubble(ctx, c); err != nil {
		sendErr(err)
		return
	}
	addr, stop, err := traffic.PortForwardRelay(ctx, c.RESTConfig())
	if err != nil {
		sendErr(err)
		return
	}
	defer stop()

	tick := func() {
		g, _, _, err := traffic.FetchFlows(ctx, addr, ns, streamFlows, l7)
		if err != nil {
			sendErr(err)
			return
		}
		svg, err := render.SVG(ctx, render.D2(g, "Traffic: "+ns, false))
		if err != nil {
			sendErr(err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", base64.StdEncoding.EncodeToString(svg))
		flusher.Flush()
	}

	tick() // immediate first frame
	t := time.NewTicker(streamInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// sseEscape collapses newlines so an error message stays a single SSE data line.
func sseEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func (s *Server) buildDiagram(ctx context.Context, view, ns, layer string, l7 bool, format string) ([]byte, string, int, error) {
	g, title, err := s.buildGraph(ctx, view, ns, layer, l7)
	if err != nil {
		return nil, "", 0, err
	}
	body, ctype, err := emitDiagram(ctx, g, title, format)
	if err != nil {
		return nil, "", 0, err
	}
	return body, ctype, len(g.Nodes), nil
}

// buildGraph produces the graph (no layout/render) for a view. This is the fast
// path shared by the SVG render and the interactive JSON endpoint.
func (s *Server) buildGraph(ctx context.Context, view, ns, layer string, l7 bool) (*graph.Graph, string, error) {
	c, err := s.collector()
	if err != nil {
		return nil, "", err
	}

	flows := func() (*graph.Graph, error) {
		if err := preflight.RequireHubble(ctx, c); err != nil {
			return nil, err
		}
		pfCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		addr, stop, err := traffic.PortForwardRelay(pfCtx, c.RESTConfig())
		if err != nil {
			return nil, err
		}
		defer stop()
		fg, _, _, err := traffic.FetchFlows(pfCtx, addr, ns, 2000, l7)
		return fg, err
	}

	switch view {
	case "traffic":
		if ns == "" {
			return nil, "", fmt.Errorf("traffic view requires a namespace")
		}
		fg, err := flows()
		if err != nil {
			return nil, "", err
		}
		return fg, "Traffic: " + ns, nil

	case "combined":
		if ns == "" {
			return nil, "", fmt.Errorf("combined view requires a namespace")
		}
		res, err := c.Namespace(ctx, ns)
		if err != nil {
			return nil, "", err
		}
		fg, err := flows()
		if err != nil {
			return nil, "", err
		}
		g, err := topology(res.Objects, layer)
		if err != nil {
			return nil, "", err
		}
		graph.Overlay(g, fg)
		return g, "Namespace+traffic: " + ns, nil

	case "cluster":
		res, err := c.Cluster(ctx)
		if err != nil {
			return nil, "", err
		}
		g, err := topology(res.Objects, layer)
		return g, "Cluster-scoped", err

	default: // namespace topology
		if ns == "" {
			return nil, "", fmt.Errorf("namespace view requires a namespace")
		}
		res, err := c.Namespace(ctx, ns)
		if err != nil {
			return nil, "", err
		}
		g, err := topology(res.Objects, layer)
		if err != nil {
			return nil, "", err
		}
		title := "Namespace: " + ns
		if layer != "" {
			title += " — layer: " + layer
		}
		return g, title, nil
	}
}

// topology builds and prunes a declarative graph, optionally scoped to a layer.
func topology(objs []*unstructured.Unstructured, layer string) (*graph.Graph, error) {
	g := graph.Build(objs)
	graph.Prune(g, false)
	if layer != "" {
		rule, ok := layers.Find(layer)
		if !ok {
			return nil, fmt.Errorf("unknown layer %q", layer)
		}
		seed := func(n *graph.Node) bool { return rule.Matches(n.Group, n.Kind) }
		g = g.Subgraph(seed, 1, layer)
		if len(g.Nodes) == 0 {
			return nil, fmt.Errorf("layer %q not present", layer)
		}
	}
	return g, nil
}

// emitDiagram renders a graph to the requested format (svg or d2), returning the
// body and its content type.
func emitDiagram(ctx context.Context, g *graph.Graph, title, format string) ([]byte, string, error) {
	d2src := render.D2(g, title, false)
	if format == "d2" {
		return []byte(d2src), "text/plain; charset=utf-8", nil
	}
	svg, err := render.SVG(ctx, d2src)
	if err != nil {
		return nil, "", err
	}
	return svg, "image/svg+xml", nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
