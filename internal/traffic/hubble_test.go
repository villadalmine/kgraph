package traffic

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

func ep(ns, pod, wlKind, wlName string, labels ...string) *flowpb.Endpoint {
	e := &flowpb.Endpoint{Namespace: ns, PodName: pod, Labels: labels}
	if wlName != "" {
		e.Workloads = []*flowpb.Workload{{Kind: wlKind, Name: wlName}}
	}
	return e
}

func flow(src, dst *flowpb.Endpoint) *flowpb.Flow {
	return &flowpb.Flow{Source: src, Destination: dst, Verdict: flowpb.Verdict_FORWARDED}
}

// The same workload seen with and without Hubble workload info collapses to a
// single node, with summed edge weights (spec 0005, AC1).
func TestReconcileMergesPodAndWorkload(t *testing.T) {
	client := ep("clients", "caller-aaaaaaaaaa-bbbbb", "Deployment", "caller")

	a := newAggregator(false)
	// flow 1: destination carries workload info -> Deployment/web
	a.add(flow(client, ep("apps", "web-7f69b95dc7-ccrf5", "Deployment", "web", "k8s:app=web")))
	// flow 2: same pod, NO workload info -> falls back to Pod/web (rsHash stripped)
	a.add(flow(client, ep("apps", "web-7f69b95dc7-ccrf5", "", "")))

	g := a.build()

	webNodes := 0
	for _, n := range g.Nodes {
		if n.Namespace == "apps" && n.Name == "web" {
			webNodes++
			if n.Kind != "Deployment" {
				t.Errorf("canonical web node should keep Deployment kind, got %q", n.Kind)
			}
			if n.Labels["app"] != "web" {
				t.Errorf("merged labels missing app=web: %v", n.Labels)
			}
		}
	}
	if webNodes != 1 {
		t.Fatalf("expected 1 web node after reconcile, got %d", webNodes)
	}

	// The two flows collapse onto one edge with weight 2.
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge after collapse, got %d", len(g.Edges))
	}
	if g.Edges[0].Weight != 2 {
		t.Errorf("expected summed weight 2, got %d", g.Edges[0].Weight)
	}
}

// A StatefulSet pod seen only without workload info (ordinal-suffixed name)
// still reconciles with its workload node (spec 0011).
func TestReconcileStatefulSetOrdinal(t *testing.T) {
	client := ep("clients", "caller-0", "StatefulSet", "caller")
	a := newAggregator(false)
	a.add(flow(client, ep("data", "db-0", "StatefulSet", "db"))) // with workload info
	a.add(flow(client, ep("data", "db-0", "", "")))              // ordinal-only fallback
	g := a.build()
	dbNodes := 0
	for _, n := range g.Nodes {
		if n.Namespace == "data" && n.Name == "db" {
			dbNodes++
		}
	}
	if dbNodes != 1 {
		t.Fatalf("expected db StatefulSet to be a single node, got %d", dbNodes)
	}
}

// Distinct workloads and external endpoints are not merged (spec 0005, AC2).
func TestReconcileKeepsDistinct(t *testing.T) {
	a := newAggregator(false)
	src := ep("apps", "web-1-aaaaa", "Deployment", "web")
	a.add(flow(src, ep("apps", "api-2-bbbbb", "Deployment", "api"))) // different workload
	a.add(flow(src, ep("", "", "", "", "reserved:world")))           // external

	g := a.build()
	if len(g.Nodes) != 3 {
		t.Fatalf("expected 3 distinct nodes (web, api, world), got %d", len(g.Nodes))
	}
}
