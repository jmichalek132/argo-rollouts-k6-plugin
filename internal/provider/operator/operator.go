package operator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Compile-time interface check.
var _ provider.Provider = (*K6OperatorProvider)(nil)

// K6OperatorProvider implements the Provider interface for in-cluster
// k6-operator TestRun execution (per D-01).
type K6OperatorProvider struct {
	clientOnce sync.Once
	client     kubernetes.Interface
	dynClient  dynamic.Interface
	clientErr  error
}

// Option configures a K6OperatorProvider.
type Option func(*K6OperatorProvider)

// WithClient injects a Kubernetes client, bypassing lazy InClusterConfig (per D-07).
// Used in tests with fake.NewSimpleClientset().
func WithClient(c kubernetes.Interface) Option {
	return func(p *K6OperatorProvider) {
		p.client = c
		// Mark clientOnce as done so ensureClient() returns the injected client.
		p.clientOnce.Do(func() {})
	}
}

// WithDynClient injects a dynamic Kubernetes client for testing.
// Must be used alongside WithClient -- WithClient marks clientOnce as done,
// so WithDynClient only needs to set the field.
func WithDynClient(c dynamic.Interface) Option {
	return func(p *K6OperatorProvider) {
		p.dynClient = c
	}
}

// NewK6OperatorProvider creates a new k6-operator provider (per D-03).
func NewK6OperatorProvider(opts ...Option) *K6OperatorProvider {
	p := &K6OperatorProvider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the provider identifier.
func (p *K6OperatorProvider) Name() string {
	return "k6-operator"
}

// ensureClient lazily initializes the Kubernetes client via sync.Once (per D-06).
//
// DESIGN DECISION (addresses HIGH review concern -- sync.Once permanent failure):
// sync.Once executes the init function exactly once. If rest.InClusterConfig() fails,
// the error is cached permanently in p.clientErr. Every subsequent call to ensureClient()
// returns the same error without retrying.
//
// This is INTENTIONAL. InClusterConfig reads the service account token from the pod
// filesystem (/var/run/secrets/kubernetes.io/serviceaccount/token). Failure means the
// pod is misconfigured (wrong RBAC, missing service account mount). These are not
// transient errors -- they require a pod restart with corrected configuration.
//
// If retry-on-failure were needed (it is not for this use case), replace sync.Once
// with a mutex + cached state pattern.
func (p *K6OperatorProvider) ensureClient() (kubernetes.Interface, dynamic.Interface, error) {
	p.clientOnce.Do(func() {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			p.clientErr = fmt.Errorf("in-cluster config: %w", err)
			slog.Error("kubernetes client initialization failed permanently",
				"error", p.clientErr,
				"provider", p.Name(),
			)
			return
		}
		p.client, p.clientErr = kubernetes.NewForConfig(cfg)
		if p.clientErr != nil {
			return
		}
		p.dynClient, p.clientErr = dynamic.NewForConfig(cfg)
		if p.clientErr == nil {
			slog.Info("kubernetes client initialized",
				"provider", p.Name(),
			)
		}
	})
	return p.client, p.dynClient, p.clientErr
}

// readScript reads a k6 script from a Kubernetes ConfigMap (per D-08, D-10).
// Validates: ConfigMap existence, key presence, non-empty content.
//
// Namespace fallback chain (addresses pass 2 HIGH review concern):
//  1. cfg.Namespace (explicit user config from AnalysisTemplate/Rollout YAML)
//  2. "default" (fallback when namespace is empty)
//
// Phase 8 namespace injection plan:
// In Phase 8, the metric plugin layer (metric.go) will extract the namespace from
// AnalysisRun.ObjectMeta.Namespace, and the step plugin layer (step.go) will extract
// it from Rollout.ObjectMeta.Namespace. Both will populate cfg.Namespace before calling
// provider methods. This means users who do not set namespace explicitly in their YAML
// will get the rollout's namespace automatically. For Phase 7, "default" is the fallback
// when cfg.Namespace is unset because the rollout namespace plumbing does not exist yet.
func (p *K6OperatorProvider) readScript(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	client, _, err := p.ensureClient()
	if err != nil {
		return "", err
	}

	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}

	cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, cfg.ConfigMapRef.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get configmap %s/%s: %w", ns, cfg.ConfigMapRef.Name, err)
	}

	script, ok := cm.Data[cfg.ConfigMapRef.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in configmap %s/%s", cfg.ConfigMapRef.Key, ns, cfg.ConfigMapRef.Name)
	}
	if script == "" {
		return "", fmt.Errorf("key %q in configmap %s/%s is empty", cfg.ConfigMapRef.Key, ns, cfg.ConfigMapRef.Name)
	}

	slog.Info("k6 script loaded from configmap",
		"configmap", cfg.ConfigMapRef.Name,
		"key", cfg.ConfigMapRef.Key,
		"namespace", ns,
		"scriptLen", len(script),
		"provider", p.Name(),
	)
	return script, nil
}

// Validate checks k6-operator-specific config fields (per D-05).
// Called by the Router before dispatching. Separated from the Provider interface
// to avoid changing the interface contract.
//
// SCOPE: Syntactic validation only -- delegates to cfg.ValidateK6Operator() which
// checks struct field presence (ConfigMapRef non-nil, Name non-empty, Key non-empty).
// Does NOT call ensureClient(), readScript(), or any Kubernetes API.
// ConfigMap existence is verified later by readScript() during actual execution.
// This distinction addresses the pass 2 MEDIUM review concern about Validate() scope.
func (p *K6OperatorProvider) Validate(cfg *provider.PluginConfig) error {
	return cfg.ValidateK6Operator()
}

// TriggerRun validates config and reads the k6 script (Phase 7 stub).
// Phase 8 will replace this with real TestRun CR creation.
func (p *K6OperatorProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	if err := cfg.ValidateK6Operator(); err != nil {
		return "", err
	}
	script, err := p.readScript(ctx, cfg)
	if err != nil {
		return "", err
	}
	slog.Info("k6 script ready for execution",
		"configmap", cfg.ConfigMapRef.Name,
		"key", cfg.ConfigMapRef.Key,
		"scriptLen", len(script),
		"provider", p.Name(),
	)
	// Phase 8 will: createTestRun(ctx, client, cfg, script) -> runID
	return "", fmt.Errorf("k6-operator provider not yet implemented (Phase 8)")
}

// GetRunResult is a Phase 7 stub. Phase 8 will implement TestRun status polling.
func (p *K6OperatorProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
	return nil, fmt.Errorf("k6-operator provider not yet implemented (Phase 8)")
}

// StopRun is a Phase 7 stub. Phase 8 will implement TestRun deletion.
func (p *K6OperatorProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
	return fmt.Errorf("k6-operator provider not yet implemented (Phase 8)")
}
