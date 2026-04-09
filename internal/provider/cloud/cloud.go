package cloud

import (
	"context"

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

// TriggerRun starts a new k6 test run.
func (p *GrafanaCloudProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	panic("not implemented")
}

// GetRunResult returns current status and metrics for a run.
func (p *GrafanaCloudProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
	panic("not implemented")
}

// StopRun requests cancellation of a running test.
func (p *GrafanaCloudProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
	panic("not implemented")
}
