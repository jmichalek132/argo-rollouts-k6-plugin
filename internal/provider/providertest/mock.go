// Package providertest provides a shared mock implementation of provider.Provider
// for use in unit tests across packages.
package providertest

import (
	"context"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// MockProvider implements provider.Provider with configurable function fields.
// Each method delegates to its corresponding function field if set, otherwise
// returns a sensible default.
type MockProvider struct {
	TriggerRunFn   func(ctx context.Context, cfg *provider.PluginConfig) (string, error)
	GetRunResultFn func(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error)
	StopRunFn      func(ctx context.Context, cfg *provider.PluginConfig, runID string) error
}

func (m *MockProvider) Name() string { return "mock" }

func (m *MockProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	if m.TriggerRunFn != nil {
		return m.TriggerRunFn(ctx, cfg)
	}
	return "mock-run-123", nil
}

func (m *MockProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
	if m.GetRunResultFn != nil {
		return m.GetRunResultFn(ctx, cfg, runID)
	}
	return &provider.RunResult{State: provider.Running}, nil
}

func (m *MockProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
	if m.StopRunFn != nil {
		return m.StopRunFn(ctx, cfg, runID)
	}
	return nil
}
