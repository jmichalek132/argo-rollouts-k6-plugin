package cloud

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

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
	testID, err := strconv.ParseInt(cfg.TestID, 10, 32)
	if err != nil {
		return "", fmt.Errorf("invalid testId %q: %w", cfg.TestID, err)
	}
	stackID, err := strconv.ParseInt(cfg.StackID, 10, 32)
	if err != nil {
		return "", fmt.Errorf("invalid stackId %q: %w", cfg.StackID, err)
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
	runIDInt, err := strconv.ParseInt(runID, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid runId %q: %w", runID, err)
	}
	stackID, err := strconv.ParseInt(cfg.StackID, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid stackId %q: %w", cfg.StackID, err)
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

	return &provider.RunResult{
		State:            state,
		TestRunURL:       fmt.Sprintf("https://app.k6.io/runs/%s", runID),
		ThresholdsPassed: isThresholdPassed(result),
		// HTTPReqFailed, HTTPReqDuration, HTTPReqs left at zero values:
		// v6 API client does not expose aggregate metric endpoints (Phase 2 enhancement).
	}, nil
}

// StopRun requests cancellation of a running test.
// No-op if the run is already in a terminal state.
func (p *GrafanaCloudProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
	runIDInt, err := strconv.ParseInt(runID, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid runId %q: %w", runID, err)
	}
	stackID, err := strconv.ParseInt(cfg.StackID, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid stackId %q: %w", cfg.StackID, err)
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
