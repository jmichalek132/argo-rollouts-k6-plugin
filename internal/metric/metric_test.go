package metric

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/providertest"
)

const (
	metricHTTPReqDuration = "http_req_duration"
	testRunID             = "run-1"
)

// mockProvider is a type alias for the shared mock, keeping test callsites concise.
type mockProvider = providertest.MockProvider

// --- Helpers ---

func testMetric(cfg map[string]interface{}) v1alpha1.Metric {
	rawCfg, _ := json.Marshal(cfg)
	return v1alpha1.Metric{
		Name: "k6-test",
		Provider: v1alpha1.MetricProvider{
			Plugin: map[string]json.RawMessage{
				pluginName: rawCfg,
			},
		},
	}
}

func defaultCfg() map[string]interface{} {
	return map[string]interface{}{
		"testId":   "42",
		"apiToken": "test-token",
		"stackId":  "12345",
		"metric":   "thresholds",
	}
}

func runningMeasurement(runID string) v1alpha1.Measurement {
	return v1alpha1.Measurement{
		Phase:    v1alpha1.AnalysisPhaseRunning,
		Metadata: map[string]string{"runId": runID},
	}
}

// testAR builds a minimal *v1alpha1.AnalysisRun fixture. When rolloutName != "",
// an OwnerReference is attached with Kind=Rollout and Controller=true, mirroring
// what the Argo Rollouts controller stamps via NewControllerRef (verified in
// rollout/analysis.go:502). Pass rolloutName=="" to simulate a standalone AR.
func testAR(name, ns, uid, rolloutName string) *v1alpha1.AnalysisRun {
	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID(uid),
		},
	}
	if rolloutName != "" {
		isController := true
		ar.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Rollout",
			Name:       rolloutName,
			Controller: &isController,
		}}
	}
	return ar
}

// --- InitPlugin tests ---

func TestInitPlugin_ReturnsNilRpcError(t *testing.T) {
	k := New(&mockProvider{})
	rpcErr := k.InitPlugin()
	assert.Empty(t, rpcErr.Error())
}

// --- Type tests ---

func TestType_ReturnsPluginName(t *testing.T) {
	k := New(&mockProvider{})
	assert.Equal(t, "jmichalek132/k6", k.Type())
}

// --- GetMetadata tests ---

func TestGetMetadata_ReturnsEmptyMap(t *testing.T) {
	k := New(&mockProvider{})
	meta := k.GetMetadata(v1alpha1.Metric{})
	assert.NotNil(t, meta)
	assert.Empty(t, meta)
}

// --- GarbageCollect tests ---

// arWithMeasurements builds an AnalysisRun with MetricResults populated so
// GarbageCollect has runIDs to walk. Keeping this helper local (not in the
// top-level helpers block) because only GC tests need this shape.
func arWithMeasurements(metricName string, measurements ...v1alpha1.Measurement) *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-gc",
			Namespace: "ns-gc",
			UID:       types.UID("ar-uid-gc"),
		},
		Status: v1alpha1.AnalysisRunStatus{
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:         metricName,
					Measurements: measurements,
				},
			},
		},
	}
}

func TestGarbageCollect_CallsCleanupForEachMeasurementRunId(t *testing.T) {
	// AR with two measurements for the matching metric -- Cleanup must be called
	// once per non-empty runId, in order.
	var cleanupCalls []string
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, runID string) error {
			cleanupCalls = append(cleanupCalls, runID)
			return nil
		},
	}

	ar := arWithMeasurements("k6-test",
		v1alpha1.Measurement{Metadata: map[string]string{"runId": "ns-a/testruns/run-1"}},
		v1alpha1.Measurement{Metadata: map[string]string{"runId": "ns-a/testruns/run-2"}},
	)

	k := New(mock)
	// k6-operator cfg so parseConfig passes without apiToken/stackId/testId.
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"metric":   "thresholds",
		"configMapRef": map[string]interface{}{
			"name": "k6-scripts",
			"key":  "test.js",
		},
	}
	rpcErr := k.GarbageCollect(ar, testMetric(cfg), 0)

	assert.Empty(t, rpcErr.Error(), "GC must return empty RpcError")
	require.Equal(t, []string{"ns-a/testruns/run-1", "ns-a/testruns/run-2"}, cleanupCalls,
		"Cleanup must be called once per measurement runId, in order")
}

