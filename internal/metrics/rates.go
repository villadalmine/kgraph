package metrics

import (
	"context"
	"fmt"
	"strings"
)

// Rate is per-workload network throughput in bytes/second.
type Rate struct {
	Rx float64 // receive
	Tx float64 // transmit
}

// WorkloadRates returns rx/tx throughput per workload in a namespace, derived
// from cAdvisor container_network_*_bytes_total rate()s (5m window), summed from
// pods up to their workload via WorkloadFromPod.
func (c *Client) WorkloadRates(ctx context.Context, ns string) (map[string]Rate, error) {
	rx, err := c.Query(ctx, networkRateQuery("receive", ns))
	if err != nil {
		return nil, err
	}
	tx, err := c.Query(ctx, networkRateQuery("transmit", ns))
	if err != nil {
		return nil, err
	}
	out := map[string]Rate{}
	add := func(samples []Sample, recv bool) {
		for _, s := range samples {
			wl := WorkloadFromPod(s.Labels["pod"])
			if wl == "" {
				continue
			}
			r := out[wl]
			if recv {
				r.Rx += s.Value
			} else {
				r.Tx += s.Value
			}
			out[wl] = r
		}
	}
	add(rx, true)
	add(tx, false)
	return out, nil
}

func networkRateQuery(dir, ns string) string {
	return fmt.Sprintf(
		`sum by (namespace,pod) (rate(container_network_%s_bytes_total{namespace=%q}[5m]))`,
		dir, ns)
}

// WorkloadFromPod derives a workload name from a pod name by stripping the
// controller-generated suffixes: ReplicaSet+pod hashes (Deployment), pod hashes
// (DaemonSet/bare ReplicaSet) and ordinals (StatefulSet). Heuristic but pure and
// good enough to match Hubble's workload-level aggregation.
func WorkloadFromPod(pod string) string {
	parts := strings.Split(pod, "-")
	n := len(parts)
	if n < 2 {
		return pod
	}
	last := parts[n-1]
	// Deployment: <name>-<rsHash>-<podHash>, both hash-like.
	if n >= 3 && isPodHash(last) && isHashLike(parts[n-2]) {
		return strings.Join(parts[:n-2], "-")
	}
	// StatefulSet ordinal: <name>-<N>.
	if isAllDigits(last) {
		return strings.Join(parts[:n-1], "-")
	}
	// DaemonSet / bare ReplicaSet: <name>-<podHash>.
	if isPodHash(last) {
		return strings.Join(parts[:n-1], "-")
	}
	return pod
}

// isPodHash matches Kubernetes' 5-char pod-template/pod hash alphabet.
func isPodHash(s string) bool {
	if len(s) != 5 {
		return false
	}
	return isHashLike(s)
}

// isHashLike reports whether s looks like a lowercase alphanumeric hash segment
// (contains at least one digit, to avoid eating real name words).
func isHashLike(s string) bool {
	if len(s) < 4 {
		return false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			return false
		}
	}
	return hasDigit
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// HumanRate formats bytes/second compactly (e.g. "1.2 kB/s").
func HumanRate(bps float64) string {
	const unit = 1000.0
	if bps < unit {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	units := []string{"kB", "MB", "GB", "TB"}
	v := bps / unit
	for _, u := range units {
		if v < unit {
			return fmt.Sprintf("%.1f %s/s", v, u)
		}
		v /= unit
	}
	return fmt.Sprintf("%.1f PB/s", v*unit)
}
