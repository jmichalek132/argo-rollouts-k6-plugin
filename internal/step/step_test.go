package step

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/providertest"
)

// mockProvider is a type alias for the shared mock, keeping test callsites concise.
type mockProvider = providertest.MockProvider

// --- Helpers ---

func makeConfig(overrides map[string]interface{}) json.RawMessage {
	cfg := map[string]interface{}{
		"testId":   "42",
		"apiToken": "test-token",
		"stackId":  "12345",
	}
	for k, v := range overrides {
		cfg[k] = v
	}
	raw, _ := json.Marshal(cfg)
	return raw
}

func makeState(runID, triggeredAt string) json.RawMessage {
	s := stepState{
		RunID:       runID,
		TriggeredAt: triggeredAt,
	}
	raw, _ := json.Marshal(s)
	return raw
}

func makeContext(config, status json.RawMessage) *types.RpcStepContext {
	return &types.RpcStepContext{
		PluginName: "jmichalek132/k6-step",
		Config:     config,
		Status:     status,
	}
}

// testRollout builds a minimal *v1alpha1.Rollout fixture for D-08 tests.
// The step plugin populates cfg.Namespace/RolloutName/RolloutUID directly from this
// object -- no owner-ref walk is needed (D-06: Rollout IS the parent).
func testRollout(name, ns, uid string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       k8stypes.UID(uid),
		},
	}
}

// --- Interface compliance test (PLUG-02) ---

func TestInterface(t *testing.T) {
	// This compiles only if K6StepPlugin implements rpc.StepPlugin
	// The compile-time check is in step.go; this test simply instantiates it.
	p := New(&mockProvider{})
	require.NotNil(t, p)
}

// --- InitPlugin test ---

func TestInitPlugin(t *testing.T) {
	p := New(&mockProvider{})
	rpcErr := p.InitPlugin()
	assert.False(t, rpcErr.HasError())
	assert.Empty(t, rpcErr.ErrorString)
}

// --- Type test ---

func TestType(t *testing.T) {
	p := New(&mockProvider{})
	assert.Equal(t, "jmichalek132/k6-step", p.Type())
}

// --- Config parsing tests (STEP-01) ---

func TestParseConfig_Valid(t *testing.T) {
	ctx := makeContext(makeConfig(nil), nil)
	p := New(&mockProvider{})
	result, rpcErr := p.Run(nil, ctx)
	// Should not return a config error
	assert.False(t, rpcErr.HasError(), "infrastructure error should not occur for valid config")
	assert.NotEqual(t, types.PhaseFailed, result.Phase, "valid config should not produce PhaseFailed")
}

func TestParseConfig_MissingTestId(t *testing.T) {
	raw := map[string]interface{}{
		"apiToken": "test-token",
		"stackId":  "12345",
	}
	cfgBytes, _ := json.Marshal(raw)
	ctx := makeContext(cfgBytes, nil)
	p := New(&mockProvider{})
	result, rpcErr := p.Run(nil, ctx)
	assert.False(t, rpcErr.HasError(), "validation errors should be PhaseFailed, not RpcError")
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "testId")
}

func TestParseConfig_MissingAPIToken(t *testing.T) {
	raw := map[string]interface{}{
		"testId":  "42",
		"stackId": "12345",
	}
	cfgBytes, _ := json.Marshal(raw)
	ctx := makeContext(cfgBytes, nil)
	p := New(&mockProvider{})
	result, rpcErr := p.Run(nil, ctx)
	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "apiToken")
}

func TestParseConfig_MissingStackId(t *testing.T) {
	raw := map[string]interface{}{
		"testId":   "42",
		"apiToken": "test-token",
	}
	cfgBytes, _ := json.Marshal(raw)
	ctx := makeContext(cfgBytes, nil)
	p := New(&mockProvider{})
	result, rpcErr := p.Run(nil, ctx)
	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "stackId")
}

// --- Run: first call trigger (STEP-02, STEP-03) ---

func TestRun_FirstCall_Trigger(t *testing.T) {
	triggered := false
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			triggered = true
			assert.Equal(t, "42", cfg.TestID)
			return "run-999", nil
		},
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/run-999",
			}, nil
		},
	}
	p := New(mock)
	ctx := makeContext(makeConfig(nil), nil) // nil Status = first call
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.True(t, triggered, "TriggerRun should be called on first Run with testId")
	assert.Equal(t, types.PhaseRunning, result.Phase)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	// Verify Status contains runId and triggeredAt
	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "run-999", state.RunID)
	assert.NotEmpty(t, state.TriggeredAt)
	assert.Equal(t, "https://app.k6.io/runs/run-999", state.TestRunURL)
}