// TestGarbageCollect_CloudProviderSeesCleanupCalls proves the metric layer is
// provider-agnostic when dispatching Cleanup -- the no-op-ness of grafana-cloud
// is a provider concern, not a metric-layer concern. The cloud package's
// TestCleanup_IsNoOp proves the other half of the contract.
func TestGarbageCollect_CloudProviderSeesCleanupCalls(t *testing.T) {
	cleanupCount := 0
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			cleanupCount++
			return nil
		},
	}

	ar := arWithMeasurements("k6-test",
		v1alpha1.Measurement{Metadata: map[string]string{"runId": "12345"}},
	)

	k := New(mock)
	// grafana-cloud (default provider) cfg.
	rpcErr := k.GarbageCollect(ar, testMetric(defaultCfg()), 0)

	assert.Empty(t, rpcErr.Error())
	assert.Equal(t, 1, cleanupCount,
		"GC dispatches Cleanup regardless of provider; no-op-ness lives in the provider itself")
}

func TestGarbageCollect_LogsWarnOnCleanupErrorAndReturnsNilRpcError(t *testing.T) {
	// GC-03: Cleanup errors are logged at Warn but NEVER surface as RpcError.
	// We assert via a call counter (the fact that Cleanup was called) plus the
	// empty RpcError -- the slog.Warn itself is not asserted (slog tests are
	// fragile and the contract is the returned RpcError, not log output).
	cleanupCount := 0
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			cleanupCount++
			return fmt.Errorf("api unavailable")
		},
	}

	ar := arWithMeasurements("k6-test",
		v1alpha1.Measurement{Metadata: map[string]string{"runId": "ns-a/testruns/run-err"}},
	)

	k := New(mock)
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"metric":   "thresholds",
		"configMapRef": map[string]interface{}{
			"name": "k6-scripts",
			"key":  "test.js",
		},
	}
	rpcErr := k.GarbageCollect(ar, testMetric(cfg), 0)

	assert.Empty(t, rpcErr.Error(), "GC-03: Cleanup errors must NEVER surface as RpcError")
	assert.Equal(t, 1, cleanupCount, "Cleanup must still be invoked for each runId")
}

func TestGarbageCollect_NilAnalysisRun(t *testing.T) {
	cleanupCount := 0
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			cleanupCount++
			return nil
		},
	}
	k := New(mock)

	rpcErr := k.GarbageCollect(nil, testMetric(defaultCfg()), 0)
	assert.Empty(t, rpcErr.Error(), "nil AR must return empty RpcError")
	assert.Equal(t, 0, cleanupCount, "nil AR must NOT trigger Cleanup calls")
}

func TestGarbageCollect_EmptyMetricResults(t *testing.T) {
	cleanupCount := 0
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			cleanupCount++
			return nil
		},
	}

	// AR with no MetricResults at all (empty Status).
	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-empty",
			Namespace: "ns-empty",
		},
	}

	k := New(mock)
	rpcErr := k.GarbageCollect(ar, testMetric(defaultCfg()), 0)
	assert.Empty(t, rpcErr.Error())
	assert.Equal(t, 0, cleanupCount, "empty MetricResults must NOT trigger Cleanup")
}

