package step

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	stepRpc "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Compile-time interface check.
var _ stepRpc.StepPlugin = (*K6StepPlugin)(nil)

const (
	// pluginName must match the name in argo-rollouts-config ConfigMap.
	pluginName = "jmichalek132/k6-step"

	// defaultTimeout is used when PluginConfig.Timeout is empty (D-03).
	defaultTimeout = 5 * time.Minute

	// maxTimeout is the upper bound for configured timeout (D-03).
	maxTimeout = 2 * time.Hour

	// requeueAfter is the fixed requeue interval for active runs (D-04).
	requeueAfter = 15 * time.Second
)

// stepState holds the persisted state across Run() calls via RpcStepContext.Status.
type stepState struct {
	RunID       string `json:"runId"`
	TriggeredAt string `json:"triggeredAt"`
	TestRunURL  string `json:"testRunURL"`
	FinalStatus string `json:"finalStatus,omitempty"`
}

// K6StepPlugin implements the RpcStep interface for k6 step plugin.
// It is the core logic for the fire-and-wait lifecycle:
// first Run() triggers a k6 test run, subsequent Run() calls poll until terminal.
type K6StepPlugin struct {
	provider provider.Provider
}

// New creates a new K6StepPlugin with the given provider backend.
func New(p provider.Provider) *K6StepPlugin {
	return &K6StepPlugin{provider: p}
}

// InitPlugin is called once when the plugin is loaded. No initialization needed.
func (k *K6StepPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}

// Type returns the plugin name used for registration.
func (k *K6StepPlugin) Type() string {
	return pluginName
}

// Run executes the step plugin lifecycle.
// First call: triggers a k6 test run (or attaches to an existing one via testRunId).
// Subsequent calls: polls GetRunResult until terminal state.
// Terminal: returns PhaseSuccessful (Passed) or PhaseFailed (Failed/Errored/Aborted/timeout).
func (k *K6StepPlugin) Run(_ *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	cfg, err := k.parseConfig(ctx)
	if err != nil {
		return types.RpcStepResult{
			Phase:   types.PhaseFailed,
			Message: err.Error(),
		}, types.RpcError{}
	}

	timeout, err := parseTimeout(cfg.Timeout)
	if err != nil {
		return types.RpcStepResult{
			Phase:   types.PhaseFailed,
			Message: err.Error(),
		}, types.RpcError{}
	}

	// Unmarshal persisted state from previous Run() call.
	var state stepState
	if len(ctx.Status) > 0 {
		if err := json.Unmarshal(ctx.Status, &state); err != nil {
			return types.RpcStepResult{}, types.RpcError{
				ErrorString: fmt.Sprintf("unmarshal step state: %v", err),
			}
		}
	}

	// First call detection: no runId in state.
	if state.RunID == "" {
		if cfg.TestRunID != "" {
			// Poll-only mode: use provided testRunId (D-12).
			state.RunID = cfg.TestRunID
			slog.Info("step plugin Run: poll-only mode",
				"runId", state.RunID,
			)
		} else {
			// Trigger mode: start a new run.
			runID, err := k.provider.TriggerRun(context.Background(), cfg)
			if err != nil {
				return types.RpcStepResult{}, types.RpcError{
					ErrorString: fmt.Sprintf("trigger run: %v", err),
				}
			}
			state.RunID = runID
			slog.Info("step plugin Run: triggered",
				"runId", state.RunID,
			)
		}
		state.TriggeredAt = time.Now().UTC().Format(time.RFC3339)
	}

	// Check timeout (D-01, D-02).
	triggeredAt, err := time.Parse(time.RFC3339, state.TriggeredAt)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{
			ErrorString: fmt.Sprintf("parse triggeredAt: %v", err),
		}
	}
	elapsed := time.Since(triggeredAt)
	if elapsed > timeout {
		// Timeout exceeded: stop the run and fail.
		if stopErr := k.provider.StopRun(context.Background(), cfg, state.RunID); stopErr != nil {
			slog.Warn("failed to stop run on timeout",
				"runId", state.RunID,
				"error", stopErr,
			)
		}
		state.FinalStatus = "TimedOut"
		statusJSON, _ := json.Marshal(state)
		return types.RpcStepResult{
			Phase:   types.PhaseFailed,
			Message: fmt.Sprintf("timed out after %s", timeout),
			Status:  statusJSON,
		}, types.RpcError{}
	}

	// Poll the run result.
	result, err := k.provider.GetRunResult(context.Background(), cfg, state.RunID)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{
			ErrorString: fmt.Sprintf("get run result: %v", err),
		}
	}

	// Update state from result.
	state.TestRunURL = result.TestRunURL

	// Map RunState to StepPhase (D-14).
	var phase types.StepPhase
	var message string
	var requeue time.Duration

	switch result.State {
	case provider.Running:
		phase = types.PhaseRunning
		requeue = requeueAfter
	case provider.Passed:
		phase = types.PhaseSuccessful
		state.FinalStatus = string(provider.Passed)
	case provider.Failed:
		phase = types.PhaseFailed
		state.FinalStatus = string(provider.Failed)
	case provider.Errored:
		phase = types.PhaseFailed
		state.FinalStatus = string(provider.Errored)
		message = "k6 run errored"
	case provider.Aborted:
		phase = types.PhaseFailed
		state.FinalStatus = string(provider.Aborted)
		message = "k6 run aborted"
	}

	statusJSON, _ := json.Marshal(state)
	return types.RpcStepResult{
		Phase:        phase,
		Message:      message,
		RequeueAfter: requeue,
		Status:       statusJSON,
	}, types.RpcError{}
}