// --- Run tests: Rollout metadata plumbing (Phase 08.1 D-08) ---

func TestRun_PopulatesCfgFromRollout(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			gotCfg = cfg
			return "run-1", nil
		},
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	rollout := testRollout("my-app", "ns-prod", "rollout-uid-1")
	p := New(mock)
	ctx := makeContext(makeConfig(nil), nil)
	_, rpcErr := p.Run(rollout, ctx)

	require.False(t, rpcErr.HasError())
	require.NotNil(t, gotCfg, "TriggerRun should have been called")
	assert.Equal(t, "ns-prod", gotCfg.Namespace, "cfg.Namespace should be populated from rollout.Namespace (D-01)")
	assert.Equal(t, "my-app", gotCfg.RolloutName)
	assert.Equal(t, "rollout-uid-1", gotCfg.RolloutUID, "cfg.RolloutUID should be populated from rollout.UID (D-05)")
	// Step plugin has no AnalysisRun -- AR fields must stay empty (D-05).
	assert.Equal(t, "", gotCfg.AnalysisRunName, "step plugin must not set AnalysisRunName")
	assert.Equal(t, "", gotCfg.AnalysisRunUID, "step plugin must not set AnalysisRunUID")
}

func TestRun_UserNamespaceWinsOverRollout(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
			gotCfg = cfg
			return "run-1", nil
		},
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	// User specifies namespace in the plugin config; Rollout lives in a different namespace.
	cfgBytes := makeConfig(map[string]interface{}{
		"namespace": "user-override",
	})
	rollout := testRollout("my-app", "ns-rollout", "rollout-uid-1")
	p := New(mock)
	ctx := makeContext(cfgBytes, nil)
	_, rpcErr := p.Run(rollout, ctx)

	require.False(t, rpcErr.HasError())
	require.NotNil(t, gotCfg)
	assert.Equal(t, "user-override", gotCfg.Namespace, "user-supplied namespace must win over rollout.Namespace (D-01)")
	assert.Equal(t, "my-app", gotCfg.RolloutName, "RolloutName still populated even when user sets namespace")
	assert.Equal(t, "rollout-uid-1", gotCfg.RolloutUID)
}

// R6 (codex review): the MEDIUM verification gap was that Task 1 extends
// stopActiveRun's signature so Terminate/Abort propagate Rollout metadata into
// StopRun, but the baseline tests only exercised Run. This test proves that
// Terminate delivers a populated *PluginConfig into StopRun -- closing the
// verification gap for the stop path.
func TestTerminate_PassesPopulatedConfigToStopRun(t *testing.T) {
	var gotCfg *provider.PluginConfig
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, cfg *provider.PluginConfig, _ string) error {
			gotCfg = cfg
			return nil
		},
	}

	// stopActiveRun reads state.RunID from ctx.Status; build a Status blob carrying
	// a serialized stepState with RunID="run-1" so the call reaches StopRun.
	state := stepState{RunID: "run-1"}
	stateBytes, err := json.Marshal(state)
	require.NoError(t, err)

	rollout := testRollout("my-app", "ns-prod", "rollout-uid-1")
	p := New(mock)
	ctx := makeContext(makeConfig(nil), stateBytes)

	_, rpcErr := p.Terminate(rollout, ctx)

	require.False(t, rpcErr.HasError())
	require.NotNil(t, gotCfg, "StopRun should have been called during Terminate (state has RunID)")
	assert.Equal(t, "ns-prod", gotCfg.Namespace,
		"Terminate must propagate cfg.Namespace through stopActiveRun into StopRun (D-01)")
	assert.Equal(t, "my-app", gotCfg.RolloutName,
		"Terminate must propagate cfg.RolloutName through stopActiveRun into StopRun (D-06)")
	assert.Equal(t, "rollout-uid-1", gotCfg.RolloutUID,
		"Terminate must propagate cfg.RolloutUID through stopActiveRun into StopRun (D-05)")
}

// --- Run: first call poll-only (STEP-02) ---