func TestGarbageCollect_MeasurementWithoutRunId(t *testing.T) {
	// Measurements with empty or missing runId must be skipped silently -- they
	// correspond to measurements that errored before runId assignment (e.g.
	// parseConfig failure in Run) and have no backing CR to clean up.
	cleanupCount := 0
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			cleanupCount++
			return nil
		},
	}

	ar := arWithMeasurements("k6-test",
		v1alpha1.Measurement{Metadata: map[string]string{}},                // no runId key
		v1alpha1.Measurement{Metadata: map[string]string{"runId": ""}},     // empty runId
		v1alpha1.Measurement{Metadata: map[string]string{"other": "junk"}}, // wrong key
	)

	k := New(mock)
	rpcErr := k.GarbageCollect(ar, testMetric(defaultCfg()), 0)
	assert.Empty(t, rpcErr.Error())
	assert.Equal(t, 0, cleanupCount, "empty runIds must be skipped")
}

// TestGarbageCollect_SkipsMeasurementsForOtherMetricNames guards a subtle
// regression: GarbageCollect must only walk the MetricResult matching
// metric.Name. Cleaning up runIds belonging to a different metric (possibly a
// different provider entirely) would cause cross-metric resource deletion.
func TestGarbageCollect_SkipsMeasurementsForOtherMetricNames(t *testing.T) {
	var cleanupCalls []string
	mock := &mockProvider{
		CleanupFn: func(_ context.Context, _ *provider.PluginConfig, runID string) error {
			cleanupCalls = append(cleanupCalls, runID)
			return nil
		},
	}

	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-multi",
			Namespace: "ns-multi",
		},
		Status: v1alpha1.AnalysisRunStatus{
			MetricResults: []v1alpha1.MetricResult{
				{
					Name: "other-metric", // different metric -- MUST be skipped
					Measurements: []v1alpha1.Measurement{
						{Metadata: map[string]string{"runId": "should-not-be-cleaned"}},
					},
				},
				{
					Name: "k6-test", // matches metric.Name -- MUST be walked
					Measurements: []v1alpha1.Measurement{
						{Metadata: map[string]string{"runId": "ns-multi/testruns/run-ours"}},
					},
				},
			},
		},
	}

	k := New(mock)
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"metric":   "thresholds",
		"configMapRef": map[string]interface{}{
			"name": "k6-scripts",
			"key":  "test.js",
		},
	}
	rpcErr := k.GarbageCollect(ar, testMetric(cfg), 0)

	assert.Empty(t, rpcErr.Error())
	require.Equal(t, []string{"ns-multi/testruns/run-ours"}, cleanupCalls,
		"GC must only clean up runIds under the matching metric.Name")
}

// --- Run tests ---

func TestRun_WithTestIdTriggersProvider(t *testing.T) {
	triggered := false
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			triggered = true
			assert.Equal(t, "42", cfg.TestID)
			return "run-999", nil
		},
	}
	k := New(mock)
	m := k.Run(nil, testMetric(defaultCfg()))
	assert.True(t, triggered, "TriggerRun should be called")
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	assert.Equal(t, "run-999", m.Metadata["runId"])
	assert.NotNil(t, m.StartedAt)
}

func TestRun_WithTestRunIdSkipsTrigger(t *testing.T) {
	triggered := false
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			triggered = true
			return "", nil
		},
	}
	cfg := defaultCfg()
	cfg["testRunId"] = "existing-run-456"
	k := New(mock)
	m := k.Run(nil, testMetric(cfg))
	assert.False(t, triggered, "TriggerRun should NOT be called in poll-only mode")
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	assert.Equal(t, "existing-run-456", m.Metadata["runId"])
}

func TestRun_MissingConfigFields_ReturnsPhaseError(t *testing.T) {
	k := New(&mockProvider{})
	// Empty config -- no required fields
	m := k.Run(nil, testMetric(map[string]interface{}{}))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.NotEmpty(t, m.Message)
}

func TestRun_ProviderTriggerRunError(t *testing.T) {
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			return "", fmt.Errorf("api error: 500")
		},
	}
	k := New(mock)
	m := k.Run(nil, testMetric(defaultCfg()))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "api error")
}

// --- Run tests: AnalysisRun metadata plumbing (Phase 08.1 D-08) ---

