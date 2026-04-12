package cloud

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	k6 "github.com/grafana/k6-cloud-openapi-client-go/k6"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Compile-time interface check.
var _ provider.Provider = (*GrafanaCloudProvider)(nil)

// GrafanaCloudProvider implements the Provider interface for Grafana Cloud k6.
type GrafanaCloudProvider struct {
	baseURL string
}

// Option configures a GrafanaCloudProvider.
type Option func(*GrafanaCloudProvider)

// WithBaseURL overrides the default API base URL (for testing).
func WithBaseURL(url string) Option {
	return func(p *GrafanaCloudProvider) {
		p.baseURL = url
	}
}

// NewGrafanaCloudProvider creates a new Grafana Cloud k6 provider.
func NewGrafanaCloudProvider(opts ...Option) *GrafanaCloudProvider {
	p := &GrafanaCloudProvider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the provider identifier.
func (p *GrafanaCloudProvider) Name() string {
	return "grafana-cloud-k6"
}

// newK6Client creates a configured k6 API client with auth from the PluginConfig.
// Per D-07: stateless, creates client per call.
func (p *GrafanaCloudProvider) newK6Client(ctx context.Context, cfg *provider.PluginConfig) (context.Context, *k6.APIClient) {
	k6Cfg := k6.NewConfiguration()
	k6Cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	if p.baseURL != "" {
		k6Cfg.Servers = k6.ServerConfigurations{{URL: p.baseURL}}
	}
	client := k6.NewAPIClient(k6Cfg)
	// Auth: Bearer token via context key (research finding #4).
	ctx = context.WithValue(ctx, k6.ContextAccessToken, cfg.APIToken)
	return ctx, client
}

// TriggerRun starts a new k6 test run. Returns the run ID as a string.
func (p *GrafanaCloudProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	testID, err := strconv.ParseInt(cfg.TestID, 10, 64)
	if err != nil || testID <= 0 || testID > math.MaxInt32 {
		return "", fmt.Errorf("invalid testId %q: must be a positive integer ≤ %d", cfg.TestID, math.MaxInt32)
	}
	stackID, err := strconv.ParseInt(cfg.StackID, 10, 64)
	if err != nil || stackID <= 0 || stackID > math.MaxInt32 {
		return "", fmt.Errorf("invalid stackId %q: must be a positive integer ≤ %d", cfg.StackID, math.MaxInt32)
	}

	ctx, client := p.newK6Client(ctx, cfg)

	testRun, resp, err := client.LoadTestsAPI.LoadTestsStart(ctx, int32(testID)).
		XStackId(int32(stackID)).
		Execute()
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return "", fmt.Errorf("trigger run for test %s: %w", cfg.TestID, err)
	}

	runID := strconv.FormatInt(int64(testRun.GetId()), 10)
	slog.Debug("triggered test run",
		"testId", cfg.TestID,
		"runId", runID,
		"provider", p.Name(),
	)
	return runID, nil
}

// GetRunResult returns current status and metrics for a run.
// Returns partial metrics during active runs (State == Running).
func (p *GrafanaCloudProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
	runIDInt, err := strconv.ParseInt(runID, 10, 64)
	if err != nil || runIDInt <= 0 || runIDInt > math.MaxInt32 {
		return nil, fmt.Errorf("invalid runId %q: must be a positive integer ≤ %d", runID, math.MaxInt32)
	}
	stackID, err := strconv.ParseInt(cfg.StackID, 10, 64)
	if err != nil || stackID <= 0 || stackID > math.MaxInt32 {
		return nil, fmt.Errorf("invalid stackId %q: must be a positive integer ≤ %d", cfg.StackID, math.MaxInt32)
	}

	ctx, client := p.newK6Client(ctx, cfg)

	testRun, resp, err := client.TestRunsAPI.TestRunsRetrieve(ctx, int32(runIDInt)).
		XStackId(int32(stackID)).
		Execute()
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("get run result for %s: %w", runID, err)
	}

	statusDetails := testRun.GetStatusDetails()
	statusType := statusDetails.GetType()
	result := testRun.Result.Get() // *string or nil

	state := mapToRunState(statusType, result)

	slog.Info("polled test run status",
		"runId", runID,
		"statusType", statusType,
		"state", state,
		"provider", p.Name(),
	)

	runResult := &provider.RunResult{
		State:            state,
		TestRunURL:       fmt.Sprintf("https://app.k6.io/runs/%s", runID),
		ThresholdsPassed: isThresholdPassed(result),
	}

	// Populate aggregate metrics from v5 API for terminal Passed/Failed runs.
	if state == provider.Passed || state == provider.Failed {
		p.populateAggregateMetrics(ctx, cfg, runID, runResult)
	}

	return runResult, nil
}