func TestRun_FirstCall_PollOnly(t *testing.T) {
	triggered := false
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			triggered = true
			return "", nil
		},
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, runID string) (*provider.RunResult, error) {
			assert.Equal(t, "existing-run-456", runID)
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/existing-run-456",
			}, nil
		},
	}
	cfg := makeConfig(map[string]interface{}{
		"testRunId": "existing-run-456",
	})
	p := New(mock)
	ctx := makeContext(cfg, nil)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.False(t, triggered, "TriggerRun should NOT be called in poll-only mode")
	assert.Equal(t, types.PhaseRunning, result.Phase)

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "existing-run-456", state.RunID)
}

// --- Run: subsequent poll still running (STEP-02) ---

func TestRun_Poll_StillRunning(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, runID string) (*provider.RunResult, error) {
			assert.Equal(t, "run-123", runID)
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/run-123",
			}, nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-123", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseRunning, result.Phase)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "run-123", state.RunID)
}

// --- Run: terminal states (STEP-04) ---

func TestRun_Terminal_Passed(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Passed,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseSuccessful, result.Phase)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "Passed", state.FinalStatus)
}

func TestRun_Terminal_Failed(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Failed,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "Failed", state.FinalStatus)
}

func TestRun_Terminal_Errored(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Errored,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "errored")

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "Errored", state.FinalStatus)
}

func TestRun_Terminal_Aborted(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Aborted,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "aborted")

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "Aborted", state.FinalStatus)
}

// --- Run: timeout (D-01, D-02) ---

func TestRun_Timeout(t *testing.T) {
	stopped := false
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, runID string) error {
			stopped = true
			assert.Equal(t, "run-1", runID)
			return nil
		},
	}
	p := New(mock)
	// Set triggeredAt to 6 minutes ago, default timeout is 5m
	triggeredAt := time.Now().UTC().Add(-6 * time.Minute).Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.True(t, stopped, "StopRun should be called on timeout")
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "timed out")

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "TimedOut", state.FinalStatus)
}

func TestRun_TimeoutDefault(t *testing.T) {
	// Default timeout is 5m, so 4 minutes should NOT timeout
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/run-1",
			}, nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Add(-4 * time.Minute).Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseRunning, result.Phase, "4 minutes should not exceed 5m default timeout")
}

func TestRun_TimeoutMax(t *testing.T) {
	p := New(&mockProvider{})
	cfg := makeConfig(map[string]interface{}{
		"timeout": "3h",
	})
	ctx := makeContext(cfg, nil)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "2h")
}

func TestRun_TimeoutInvalid(t *testing.T) {
	p := New(&mockProvider{})
	cfg := makeConfig(map[string]interface{}{
		"timeout": "not-a-duration",
	})
	ctx := makeContext(cfg, nil)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.NotEmpty(t, result.Message)
}

func TestRun_TimeoutNegative(t *testing.T) {
	p := New(&mockProvider{})
	cfg := makeConfig(map[string]interface{}{
		"timeout": "-5m",
	})
	ctx := makeContext(cfg, nil)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "positive")
}

// --- Run: Status always contains runId (STEP-03) ---

func TestRun_StatusContainsRunId(t *testing.T) {
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			return "run-abc", nil
		},
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return &provider.RunResult{
				State:      provider.Running,
				TestRunURL: "https://app.k6.io/runs/run-abc",
			}, nil
		},
	}
	p := New(mock)
	ctx := makeContext(makeConfig(nil), nil)
	result, rpcErr := p.Run(nil, ctx)

	assert.False(t, rpcErr.HasError())
	require.NotNil(t, result.Status, "Status must not be nil")

	var state stepState
	require.NoError(t, json.Unmarshal(result.Status, &state))
	assert.Equal(t, "run-abc", state.RunID, "Status must always contain runId")
}

// --- Terminate tests (STEP-05, D-07) ---

func TestTerminate_StopsRun(t *testing.T) {
	stopped := false
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, runID string) error {
			stopped = true
			assert.Equal(t, "run-1", runID)
			return nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Terminate(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.True(t, stopped, "StopRun should be called on Terminate")
	// Terminate returns empty result (D-07)
	assert.Equal(t, types.RpcStepResult{}, result)
}

func TestTerminate_NoRunId(t *testing.T) {
	stopped := false
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			stopped = true
			return nil
		},
	}
	p := New(mock)
	ctx := makeContext(makeConfig(nil), nil) // nil Status
	result, rpcErr := p.Terminate(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.False(t, stopped, "StopRun should NOT be called without runId in Status")
	assert.Equal(t, types.RpcStepResult{}, result)
}