func TestRun_PopulatesCfgFromAnalysisRun(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			gotCfg = cfg
			return testRunID, nil
		},
	}
	ar := testAR("ar-1", "ns-prod", "ar-uid-1", "my-app")
	k := New(mock)
	m := k.Run(ar, testMetric(defaultCfg()))

	require.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	require.NotNil(t, gotCfg, "TriggerRun should have been called")
	assert.Equal(t, "ns-prod", gotCfg.Namespace, "cfg.Namespace should be populated from ar.Namespace (D-01)")
	assert.Equal(t, "my-app", gotCfg.RolloutName, "cfg.RolloutName should be walked from OwnerReferences (D-03)")
	assert.Equal(t, "ar-1", gotCfg.AnalysisRunName)
	assert.Equal(t, "ar-uid-1", gotCfg.AnalysisRunUID)
}

func TestRun_StandaloneAnalysisRun(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			gotCfg = cfg
			return testRunID, nil
		},
	}
	ar := testAR("ar-solo", "ns-prod", "ar-uid-solo", "") // no Rollout owner ref
	k := New(mock)
	m := k.Run(ar, testMetric(defaultCfg()))

	require.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	require.NotNil(t, gotCfg)
	assert.Equal(t, "", gotCfg.RolloutName, "standalone AR must leave RolloutName empty (D-04)")
	assert.Equal(t, "ns-prod", gotCfg.Namespace)
	assert.Equal(t, "ar-solo", gotCfg.AnalysisRunName)
	assert.Equal(t, "ar-uid-solo", gotCfg.AnalysisRunUID)
}

func TestRun_UserNamespaceWinsOverAR(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			gotCfg = cfg
			return testRunID, nil
		},
	}
	// User specifies namespace in the plugin config; AR lives in a different namespace.
	cfg := defaultCfg()
	cfg["namespace"] = "user-override"
	ar := testAR("ar-1", "ns-ar", "ar-uid-1", "my-app")
	k := New(mock)
	m := k.Run(ar, testMetric(cfg))

	require.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	require.NotNil(t, gotCfg)
	assert.Equal(t, "user-override", gotCfg.Namespace, "user-supplied namespace must win over ar.Namespace (D-01)")
	assert.Equal(t, "my-app", gotCfg.RolloutName, "RolloutName still populated even when user sets namespace")
}

// R3 (codex review): D-03 says "prefer the entry with Controller==true". When an AR
// carries multiple Rollout owner refs -- e.g. a non-controller ref followed by the
// controller ref -- the walk must select the controller entry, not the first match.
func TestRun_PrefersControllerRolloutOwnerRef(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			gotCfg = cfg
			return testRunID, nil
		},
	}

	// Build an AR with TWO Rollout owner refs; the first has Controller=nil (not
	// the controller), the second has Controller=&true (the real controller ref).
	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-multi",
			Namespace: "ns-prod",
			UID:       types.UID("ar-uid-multi"),
		},
	}
	isController := true
	ar.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Rollout",
			Name:       "not-controller",
			UID:        types.UID("uid-wrong"),
			Controller: nil, // NOT the controller -- must NOT be picked
		},
		{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Rollout",
			Name:       "my-app",
			UID:        types.UID("uid-right"),
			Controller: &isController, // controller -- MUST be picked
		},
	}

	k := New(mock)
	m := k.Run(ar, testMetric(defaultCfg()))

	require.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	require.NotNil(t, gotCfg)
	assert.Equal(t, "my-app", gotCfg.RolloutName,
		"D-03: walk must pick the Rollout owner ref with Controller==true, not the first match")
	assert.NotEqual(t, "not-controller", gotCfg.RolloutName,
		"non-controller Rollout ref must not hijack RolloutName")
}

// --- Resume tests: AnalysisRun metadata plumbing (Phase 08.1 D-08, R4) ---

