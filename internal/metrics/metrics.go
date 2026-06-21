// Package metrics is a tiny, dependency-free Prometheus HTTP API client used to
// enrich the traffic view with real throughput rates (bytes/sec) per workload.
// See specs/0002-prometheus-rates.md.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sample is one Prometheus vector result: its label set and instant value.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Client talks to a Prometheus HTTP API at base (e.g. "http://127.0.0.1:9090").
type Client struct {
	base string
	http *http.Client
}

// New returns a Client for the given base URL.
func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 15 * time.Second}}
}

// Query runs an instant PromQL query and returns the vector result.
func (c *Client) Query(ctx context.Context, promQL string) ([]Sample, error) {
	u := c.base + "/api/v1/query?query=" + url.QueryEscape(promQL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus query failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return parseVector(body)
}

// parseVector decodes a Prometheus `resultType:"vector"` response body.
func parseVector(body []byte) ([]Sample, error) {
	var r struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"` // [<ts float>, "<value string>"]
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decoding prometheus response: %w", err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s", r.Error)
	}
	out := make([]Sample, 0, len(r.Data.Result))
	for _, res := range r.Data.Result {
		if len(res.Value) != 2 {
			continue
		}
		s, ok := res.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}
		out = append(out, Sample{Labels: res.Metric, Value: v})
	}
	return out, nil
}