func TestTerminate_StopRunError(t *testing.T) {
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			return fmt.Errorf("api error: 500")
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Terminate(nil, ctx)

	assert.False(t, rpcErr.HasError(), "StopRun error should be swallowed (D-08)")
	assert.Equal(t, types.RpcStepResult{}, result, "should return empty result even on StopRun error")
}

// --- Abort tests (STEP-05, D-07) ---

func TestAbort_StopsRun(t *testing.T) {
	stopped := false
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, runID string) error {
			stopped = true
			assert.Equal(t, "run-1", runID)
			return nil
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Abort(nil, ctx)

	assert.False(t, rpcErr.HasError())
	assert.True(t, stopped, "StopRun should be called on Abort")
	assert.Equal(t, types.RpcStepResult{}, result)
}

func TestAbort_StopRunError(t *testing.T) {
	mock := &mockProvider{
		StopRunFn: func(_ context.Context, _ *provider.PluginConfig, _ string) error {
			return fmt.Errorf("api error: 500")
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	result, rpcErr := p.Abort(nil, ctx)

	assert.False(t, rpcErr.HasError(), "StopRun error should be swallowed (D-08)")
	assert.Equal(t, types.RpcStepResult{}, result)
}

// --- k6-operator config validation tests ---

func TestParseConfig_K6OperatorValid(t *testing.T) {
	// k6-operator config without apiToken/stackId/testId must pass parseConfig.
	// Addresses HIGH review concern: config validation trap.
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			return "", fmt.Errorf("k6-operator provider not yet implemented")
		},
	}
	cfg := map[string]interface{}{
		"provider": "k6-operator",
		"configMapRef": map[string]interface{}{
			"name": "k6-scripts",
			"key":  "test.js",
		},
		"namespace": "test-ns",
	}
	cfgBytes, _ := json.Marshal(cfg)
	ctx := makeContext(cfgBytes, nil)
	p := New(mock)
	_, rpcErr := p.Run(nil, ctx)
	// The error should come from the provider, not from config validation.
	// If rpcErr is set, it should NOT be about apiToken/stackId.
	if rpcErr.HasError() {
		assert.NotContains(t, rpcErr.ErrorString, "apiToken")
		assert.NotContains(t, rpcErr.ErrorString, "stackId")
		assert.NotContains(t, rpcErr.ErrorString, "testId")
	}
}

func TestParseConfig_K6OperatorMissingConfigMapRef(t *testing.T) {
	cfg := map[string]interface{}{
		"provider": "k6-operator",
	}
	cfgBytes, _ := json.Marshal(cfg)
	ctx := makeContext(cfgBytes, nil)
	p := New(&mockProvider{})
	result, rpcErr := p.Run(nil, ctx)
	assert.False(t, rpcErr.HasError())
	assert.Equal(t, types.PhaseFailed, result.Phase)
	assert.Contains(t, result.Message, "configMapRef")
}

// --- Run: infrastructure error tests ---

func TestRun_TriggerError(t *testing.T) {
	mock := &mockProvider{
		TriggerRunFn: func(_ context.Context, _ *provider.PluginConfig) (string, error) {
			return "", fmt.Errorf("api error: 500")
		},
	}
	p := New(mock)
	ctx := makeContext(makeConfig(nil), nil)
	_, rpcErr := p.Run(nil, ctx)

	assert.True(t, rpcErr.HasError(), "infrastructure errors should return RpcError")
	assert.Contains(t, rpcErr.ErrorString, "api error")
}

func TestRun_GetRunResultError(t *testing.T) {
	mock := &mockProvider{
		GetRunResultFn: func(_ context.Context, _ *provider.PluginConfig, _ string) (*provider.RunResult, error) {
			return nil, fmt.Errorf("network timeout")
		},
	}
	p := New(mock)
	triggeredAt := time.Now().UTC().Format(time.RFC3339)
	status := makeState("run-1", triggeredAt)
	ctx := makeContext(makeConfig(nil), status)
	_, rpcErr := p.Run(nil, ctx)

	assert.True(t, rpcErr.HasError(), "infrastructure errors should return RpcError")
	assert.Contains(t, rpcErr.ErrorString, "network timeout")
}
