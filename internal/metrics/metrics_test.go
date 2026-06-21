package metrics

import "testing"

// parseVector decodes a recorded Prometheus vector response (spec 0002, AC1).
func TestParseVector(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"namespace":"pihole","pod":"pihole-7f69b95dc7-ccrf5"},"value":[1781987868.359,"127.63096055818663"]},
		{"metric":{"namespace":"pihole","pod":"other-0"},"value":[1781987868.359,"5"]}
	]}}`)
	got, err := parseVector(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 samples, got %d", len(got))
	}
	if got[0].Labels["pod"] != "pihole-7f69b95dc7-ccrf5" || got[0].Value < 127 || got[0].Value > 128 {
		t.Errorf("unexpected first sample: %+v", got[0])
	}
}

func TestParseVectorError(t *testing.T) {
	if _, err := parseVector([]byte(`{"status":"error","error":"bad query"}`)); err == nil {
		t.Error("expected error for status:error response")
	}
}

// WorkloadFromPod maps controller pod names to their workload (spec 0002, AC2).
func TestWorkloadFromPod(t *testing.T) {
	cases := map[string]string{
		"pihole-7f69b95dc7-ccrf5":                       "pihole",                                      // Deployment
		"prometheus-kube-prometheus-stack-prometheus-0": "prometheus-kube-prometheus-stack-prometheus", // StatefulSet
		"alloy-2m4xz":           "alloy",      // DaemonSet/RS hash
		"standalone":            "standalone", // bare pod
		"my-app-v2-abc12-x9z8q": "my-app-v2",  // name with version + hashes
	}
	for pod, want := range cases {
		if got := WorkloadFromPod(pod); got != want {
			t.Errorf("WorkloadFromPod(%q) = %q, want %q", pod, got, want)
		}
	}
}

func TestHumanRate(t *testing.T) {
	cases := map[float64]string{
		0:         "0 B/s",
		127.6:     "128 B/s",
		2048:      "2.0 kB/s",
		5_000_000: "5.0 MB/s",
	}
	for in, want := range cases {
		if got := HumanRate(in); got != want {
			t.Errorf("HumanRate(%v) = %q, want %q", in, got, want)
		}
	}
}
