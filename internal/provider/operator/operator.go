package operator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	logReader  PodLogReader // injectable for testing; nil = use k8sPodLogReader
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

// WithLogReader injects a PodLogReader for testing.
// Production code uses k8sPodLogReader created from the Kubernetes client.
func WithLogReader(r PodLogReader) Option {
	return func(p *K6OperatorProvider) {
		p.logReader = r
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

// ensureLogReader returns the PodLogReader, creating a k8sPodLogReader from the
// Kubernetes client if none was injected via WithLogReader.
func (p *K6OperatorProvider) ensureLogReader(client kubernetes.Interface) PodLogReader {
	if p.logReader != nil {
		return p.logReader
	}
	return &k8sPodLogReader{client: client}
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

// TriggerRun creates a k6-operator TestRun or PrivateLoadZone CR via dynamic client.
// Returns an encoded run ID containing namespace/resource/name for lifecycle identity.
//
// Validation runs BEFORE readScript (addresses MEDIUM review concern about validation
// ordering -- no I/O wasted on invalid config).
//
// Auto-detects PrivateLoadZone when Grafana Cloud credentials are present (per D-02).
// Sets OwnerReferences when cfg.AnalysisRunUID is non-empty (per D-09).
func (p *K6OperatorProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	// 1. Validate config FIRST -- before any I/O (addresses review concern: validation ordering).
	if err := cfg.ValidateK6Operator(); err != nil {
		return "", err
	}

	// 2. Read script from ConfigMap (proves ConfigMap exists).
	// NOTE: This is a best-effort pre-flight check (TOCTOU). The k6-operator
	// independently reads the ConfigMap when processing the CR. This check provides
	// an early, user-facing error message if the ConfigMap is missing or malformed,
	// but cannot prevent races where the ConfigMap is deleted between this check
	// and the operator's read.
	_, err := p.readScript(ctx, cfg)
	if err != nil {
		return "", err
	}

	// 3. Get clients.
	_, dynClient, err := p.ensureClient()
	if err != nil {
		return "", err
	}

	// 4. Resolve namespace.
	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}

	// 5. Generate CR name using cfg.RolloutName.
	rolloutName := cfg.RolloutName
	if rolloutName == "" {
		rolloutName = "unknown"
	}
	crName := testRunName(rolloutName, ns)

	// 6. Build and create CR (auto-detect TestRun vs PrivateLoadZone per D-02).
	// Owner references are set inside buildTestRun/buildPrivateLoadZone
	// using cfg.AnalysisRunUID (per D-09). If UID is empty, no owner ref is set.
	if isCloudConnected(cfg) {
		plz := buildPrivateLoadZone(cfg, ns, crName)
		obj, convErr := runtime.DefaultUnstructuredConverter.ToUnstructured(plz)
		if convErr != nil {
			return "", fmt.Errorf("convert PrivateLoadZone to unstructured: %w", convErr)
		}
		u := &unstructured.Unstructured{Object: obj}
		created, createErr := dynClient.Resource(plzGVR).Namespace(ns).Create(ctx, u, metav1.CreateOptions{})
		if createErr != nil {
			return "", fmt.Errorf("create PrivateLoadZone %s/%s: %w", ns, crName, createErr)
		}
		slog.Info("k6 PrivateLoadZone created",
			"name", created.GetName(),
			"namespace", ns,
			"provider", p.Name(),
		)
		// Encode full lifecycle identity: namespace + resource + name.
		// Addresses HIGH review concern: GetRunResult/StopRun recover GVR from runID.
		return encodeRunID(ns, "privateloadzones", created.GetName()), nil
	}

	tr := buildTestRun(cfg, cfg.ConfigMapRef.Name, cfg.ConfigMapRef.Key, ns, crName)
	obj, convErr := runtime.DefaultUnstructuredConverter.ToUnstructured(tr)
	if convErr != nil {
		return "", fmt.Errorf("convert TestRun to unstructured: %w", convErr)
	}
	u := &unstructured.Unstructured{Object: obj}
	created, createErr := dynClient.Resource(testRunGVR).Namespace(ns).Create(ctx, u, metav1.CreateOptions{})
	if createErr != nil {
		return "", fmt.Errorf("create TestRun %s/%s: %w", ns, crName, createErr)
	}
	slog.Info("k6 TestRun created",
		"name", created.GetName(),
		"namespace", ns,
		"provider", p.Name(),
	)
	// Encode full lifecycle identity: namespace + resource + name.
	return encodeRunID(ns, "testruns", created.GetName()), nil
}

// GetRunResult polls the TestRun CR status and determines pass/fail from runner pods.
//
// Decodes runID to recover namespace, GVR, and CR name (addresses HIGH review concern
// about lifecycle identity -- no re-derivation from config).
//
// Handles: missing CR (NotFound), absent stage field (freshly created CR returns Running),
// and Pitfall 2 (pods not yet terminated returns Running).
func (p *K6OperatorProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
	// Decode lifecycle identity from runID (addresses HIGH review concern).
	ns, resource, name, err := decodeRunID(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run ID: %w", err)
	}
	gvr := gvrForResource(resource)

	client, dynClient, err := p.ensureClient()
	if err != nil {
		return nil, err
	}

	// Single GET per D-04 (no polling loop).
	// Addresses MEDIUM review concern: NotFound/missing CR returns wrapped error.
	u, err := dynClient.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s %s/%s: %w", resource, ns, name, err)
	}

	// Extract stage from status.
	// Addresses MEDIUM review concern: absent stage field returns empty string
	// which stageToRunState maps to Running (keep polling until operator sets stage).
	stage, _, _ := unstructured.NestedString(u.Object, "status", "stage")
	// NOTE: error from NestedString is intentionally ignored.
	// If status.stage is missing (freshly created CR) or malformed,
	// stage will be "" which stageToRunState maps to Running.
	// This is correct behavior: keep polling until operator populates the field.

	// Map stage to RunState.
	state := stageToRunState(stage)

	slog.Debug("TestRun status polled",
		"name", name,
		"namespace", ns,
		"stage", stage,
		"state", state,
		"resource", resource,
		"provider", p.Name(),
	)

	// For terminal stages, check runner pod exit codes (per D-05, issue #577 workaround).
	if stage == "finished" {
		exitState, exitErr := checkRunnerExitCodes(ctx, client, ns, name)
		if exitErr != nil {
			slog.Warn("failed to check runner exit codes, treating as error",
				"name", name,
				"namespace", ns,
				"error", exitErr,
				"provider", p.Name(),
			)
			return &provider.RunResult{
				State: provider.Errored,
			}, nil
		}
		state = exitState
	}

	result := &provider.RunResult{
		State:            state,
		ThresholdsPassed: state == provider.Passed,
	}

	// Populate detailed metrics from handleSummary for terminal states (METR-01).
	// Mirrors cloud.go populateAggregateMetrics pattern: failures log Warn, return zero metrics.
	//
	// Only Passed and Failed states trigger summary parsing. Other terminal states
	// (Errored, Aborted) skip this because:
	// - Errored: k6 crashed or infra issue, handleSummary may not have run
	// - Aborted: run was stopped, no complete metrics available
	//
	// Threshold pass/fail is determined by exit codes (Phase 8), NOT by handleSummary
	// threshold data. handleSummary provides supplementary detailed metrics only.
	// Graceful degradation per D-05: failures log Warn with structured fields,
	// result retains zero-value metrics (distinguishable from real zeros only via
	// the slog warning -- acceptable because zero http_reqs means "no data").
	if state == provider.Passed || state == provider.Failed {
		logReader := p.ensureLogReader(client)
		metrics, err := parseSummaryFromPods(ctx, logReader, client, ns, name)
		if err != nil {
			slog.Warn("failed to parse handleSummary from pod logs, detailed metrics unavailable",
				"name", name,
				"namespace", ns,
				"error", err,
				"provider", p.Name(),
				"affectedMetrics", "http_req_failed,http_req_duration,http_reqs",
			)
			// result keeps zero-value metrics -- graceful degradation per D-05
		} else {
			result.HTTPReqFailed = metrics.HTTPReqFailed
			result.HTTPReqDuration = metrics.HTTPReqDuration
			result.HTTPReqs = metrics.HTTPReqs
		}
	}

	return result, nil
}

// StopRun deletes the TestRun or PrivateLoadZone CR via dynamic client (per D-08).
//
// Decodes runID to recover namespace, GVR, and CR name (addresses HIGH review concern
// about lifecycle identity). Treats NotFound as success for idempotent abort paths
// (addresses HIGH review concern about idempotent delete).
//
// Delegates to the shared deleteCR helper with source="stop" to distinguish this
// cancel-path delete from the success-path Cleanup delete in controller logs.
func (p *K6OperatorProvider) StopRun(ctx context.Context, _ *provider.PluginConfig, runID string) error {
	return p.deleteCR(ctx, runID, "stop")
}

// Cleanup releases the TestRun or PrivateLoadZone CR for a run that has reached
// a terminal state. Called by K6MetricProvider.GarbageCollect (and the step
// plugin's terminal-state hook in Phase 11-02) for each runID stored in
// measurement metadata.
//
// Idempotent: NotFound is treated as success (the CR may already have been
// reaped by k6-operator, by an earlier Cleanup pass, or by StopRun on a
// cancelled run). Delegates to the shared deleteCR helper with source="cleanup"
// to distinguish this delete from StopRun deletes in controller logs.
//
// Semantically distinct from StopRun ("cancel an in-flight run"); see Provider
// interface Godoc for the rationale behind the two-method split.
func (p *K6OperatorProvider) Cleanup(ctx context.Context, _ *provider.PluginConfig, runID string) error {
	return p.deleteCR(ctx, runID, "cleanup")
}

// deleteCR deletes a TestRun or PrivateLoadZone CR via the dynamic client.
// Treats k8serrors.IsNotFound as success (idempotent). Shared between StopRun
// (cancel path, source="stop") and Cleanup (success-path GC, source="cleanup").
//
// The source field is a structured log key that lets operators grep controller
// logs for cancel-vs-cleanup deletes without recovering the call site from the
// stack trace -- useful when diagnosing runaway GC or missed cancels.
func (p *K6OperatorProvider) deleteCR(ctx context.Context, runID, source string) error {
	// Decode lifecycle identity from runID (addresses HIGH review concern).
	ns, resource, name, err := decodeRunID(runID)
	if err != nil {
		return fmt.Errorf("invalid run ID: %w", err)
	}
	gvr := gvrForResource(resource)

	_, dynClient, err := p.ensureClient()
	if err != nil {
		return err
	}

	// Delete the CR (per D-08).
	// NotFound treated as success for idempotent abort and cleanup paths.
	err = dynClient.Resource(gvr).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			slog.Info("TestRun already deleted (idempotent)",
				"name", name,
				"namespace", ns,
				"resource", resource,
				"source", source,
				"provider", p.Name(),
			)
			return nil
		}
		return fmt.Errorf("delete %s %s/%s: %w", resource, ns, name, err)
	}

	slog.Info("k6 TestRun deleted",
		"name", name,
		"namespace", ns,
		"resource", resource,
		"source", source,
		"provider", p.Name(),
	)
	return nil
}
