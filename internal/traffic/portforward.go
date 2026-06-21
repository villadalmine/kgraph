package traffic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	relayNamespace = "kube-system"
	relayPort      = 4245 // hubble-relay container port

	promPort = 9090 // Prometheus HTTP port (universal default)
)

// PortForwardRelay opens a local port forwarded to a hubble-relay pod and
// returns the local address (127.0.0.1:PORT) plus a stop function. The caller
// must call stop() when done.
func PortForwardRelay(ctx context.Context, cfg *rest.Config) (addr string, stop func(), err error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	pod, err := findRelayPod(ctx, cs)
	if err != nil {
		return "", nil, err
	}
	return forwardPod(ctx, cfg, cs, pod.Namespace, pod.Name, relayPort, "hubble-relay")
}

// PortForwardPrometheus opens a local port forwarded to a Prometheus pod found
// anywhere in the cluster (no hardcoded namespace) via chart-neutral selectors,
// and returns the local address plus a stop function.
func PortForwardPrometheus(ctx context.Context, cfg *rest.Config) (addr string, stop func(), err error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	// namespace "" => search all namespaces; works regardless of where (or with
	// which chart) Prometheus is deployed.
	pod, err := findPod(ctx, cs, "",
		"app.kubernetes.io/name=prometheus",
		"app.kubernetes.io/component=prometheus",
		"app=prometheus")
	if err != nil {
		return "", nil, fmt.Errorf("locating Prometheus pod (try --prom): %w", err)
	}
	return forwardPod(ctx, cfg, cs, pod.Namespace, pod.Name, prometheusPort(pod), "prometheus")
}

// prometheusPort reads the HTTP port from the pod's container spec (named
// web/http/http-web/metrics, else a 9090 port), falling back to the default.
func prometheusPort(pod *corev1.Pod) int {
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			switch p.Name {
			case "web", "http", "http-web", "metrics":
				return int(p.ContainerPort)
			}
		}
	}
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			if p.ContainerPort == promPort {
				return promPort
			}
		}
	}
	return promPort
}

// forwardPod port-forwards a single pod's targetPort to a random local port.
func forwardPod(ctx context.Context, cfg *rest.Config, cs kubernetes.Interface, namespace, pod string, targetPort int, label string) (addr string, stop func(), err error) {
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return "", nil, err
	}
	reqURL := cs.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, &url.URL{
		Scheme: reqURL.Scheme, Host: reqURL.Host, Path: reqURL.Path,
	})

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	// "0:<port>" => pick a random free local port forwarded to targetPort.
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", targetPort)}, stopCh, readyCh, nil, nil)
	if err != nil {
		return "", nil, err
	}

	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()

	select {
	case <-readyCh:
	case err := <-errCh:
		return "", nil, fmt.Errorf("port-forward to %s failed: %w", label, err)
	case <-ctx.Done():
		close(stopCh)
		return "", nil, ctx.Err()
	}

	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return "", nil, fmt.Errorf("resolving forwarded port: %w", err)
	}
	return fmt.Sprintf("127.0.0.1:%d", ports[0].Local), func() { close(stopCh) }, nil
}

// MTLSFromSecret reads a TLS secret (default kube-system/hubble-relay-client-certs
// when ref is "") and returns a FetchFlows Option configuring mTLS from its
// ca.crt/tls.crt/tls.key. ref is "namespace/name". Use for a relay that requires
// client certificates. Agnostic: the secret name/namespace are overridable.
func MTLSFromSecret(ctx context.Context, cfg *rest.Config, ref string) (Option, error) {
	ns, name := "kube-system", "hubble-relay-client-certs"
	if ref != "" {
		parts := strings.SplitN(ref, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("--relay-mtls-secret must be namespace/name, got %q", ref)
		}
		ns, name = parts[0], parts[1]
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	sec, err := cs.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading mTLS secret %s/%s: %w", ns, name, err)
	}
	cert, key := sec.Data["tls.crt"], sec.Data["tls.key"]
	if len(cert) == 0 || len(key) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing tls.crt/tls.key", ns, name)
	}
	return WithMTLSPEM(sec.Data["ca.crt"], cert, key), nil
}

// findRelayPod returns a running hubble-relay pod.
func findRelayPod(ctx context.Context, cs kubernetes.Interface) (*corev1.Pod, error) {
	return findPod(ctx, cs, relayNamespace, "k8s-app=hubble-relay", "app.kubernetes.io/name=hubble-relay")
}

// findPod returns a running pod matching any of the given label selectors (tried
// in order). A namespace of "" searches all namespaces, making discovery
// agnostic to where a component is deployed.
func findPod(ctx context.Context, cs kubernetes.Interface, namespace string, selectors ...string) (*corev1.Pod, error) {
	for _, sel := range selectors {
		pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
		if err != nil {
			return nil, err
		}
		for i := range pods.Items {
			if pods.Items[i].Status.Phase == corev1.PodRunning {
				return &pods.Items[i], nil
			}
		}
	}
	scope := namespace
	if scope == "" {
		scope = "any namespace"
	}
	return nil, fmt.Errorf("no running pod found in %s for selectors %v", scope, selectors)
}