// R4 (codex review): Plan 02 modifies Run, Resume, and Terminate but the baseline
// tests only exercise Run. This test proves populateFromAnalysisRun is wired into
// Resume by asserting the GetRunResultFn receives a populated *PluginConfig.
func TestResume_ExtractsAnalysisRunMetadata(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, cfg *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			gotCfg = cfg
			return &provider.RunResult{State: provider.Running}, nil
		},
	}

	// Resume requires a Measurement that already carries the runId metadata
	// (normally stamped by Run). Mirror the canonical runningMeasurement shape.
	measurement := v1alpha1.Measurement{
		Phase: v1alpha1.AnalysisPhaseRunning,
		Metadata: map[string]string{
			"runId": testRunID,
		},
	}

	ar := testAR("ar-res", "ns-res", "ar-uid-res", "my-app")
	k := New(mock)
	_ = k.Resume(ar, testMetric(defaultCfg()), measurement)

	require.NotNil(t, gotCfg, "GetRunResult should have been called during Resume")
	assert.Equal(t, "ns-res", gotCfg.Namespace,
		"Resume must populate cfg.Namespace from ar.Namespace (D-01)")
	assert.Equal(t, "my-app", gotCfg.RolloutName,
		"Resume must populate cfg.RolloutName from the controller owner ref (D-03)")
	assert.Equal(t, "ar-res", gotCfg.AnalysisRunName,
		"Resume must populate cfg.AnalysisRunName from ar.Name")
	assert.Equal(t, "ar-uid-res", gotCfg.AnalysisRunUID,
		"Resume must populate cfg.AnalysisRunUID from ar.UID")
}

// --- Resume tests: thresholds metric ---

func TestResume_ThresholdsPassed(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, runID string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:            provider.Passed,
				TestRunURL:       "https://app.k6.io/runs/" + runID,
				ThresholdsPassed: true,
			}, nil
		},
	}
	k := New(mock)
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, m.Phase)
	assert.Equal(t, "1", m.Value)
	assert.NotNil(t, m.FinishedAt)
}

func TestResume_ThresholdsFailed(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, runID string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:            provider.Failed,
				TestRunURL:       "https://app.k6.io/runs/" + runID,
				ThresholdsPassed: false,
			}, nil
		},
	}
	k := New(mock)
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, m.Phase)
	assert.Equal(t, "0", m.Value)
	assert.NotNil(t, m.FinishedAt)
}

// --- Resume tests: http_req_failed metric ---

func TestResume_HTTPReqFailed(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:         provider.Passed,
				TestRunURL:    "https://app.k6.io/runs/1",
				HTTPReqFailed: 0.023,
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = "http_req_failed"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, m.Phase)
	assert.Equal(t, "0.023", m.Value)
}

// --- Resume tests: http_req_duration metric ---

func TestResume_HTTPReqDurationP50(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/1",
				HTTPReqDuration: provider.Percentiles{
					P50: 150.0,
					P95: 234.5,
					P99: 450.0,
				},
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = metricHTTPReqDuration
	cfg["aggregation"] = "p50"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, m.Phase)
	assert.Equal(t, "150", m.Value)
}

func TestResume_HTTPReqDurationP95(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/1",
				HTTPReqDuration: provider.Percentiles{
					P50: 150.0,
					P95: 234.5,
					P99: 450.0,
				},
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = metricHTTPReqDuration
	cfg["aggregation"] = "p95"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, m.Phase)
	assert.Equal(t, "234.5", m.Value)
}

func TestResume_HTTPReqDurationP99(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/1",
				HTTPReqDuration: provider.Percentiles{
					P50: 150.0,
					P95: 234.5,
					P99: 450.0,
				},
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = metricHTTPReqDuration
	cfg["aggregation"] = "p99"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, m.Phase)
	assert.Equal(t, "450", m.Value)
}

func TestResume_HTTPReqDuration_MissingAggregation(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/1",
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = metricHTTPReqDuration
	// no aggregation
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "aggregation")
}

func TestResume_HTTPReqDuration_InvalidAggregation(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/1",
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = metricHTTPReqDuration
	cfg["aggregation"] = "p100"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "unsupported aggregation")
}

// --- Resume tests: http_reqs metric ---

