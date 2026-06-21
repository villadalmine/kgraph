package graph

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Build constructs a graph from a flat list of namespaced objects, inferring
// owner, selector and reference edges.
func Build(objs []*unstructured.Unstructured) *Graph {
	g := New()
	for _, o := range objs {
		g.AddObject(o)
	}
	// Index pods (and the group "") by name for selector matching within ns.
	for _, o := range objs {
		buildOwnerEdges(g, o)
		buildRefEdges(g, o)
		buildSelectorEdges(g, objs, o)
	}
	return g
}

// buildOwnerEdges links each object to its ownerReferences (owner -> child).
func buildOwnerEdges(g *Graph, o *unstructured.Unstructured) {
	child := g.AddObject(o)
	for _, ref := range o.GetOwnerReferences() {
		group := groupFromAPIVersion(ref.APIVersion)
		ownerID := NodeID(group, ref.Kind, o.GetNamespace(), ref.Name)
		g.AddEdge(ownerID, child.ID, RelOwner, "")
	}
}

// buildSelectorEdges links selector-bearing objects (Service, workloads,
// NetworkPolicy) to the pods/objects they select within the same namespace.
func buildSelectorEdges(g *Graph, objs []*unstructured.Unstructured, o *unstructured.Unstructured) {
	src := g.AddObject(o)
	sel := extractSelector(o)
	if len(sel) == 0 {
		return
	}
	for _, cand := range objs {
		if cand.GetNamespace() != o.GetNamespace() {
			continue
		}
		if cand.GetUID() == o.GetUID() {
			continue
		}
		// Only match pods/workload children to avoid noise.
		if !isSelectableTarget(cand.GetKind()) {
			continue
		}
		if labelsMatch(sel, cand.GetLabels()) {
			g.AddEdge(src.ID, g.AddObject(cand).ID, RelSelector, "")
		}
	}
}

// buildRefEdges links objects to ConfigMaps/Secrets/PVCs/ServiceAccounts they
// reference, and routing objects to their backend services.
func buildRefEdges(g *Graph, o *unstructured.Unstructured) {
	src := g.AddObject(o)
	ns := o.GetNamespace()

	addRef := func(group, kind, name, note string) {
		if name == "" {
			return
		}
		g.AddEdge(src.ID, NodeID(group, kind, ns, name), RelRef, note)
	}

	switch o.GetKind() {
	case "Pod":
		addPodRefs(g, o, addRef)
	case "Ingress":
		walkStringFields(o.Object, "service", func(name string) {
			addRef("", "Service", name, "backend")
		})
	case "HTTPRoute", "GRPCRoute", "TCPRoute":
		// Gateway API: spec.rules[].backendRefs[].name -> Service
		forEachBackendRef(o.Object, func(name string) {
			addRef("", "Service", name, "backend")
		})
	case "PersistentVolumeClaim":
		if vol, ok, _ := unstructured.NestedString(o.Object, "spec", "volumeName"); ok {
			addRef("", "PersistentVolume", vol, "volume")
		}
	}
}

func addPodRefs(g *Graph, o *unstructured.Unstructured, addRef func(group, kind, name, note string)) {
	if sa, ok, _ := unstructured.NestedString(o.Object, "spec", "serviceAccountName"); ok {
		addRef("", "ServiceAccount", sa, "serviceAccount")
	}
	vols, _, _ := unstructured.NestedSlice(o.Object, "spec", "volumes")
	for _, v := range vols {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if cm, ok, _ := unstructured.NestedString(vm, "configMap", "name"); ok {
			addRef("", "ConfigMap", cm, "configMap")
		}
		if s, ok, _ := unstructured.NestedString(vm, "secret", "secretName"); ok {
			addRef("", "Secret", s, "secret")
		}
		if pvc, ok, _ := unstructured.NestedString(vm, "persistentVolumeClaim", "claimName"); ok {
			addRef("", "PersistentVolumeClaim", pvc, "pvc")
		}
	}
	// envFrom / valueFrom references in containers.
	for _, path := range [][]string{{"spec", "containers"}, {"spec", "initContainers"}} {
		cs, _, _ := unstructured.NestedSlice(o.Object, path...)
		for _, c := range cs {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			envFrom, _, _ := unstructured.NestedSlice(cm, "envFrom")
			for _, e := range envFrom {
				em, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				if n, ok, _ := unstructured.NestedString(em, "configMapRef", "name"); ok {
					addRef("", "ConfigMap", n, "envFrom")
				}
				if n, ok, _ := unstructured.NestedString(em, "secretRef", "name"); ok {
					addRef("", "Secret", n, "envFrom")
				}
			}
		}
	}
}

// extractSelector returns the matchLabels selector of an object if present,
// handling both spec.selector.matchLabels and the bare spec.selector map
// (used by Service).
func extractSelector(o *unstructured.Unstructured) map[string]string {
	if ml, ok, _ := unstructured.NestedStringMap(o.Object, "spec", "selector", "matchLabels"); ok && len(ml) > 0 {
		return ml
	}
	if sm, ok, _ := unstructured.NestedStringMap(o.Object, "spec", "selector"); ok && len(sm) > 0 {
		return sm
	}
	// NetworkPolicy / CiliumNetworkPolicy: spec.podSelector.matchLabels
	if ml, ok, _ := unstructured.NestedStringMap(o.Object, "spec", "podSelector", "matchLabels"); ok && len(ml) > 0 {
		return ml
	}
	if ml, ok, _ := unstructured.NestedStringMap(o.Object, "spec", "endpointSelector", "matchLabels"); ok && len(ml) > 0 {
		return ml
	}
	return nil
}

func isSelectableTarget(kind string) bool {
	switch kind {
	case "Pod", "ReplicaSet", "Deployment", "StatefulSet", "DaemonSet", "Endpoints", "EndpointSlice":
		return true
	}
	return false
}

func labelsMatch(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func groupFromAPIVersion(apiVersion string) string {
	if i := strings.Index(apiVersion, "/"); i >= 0 {
		return apiVersion[:i]
	}
	return "" // core group
}

// walkStringFields recursively finds all string values under the given key.
func walkStringFields(m map[string]interface{}, key string, fn func(string)) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if k == key {
				fn(val)
			}
		case map[string]interface{}:
			if k == key {
				if name, ok := val["name"].(string); ok {
					fn(name)
				}
			}
			walkStringFields(val, key, fn)
		case []interface{}:
			for _, item := range val {
				if im, ok := item.(map[string]interface{}); ok {
					walkStringFields(im, key, fn)
				}
			}
		}
	}
}

// forEachBackendRef finds Gateway API backendRefs[].name values.
func forEachBackendRef(m map[string]interface{}, fn func(string)) {
	rules, _, _ := unstructured.NestedSlice(m, "spec", "rules")
	for _, r := range rules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		refs, _, _ := unstructured.NestedSlice(rm, "backendRefs")
		for _, ref := range refs {
			refm, ok := ref.(map[string]interface{})
			if !ok {
				continue
			}
			if name, ok := refm["name"].(string); ok {
				fn(name)
			}
		}
	}
}