// Terminate stops an active run and returns an empty result (D-07).
func (k *K6StepPlugin) Terminate(_ *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	k.stopActiveRun(ctx, "terminate")
	return types.RpcStepResult{}, types.RpcError{}
}

// Abort reverts actions performed during Run by stopping the active run (D-07).
func (k *K6StepPlugin) Abort(_ *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	k.stopActiveRun(ctx, "abort")
	return types.RpcStepResult{}, types.RpcError{}
}

// stopActiveRun is a shared helper for Terminate and Abort.
// It parses config and state, then calls StopRun if a runId is present.
// Errors are logged but never returned (D-08).
func (k *K6StepPlugin) stopActiveRun(ctx *types.RpcStepContext, action string) {
	cfg, err := k.parseConfig(ctx)
	if err != nil {
		slog.Warn("failed to parse config during "+action,
			"error", err,
		)
		return
	}

	var state stepState
	if len(ctx.Status) > 0 {
		if err := json.Unmarshal(ctx.Status, &state); err != nil {
			slog.Warn("failed to unmarshal state during "+action,
				"error", err,
			)
			return
		}
	}

	if state.RunID == "" {
		return
	}

	if err := k.provider.StopRun(context.Background(), cfg, state.RunID); err != nil {
		slog.Warn("failed to stop run during "+action,
			"runId", state.RunID,
			"error", err,
		)
	}
}

// parseConfig extracts and validates the plugin config from the step context.
func (k *K6StepPlugin) parseConfig(ctx *types.RpcStepContext) (*provider.PluginConfig, error) {
	if ctx == nil || len(ctx.Config) == 0 {
		return nil, fmt.Errorf("step context or config is nil")
	}

	var cfg provider.PluginConfig
	if err := json.Unmarshal(ctx.Config, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal plugin config: %w", err)
	}

	if cfg.TestID == "" && cfg.TestRunID == "" {
		return nil, fmt.Errorf("either testId or testRunId is required")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("apiToken is required (check Secret reference)")
	}
	if cfg.StackID == "" {
		return nil, fmt.Errorf("stackId is required (check Secret reference)")
	}

	return &cfg, nil
}

// parseTimeout parses the timeout string into a duration (D-03).
// Empty string returns defaultTimeout. Invalid strings return an error.
// Values exceeding maxTimeout return an error.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return defaultTimeout, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", s, err)
	}

	if d > maxTimeout {
		return 0, fmt.Errorf("timeout %s exceeds maximum of 2h", d)
	}

	return d, nil
}