// populateAggregateMetrics queries the v5 aggregate endpoint to fill in
// HTTPReqFailed, HTTPReqDuration, and HTTPReqs on a RunResult.
// Failures are logged at Warn level but do not cause GetRunResult to fail
// (graceful degradation -- v6 status/thresholds are the primary data).
func (p *GrafanaCloudProvider) populateAggregateMetrics(ctx context.Context, cfg *provider.PluginConfig, runID string, result *provider.RunResult) {
	// http_req_failed rate
	if val, err := p.QueryAggregateMetric(ctx, cfg, runID, AggregateMetricQuery{
		MetricName: "http_req_failed",
		QueryFunc:  "rate",
	}); err != nil {
		slog.Warn("v5 aggregate query failed", "metric", "http_req_failed", "runId", runID, "error", err)
	} else {
		result.HTTPReqFailed = val
	}

	// http_req_duration percentiles
	for _, pct := range []struct {
		quantile string
		field    *float64
	}{
		{"0.50", &result.HTTPReqDuration.P50},
		{"0.95", &result.HTTPReqDuration.P95},
		{"0.99", &result.HTTPReqDuration.P99},
	} {
		if val, err := p.QueryAggregateMetric(ctx, cfg, runID, AggregateMetricQuery{
			MetricName: "http_req_duration",
			QueryFunc:  fmt.Sprintf("histogram_quantile(%s)", pct.quantile),
		}); err != nil {
			slog.Warn("v5 aggregate query failed", "metric", "http_req_duration", "quantile", pct.quantile, "runId", runID, "error", err)
		} else {
			*pct.field = val
		}
	}

	// http_reqs rate
	if val, err := p.QueryAggregateMetric(ctx, cfg, runID, AggregateMetricQuery{
		MetricName: "http_reqs",
		QueryFunc:  "rate",
	}); err != nil {
		slog.Warn("v5 aggregate query failed", "metric", "http_reqs", "runId", runID, "error", err)
	} else {
		result.HTTPReqs = val
	}
}

// StopRun requests cancellation of a running test.
// No-op if the run is already in a terminal state.
func (p *GrafanaCloudProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
	runIDInt, err := strconv.ParseInt(runID, 10, 64)
	if err != nil || runIDInt <= 0 || runIDInt > math.MaxInt32 {
		return fmt.Errorf("invalid runId %q: must be a positive integer ≤ %d", runID, math.MaxInt32)
	}
	stackID, err := strconv.ParseInt(cfg.StackID, 10, 64)
	if err != nil || stackID <= 0 || stackID > math.MaxInt32 {
		return fmt.Errorf("invalid stackId %q: must be a positive integer ≤ %d", cfg.StackID, math.MaxInt32)
	}

	ctx, client := p.newK6Client(ctx, cfg)

	resp, err := client.TestRunsAPI.TestRunsAbort(ctx, int32(runIDInt)).
		XStackId(int32(stackID)).
		Execute()
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("stop run %s: %w", runID, err)
	}

	slog.Info("stopped test run",
		"runId", runID,
		"provider", p.Name(),
	)
	return nil
}