func TestResume_HTTPReqs(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/1",
				HTTPReqs:   142.3,
			}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = "http_reqs"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, m.Phase)
	assert.Equal(t, "142.3", m.Value)
}

func TestResume_NilMetadata(t *testing.T) {
	// If the controller passes a measurement with nil Metadata (shouldn't happen in
	// practice, but the interface contract doesn't prevent it), Resume must not panic
	// and must initialise the map before writing to it.
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:            provider.Passed,
				ThresholdsPassed: true,
				TestRunURL:       "https://app.k6.io/runs/1",
			}, nil
		},
	}
	// Seed a measurement that has nil Metadata but a known runId stored elsewhere.
	// We simulate the controller passing a pre-populated runId via an explicit map init.
	measurement := v1alpha1.Measurement{
		Phase:    v1alpha1.AnalysisPhaseRunning,
		Metadata: map[string]string{"runId": testRunID},
	}
	measurement.Metadata = nil // drop it to trigger the nil guard

	k := New(mock)
	// With nil Metadata the runId lookup returns "" → early error return (no panic).
	m := k.Resume(nil, testMetric(defaultCfg()), measurement)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "runId not found")
}

// --- Resume tests: state mapping ---

func TestResume_RunningState(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/1",
			}, nil
		},
	}
	k := New(mock)
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, m.Phase)
	assert.Nil(t, m.FinishedAt)
}

func TestResume_ErroredState(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Errored,
				TestRunURL: "https://app.k6.io/runs/1",
			}, nil
		},
	}
	k := New(mock)
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.NotEmpty(t, m.Message)
	assert.NotNil(t, m.FinishedAt)
}

func TestResume_AbortedState(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Aborted,
				TestRunURL: "https://app.k6.io/runs/1",
			}, nil
		},
	}
	k := New(mock)
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.NotEmpty(t, m.Message)
	assert.NotNil(t, m.FinishedAt)
}

// --- Resume tests: metadata ---

func TestResume_AlwaysSetsMetadata(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:            provider.Passed,
				TestRunURL:       "https://app.k6.io/runs/run-1",
				ThresholdsPassed: true,
			}, nil
		},
	}
	k := New(mock)
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.Equal(t, testRunID, m.Metadata["runId"])
	assert.Equal(t, "https://app.k6.io/runs/run-1", m.Metadata["testRunURL"])
	assert.Equal(t, "Passed", m.Metadata["runState"])
	assert.Equal(t, "1", m.Metadata["metricValue"])
}

func TestResume_MissingRunIdInMetadata(t *testing.T) {
	k := New(&mockProvider{})
	m := v1alpha1.Measurement{
		Phase:    v1alpha1.AnalysisPhaseRunning,
		Metadata: map[string]string{},
	}
	result := k.Resume(nil, testMetric(defaultCfg()), m)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, result.Phase)
	assert.Contains(t, result.Message, "runId")
}

// --- Terminate tests ---

func TestTerminate_WithRunIdCallsStopRun(t *testing.T) {
	stopped := false
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, runID string) error {
			stopped = true
			assert.Equal(t, testRunID, runID)
			return nil
		},
	}
	k := New(mock)
	m := k.Terminate(nil, testMetric(defaultCfg()), runningMeasurement(testRunID))
	assert.True(t, stopped, "StopRun should be called")
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "terminated")
	assert.NotNil(t, m.FinishedAt)
}

func TestTerminate_WithoutRunIdNoStopRun(t *testing.T) {
	stopped := false
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			stopped = true
			return nil
		},
	}
	k := New(mock)
	m := v1alpha1.Measurement{
		Phase:    v1alpha1.AnalysisPhaseRunning,
		Metadata: map[string]string{},
	}
	result := k.Terminate(nil, testMetric(defaultCfg()), m)
	assert.False(t, stopped, "StopRun should NOT be called without runId")
	assert.Equal(t, v1alpha1.AnalysisPhaseError, result.Phase)
}

// --- Config parsing tests ---

