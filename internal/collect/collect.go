// Package collect connects to a cluster via kubeconfig and lists the resources
// of a namespace using dynamic discovery, so no resource types are hardcoded.
package collect

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// requestTimeout bounds every API call so an unreachable aggregated API
// (metrics, vcluster, etc.) cannot hang the whole collection.
const requestTimeout = 15 * time.Second

// listWorkers caps concurrent List calls.
const listWorkers = 12

// Collector lists namespaced resources from a cluster.
type Collector struct {
	dyn  dynamic.Interface
	disc discovery.DiscoveryInterface
	cfg  *rest.Config
}

// Result holds collected objects plus any non-fatal warnings (e.g. RBAC denials).
type Result struct {
	Objects  []*unstructured.Unstructured
	Warnings []string
}

// Discovery exposes the discovery client for capability checks.
func (c *Collector) Discovery() discovery.DiscoveryInterface { return c.disc }

// Dynamic exposes the dynamic client for targeted reads.
func (c *Collector) Dynamic() dynamic.Interface { return c.dyn }

// RESTConfig exposes the REST config (e.g. for port-forwarding).
func (c *Collector) RESTConfig() *rest.Config { return c.cfg }

// Namespaces lists namespace names in the cluster.
func (c *Collector) Namespaces(ctx context.Context) ([]string, error) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	list, err := c.dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, list.Items[i].GetName())
	}
	return out, nil
}

// New builds a Collector from a kubeconfig path and context name (both optional;
// empty values fall back to the standard loading rules / current context).
func New(kubeconfig, contextName string) (*Collector, error) {
	cfg, err := restConfig(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	return &Collector{dyn: dyn, disc: disc, cfg: cfg}, nil
}

func restConfig(kubeconfig, contextName string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	cfg.Timeout = requestTimeout
	// Default QPS (5) throttles heavily when scanning hundreds of GVRs; raise it.
	cfg.QPS = 50
	cfg.Burst = 100
	return cfg, nil
}

// Namespace lists all namespaced resources of the given namespace across every
// discoverable API group/version that supports list. RBAC denials and similar
// per-resource errors are collected as warnings instead of failing the run.
func (c *Collector) Namespace(ctx context.Context, namespace string) (*Result, error) {
	res := &Result{}
	lists, err := c.disc.ServerPreferredNamespacedResources()
	if err != nil {
		// Partial results may accompany an error (e.g. an aggregated API down).
		res.Warnings = append(res.Warnings, fmt.Sprintf("discovery partial: %v", err))
	}
	gvrs := discoverGVRs(lists, false)
	if len(gvrs) == 0 {
		return res, fmt.Errorf("no API resources discovered")
	}
	c.list(ctx, gvrs, namespace, res)
	return res, nil
}

// Cluster lists all cluster-scoped (non-namespaced) resources across every
// discoverable API group/version, with the same partial-failure tolerance.
func (c *Collector) Cluster(ctx context.Context) (*Result, error) {
	res := &Result{}
	lists, err := c.disc.ServerPreferredResources()
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("discovery partial: %v", err))
	}
	gvrs := discoverGVRs(lists, true)
	if len(gvrs) == 0 {
		return res, fmt.Errorf("no API resources discovered")
	}
	c.list(ctx, gvrs, "", res)
	return res, nil
}

// discoverGVRs returns the unique, listable GVRs from discovery lists. When
// clusterScoped is true only non-namespaced resources are returned; otherwise
// the lists are assumed already scoped to namespaced resources.
func discoverGVRs(lists []*metav1.APIResourceList, clusterScoped bool) []schema.GroupVersionResource {
	seen := map[schema.GroupVersionResource]bool{}
	var gvrs []schema.GroupVersionResource
	for _, list := range lists {
		gv, perr := schema.ParseGroupVersion(list.GroupVersion)
		if perr != nil {
			continue
		}
		for _, r := range list.APIResources {
			if !canList(r) || strings.Contains(r.Name, "/") {
				continue // skip subresources and non-listable resources
			}
			if clusterScoped && r.Namespaced {
				continue
			}
			gvr := gv.WithResource(r.Name)
			if seen[gvr] {
				continue
			}
			seen[gvr] = true
			gvrs = append(gvrs, gvr)
		}
	}
	return gvrs
}

// list fetches each GVR concurrently into res. An empty namespace means a
// cluster-scoped list. Each call is bounded by requestTimeout; per-resource
// errors become warnings.
func (c *Collector) list(ctx context.Context, gvrs []schema.GroupVersionResource, namespace string, res *Result) {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, listWorkers)
	)
	for _, gvr := range gvrs {
		gvr := gvr
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			lctx, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()
			var ri dynamic.ResourceInterface = c.dyn.Resource(gvr)
			if namespace != "" {
				ri = c.dyn.Resource(gvr).Namespace(namespace)
			}
			ul, lerr := ri.List(lctx, metav1.ListOptions{})

			mu.Lock()
			defer mu.Unlock()
			if lerr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skip %s: %v", gvr.String(), lerr))
				return
			}
			for i := range ul.Items {
				item := ul.Items[i]
				res.Objects = append(res.Objects, &item)
			}
		}()
	}
	wg.Wait()
}

func canList(r metav1.APIResource) bool {
	for _, v := range r.Verbs {
		if v == "list" {
			return true
		}
	}
	return false
}
