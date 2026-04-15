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

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/providertest"
)

const metricHTTPReqDuration = "http_req_duration"

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

func TestGarbageCollect_ReturnsNilRpcError(t *testing.T) {
	k := New(&mockProvider{})
	rpcErr := k.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.Empty(t, rpcErr.Error())
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
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
		Metadata: map[string]string{"runId": "run-1"},
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
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
	assert.Equal(t, "run-1", m.Metadata["runId"])
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
			assert.Equal(t, "run-1", runID)
			return nil
		},
	}
	k := New(mock)
	m := k.Terminate(nil, testMetric(defaultCfg()), runningMeasurement("run-1"))
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
	m := k.Resume(nil, testMetric(cfg), runningMeasurement("run-1"))
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