func TestConfig_InvalidMetricType(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{State: provider.Passed, TestRunURL: "http://x"}, nil
		},
	}
	cfg := defaultCfg()
	cfg["metric"] = "invalid_metric"
	k := New(mock)
	m := k.Resume(nil, testMetric(cfg), runningMeasurement(testRunID))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "unsupported metric type")
}

func TestConfig_MissingAPIToken(t *testing.T) {
	k := New(&mockProvider{})
	cfg := map[string]interface{}{
		"testId":  "42",
		"stackId": "12345",
		"metric":  "thresholds",
	}
	m := k.Run(nil, testMetric(cfg))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "apiToken")
}

func TestConfig_MissingStackId(t *testing.T) {
	k := New(&mockProvider{})
	cfg := map[string]interface{}{
		"testId":   "42",
		"apiToken": "test-token",
		"metric":   "thresholds",
	}
	m := k.Run(nil, testMetric(cfg))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "stackId")
}

func TestConfig_MissingMetric(t *testing.T) {
	k := New(&mockProvider{})
	cfg := map[string]interface{}{
		"testId":   "42",
		"apiToken": "test-token",
		"stackId":  "12345",
	}
	m := k.Run(nil, testMetric(cfg))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "metric")
}

func TestConfig_NeitherTestIdNorTestRunId(t *testing.T) {
	k := New(&mockProvider{})
	cfg := map[string]interface{}{
		"apiToken": "test-token",
		"stackId":  "12345",
		"metric":   "thresholds",
	}
	m := k.Run(nil, testMetric(cfg))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "testId")
}

// --- k6-operator config validation tests ---

func TestConfig_K6OperatorValidConfig(t *testing.T) {
	// k6-operator config without apiToken/stackId/testId must pass parseConfig.
	// Addresses HIGH review concern: config validation trap.
	k := New(&mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			return "", fmt.Errorf("k6-operator provider not yet implemented")
		},
	})
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"metric":   "thresholds",
		"configMapRef": map[string]interface{}{
			"name": "k6-scripts",
			"key":  "test.js",
		},
		"namespace": "test-ns",
	}
	m := k.Run(nil, testMetric(cfg))
	// Should NOT fail on missing apiToken/stackId. The error should come from the
	// provider stub ("not yet implemented"), not from config validation.
	assert.NotContains(t, m.Message, "apiToken")
	assert.NotContains(t, m.Message, "stackId")
	assert.NotContains(t, m.Message, "testId")
}

func TestConfig_K6OperatorMissingConfigMapRef(t *testing.T) {
	k := New(&mockProvider{})
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"metric":   "thresholds",
	}
	m := k.Run(nil, testMetric(cfg))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "configMapRef")
}

func TestConfig_K6OperatorMissingMetric(t *testing.T) {
	// metric field is still required for k6-operator in metric plugin.
	k := New(&mockProvider{})
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"configMapRef": map[string]interface{}{
			"name": "k6-scripts",
			"key":  "test.js",
		},
	}
	m := k.Run(nil, testMetric(cfg))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, m.Phase)
	assert.Contains(t, m.Message, "metric")
}

// --- Concurrent safety test ---

func TestConcurrentSafety(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, runID string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:            provider.Passed,
				TestRunURL:       "https://app.k6.io/runs/" + runID,
				ThresholdsPassed: true,
			}, nil
		},
	}
	k := New(mock)

	var wg sync.WaitGroup
	results := make([]v1alpha1.Measurement, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			runID := fmt.Sprintf("run-%d", idx)
			m := runningMeasurement(runID)
			results[idx] = k.Resume(nil, testMetric(defaultCfg()), m)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 10; i++ {
		require.Equal(t, v1alpha1.AnalysisPhaseSuccessful, results[i].Phase, "goroutine %d", i)
		expectedRunID := fmt.Sprintf("run-%d", i)
		assert.Equal(t, expectedRunID, results[i].Metadata["runId"], "goroutine %d got wrong runId", i)
	}
}
