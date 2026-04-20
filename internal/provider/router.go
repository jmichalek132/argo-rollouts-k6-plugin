package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
)

// Compile-time interface check.
var _ Provider = (*Router)(nil)

// Router multiplexes provider calls to the correct backend
// based on the Provider field in PluginConfig (per D-01).
type Router struct {
	providers map[string]Provider
	fallback  string
}

// RouterOption configures a Router.
type RouterOption func(*Router)

// WithProvider registers a named provider backend.
// Provider names use exact string matching -- no normalization is applied.
// Valid names: "grafana-cloud", "k6-operator".
func WithProvider(name string, p Provider) RouterOption {
	return func(r *Router) {
		r.providers[name] = p
	}
}

// NewRouter creates a Router with the given provider backends (per D-03).
// The fallback provider is "grafana-cloud" for backward compatibility (per D-02).
func NewRouter(opts ...RouterOption) *Router {
	r := &Router{
		providers: make(map[string]Provider),
		fallback:  "grafana-cloud",
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// resolve returns the provider for the given config.
// Empty cfg.Provider falls back to "grafana-cloud" (per D-02).
// Provider names are matched exactly -- no case folding or whitespace trimming.
// This is intentional: provider names come from YAML config which is case-sensitive.
func (r *Router) resolve(cfg *PluginConfig) (Provider, error) {
	name := cfg.Provider
	if name == "" {
		name = r.fallback
	}
	p, ok := r.providers[name]
	if !ok {
		registered := make([]string, 0, len(r.providers))
		for k := range r.providers {
			registered = append(registered, k)
		}
		sort.Strings(registered)
		return nil, fmt.Errorf("unknown provider %q (registered: %v)", name, registered)
	}
	return p, nil
}

// TriggerRun dispatches to the resolved provider.
func (r *Router) TriggerRun(ctx context.Context, cfg *PluginConfig) (string, error) {
	p, err := r.resolve(cfg)
	if err != nil {
		return "", err
	}
	slog.Debug("routing TriggerRun", "provider", p.Name())
	return p.TriggerRun(ctx, cfg)
}

// GetRunResult dispatches to the resolved provider.
func (r *Router) GetRunResult(ctx context.Context, cfg *PluginConfig, runID string) (*RunResult, error) {
	p, err := r.resolve(cfg)
	if err != nil {
		return nil, err
	}
	slog.Debug("routing GetRunResult", "provider", p.Name(), "runID", runID)
	return p.GetRunResult(ctx, cfg, runID)
}

// StopRun dispatches to the resolved provider.
func (r *Router) StopRun(ctx context.Context, cfg *PluginConfig, runID string) error {
	p, err := r.resolve(cfg)
	if err != nil {
		return err
	}
	slog.Debug("routing StopRun", "provider", p.Name(), "runID", runID)
	return p.StopRun(ctx, cfg, runID)
}

// Cleanup dispatches to the resolved provider.
//
// Called by K6MetricProvider.GarbageCollect (and the step plugin's terminal-state
// hook in Phase 11-02) once per runID stored in measurement metadata. The
// resolved provider decides whether cleanup is a real delete (k6-operator) or
// a no-op (grafana-cloud -- Cloud API retains runs server-side).
func (r *Router) Cleanup(ctx context.Context, cfg *PluginConfig, runID string) error {
	p, err := r.resolve(cfg)
	if err != nil {
		return err
	}
	slog.Debug("routing Cleanup", "provider", p.Name(), "runID", runID)
	return p.Cleanup(ctx, cfg, runID)
}

// Name returns "router" for the interface contract.
// Individual dispatch calls log the resolved provider name via p.Name().
func (r *Router) Name() string {
	return "router"
}
