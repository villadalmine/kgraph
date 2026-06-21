package traffic

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// transportCreds: default is insecure; WithTLS yields TLS creds; a bad CA errors
// clearly (spec 0009, AC2/AC3).
func TestTransportCreds(t *testing.T) {
	var def dialConfig
	c, err := def.transportCreds()
	if err != nil || c == nil || c.Info().SecurityProtocol != "insecure" {
		t.Fatalf("default should be insecure creds, got %v err=%v", c, err)
	}

	var d dialConfig
	WithTLS(true, "relay.example", "")(&d)
	if !d.useTLS || !d.skipVerify || d.serverName != "relay.example" {
		t.Fatalf("WithTLS did not set config: %+v", d)
	}
	tc, err := d.transportCreds()
	if err != nil || tc == nil || tc.Info().SecurityProtocol != "tls" {
		t.Fatalf("expected tls creds, got %v err=%v", tc, err)
	}

	// A garbage CA bundle is rejected.
	if _, err := (dialConfig{useTLS: true, caPEM: []byte("not a cert")}).transportCreds(); err == nil {
		t.Error("invalid CA bundle should error")
	}
	// An invalid client keypair (mTLS) is rejected.
	if _, err := (dialConfig{useTLS: true, certPEM: []byte("x"), keyPEM: []byte("y")}).transportCreds(); err == nil {
		t.Error("invalid client keypair should error")
	}
}

func TestMTLSOption(t *testing.T) {
	var d dialConfig
	WithMTLSPEM([]byte("ca"), []byte("cert"), []byte("key"))(&d)
	if !d.useTLS || string(d.caPEM) != "ca" || string(d.certPEM) != "cert" || string(d.keyPEM) != "key" {
		t.Errorf("WithMTLSPEM did not set config: %+v", d)
	}
}

// prometheusPort reads the named/declared port from the pod spec (spec 0009, AC1).
func TestPrometheusPort(t *testing.T) {
	podWithWeb := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: 9091}}},
	}}}
	if got := prometheusPort(podWithWeb); got != 9091 {
		t.Errorf("want named-port 9091, got %d", got)
	}

	podWith9090 := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Ports: []corev1.ContainerPort{{Name: "grpc", ContainerPort: 9090}}},
	}}}
	if got := prometheusPort(podWith9090); got != 9090 {
		t.Errorf("want 9090 fallback, got %d", got)
	}

	bare := &corev1.Pod{}
	if got := prometheusPort(bare); got != promPort {
		t.Errorf("want default %d, got %d", promPort, got)
	}
}
