package graph

// Overlay merges observed flow edges (from a traffic graph) onto a declarative
// base graph, so a single diagram shows both what is deployed and what actually
// talks. Flow endpoints are matched to base nodes by (namespace, name),
// preferring a workload-ish kind so a flow for "web" attaches to the Deployment
// rather than a same-named Service. Endpoints with no match (e.g. external/world)
// are added to the base. Naming-agnostic. See specs/0007-combined-topology-traffic.md.
func Overlay(base, flows *Graph) {
	type nk struct{ ns, name string }
	idx := map[nk]*Node{}
	for _, n := range base.Nodes {
		key := nk{n.Namespace, n.Name}
		if cur, ok := idx[key]; !ok || workloadRank(n.Kind) > workloadRank(cur.Kind) {
			idx[key] = n
		}
	}

	resolve := func(id string) *Node {
		fn := flows.Nodes[id]
		if fn == nil {
			return nil
		}
		key := nk{fn.Namespace, fn.Name}
		if t, ok := idx[key]; ok {
			return t
		}
		// Unmatched endpoint (external/world or a workload absent from the
		// declarative scope): add it so the flow has somewhere to land.
		nn := base.AddNode(fn.Group, fn.Kind, fn.Namespace, fn.Name)
		if nn.Labels == nil {
			nn.Labels = fn.Labels
		}
		nn.Layer = fn.Layer
		idx[key] = nn
		return nn
	}

	for _, e := range flows.Edges {
		from := resolve(e.From)
		to := resolve(e.To)
		if from == nil || to == nil {
			continue
		}
		base.AddFlowEdgeFull(from.ID, to.ID, e.Weight, e.Dropped, e.Error, e.Note)
	}
}

// workloadRank ranks kinds so flow edges attach to the most workload-like node
// among base nodes sharing a (namespace, name).
func workloadRank(kind string) int {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Workload":
		return 4
	case "ReplicaSet":
		return 3
	case "Pod":
		return 2
	case "":
		return 0
	default:
		return 1
	}
}
