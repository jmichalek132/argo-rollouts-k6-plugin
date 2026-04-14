package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

const defaultBaseURL = "https://api.k6.io"

// maxResponseBodySize limits the v5 response body read to 1 MB to prevent OOM
// from a misbehaving or compromised server.
const maxResponseBodySize = 1 << 20

// AggregateMetricQuery defines a v5 aggregate metric query.
type AggregateMetricQuery struct {
	MetricName string // "http_req_failed", "http_req_duration", "http_reqs"
	QueryFunc  string // "rate", "histogram_quantile(0.95)", etc.
}

// isValidMetricParam validates that a metric parameter contains only safe characters
// for URL interpolation. This prevents path injection via MetricName or QueryFunc
// in the v5 aggregate endpoint URL.
func isValidMetricParam(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '(' || r == ')') {
			return false
		}
	}
	return true
}

// QueryAggregateMetric calls the k6 Cloud v5 aggregate endpoint.
// URL: {baseURL}/cloud/v5/test_runs/{runID}/query_aggregate_k6(metric='{name}',query='{func}')
func (p *GrafanaCloudProvider) QueryAggregateMetric(
	ctx context.Context,
	cfg *provider.PluginConfig,
	runID string,
	query AggregateMetricQuery,
) (float64, error) {
	if !isValidMetricParam(query.MetricName) {
		return 0, fmt.Errorf("invalid metric name %q: must match [a-zA-Z0-9_.()] only", query.MetricName)
	}
	if !isValidMetricParam(query.QueryFunc) {
		return 0, fmt.Errorf("invalid query function %q: must match [a-zA-Z0-9_.()] only", query.QueryFunc)
	}

	base := p.baseURL
	if base == "" {
		base = defaultBaseURL
	}

	url := fmt.Sprintf("%s/cloud/v5/test_runs/%s/query_aggregate_k6(metric='%s',query='%s')",
		base, runID, query.MetricName, query.QueryFunc)

	slog.Debug("querying v5 aggregate metric",
		"url", url,
		"metric", query.MetricName,
		"query", query.QueryFunc,
		"provider", p.Name(),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create v5 request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("X-Stack-Id", cfg.StackID)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("v5 aggregate query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
		return 0, fmt.Errorf("v5 aggregate query returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return 0, fmt.Errorf("read v5 response: %w", err)
	}

	return parseAggregateValue(body)
}

// aggregateResponse represents the Prometheus-style vector response from the v5 API.
type aggregateResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"` // [[timestamp, value], ...]
		} `json:"result"`
	} `json:"data"`
}

// parseAggregateValue extracts the numeric value from a v5 aggregate response body.
// Returns 0.0 for empty results (test still running or no traffic).
func parseAggregateValue(body []byte) (float64, error) {
	var resp aggregateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse aggregate response: %w", err)
	}
	if resp.Status != "success" {
		return 0, fmt.Errorf("aggregate query failed: status=%s", resp.Status)
	}
	if len(resp.Data.Result) == 0 {
		return 0, nil // No data yet (test still running or no traffic)
	}
	values := resp.Data.Result[0].Values
	if len(values) == 0 || len(values[0]) < 2 {
		return 0, nil
	}
	// Value can be float64 or string in JSON
	switch v := values[0][1].(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("unexpected value type %T", v)
	}
}
