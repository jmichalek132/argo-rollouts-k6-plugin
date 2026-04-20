package step

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	stepRpc "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
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

	// providerCallTimeout is the maximum duration for any single provider API call.
	providerCallTimeout = 60 * time.Second
)

// stepState holds the persisted state across Run() calls via RpcStepContext.Status.
type stepState struct {
	RunID       string `json:"runId"`
	TriggeredAt string `json:"triggeredAt"`
	TestRunURL  string `json:"testRunURL"`
	FinalStatus string `json:"finalStatus,omitempty"`
	// CleanupDone is set to true after provider.Cleanup fires for a success-path
	// terminal state (Passed/Failed/Errored). Prevents re-firing Cleanup on
	// subsequent Run() invocations that may arrive during controller
	// reconciliation races after the step has reached a terminal phase.
	//
	// Best-effort per GC-03: this flag flips to true even if the underlying
	// Cleanup call returned an error -- no retry loops (REQUIREMENTS.md
	// "Retry loop on cleanup failure" is explicitly Out of Scope).
	//
	// Backward compatible: the json:",omitempty" tag keeps serialized state
	// compact for pre-v0.4.0 runs in flight during upgrade; an absent field
	// deserializes as false, which is the correct default.
	CleanupDone bool `json:"cleanupDone,omitempty"`
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
func (k *K6StepPlugin) Run(rollout *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	cfg, err := k.parseConfig(ctx)
	if err != nil {
		return types.RpcStepResult{
			Phase:   types.PhaseFailed,
			Message: err.Error(),
		}, types.RpcError{}
	}

	populateFromRollout(cfg, rollout, "step.Run")

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
			triggerCtx, triggerCancel := context.WithTimeout(context.Background(), providerCallTimeout)
			defer triggerCancel()
			runID, err := k.provider.TriggerRun(triggerCtx, cfg)
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
		stopCtx, stopCancel := context.WithTimeout(context.Background(), providerCallTimeout)
		defer stopCancel()
		if stopErr := k.provider.StopRun(stopCtx, cfg, state.RunID); stopErr != nil {
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
	pollCtx, pollCancel := context.WithTimeout(context.Background(), providerCallTimeout)
	defer pollCancel()
	result, err := k.provider.GetRunResult(pollCtx, cfg, state.RunID)
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

	// Success-path cleanup hook (GC-02).
	// Fires on the FIRST observation of a success-path terminal state
	// (Passed, Failed, Errored) and NEVER on Running, Aborted, or TimedOut:
	//   - Running: run still in flight.
	//   - Aborted: the Terminate/Abort RPC path already invoked StopRun via
	//     stopActiveRun (D-07). On the k6-operator backend StopRun and Cleanup
	//     share deleteCR, so firing Cleanup here would be a redundant
	//     NotFound-as-success roundtrip.
	//   - TimedOut: the timeout branch above returns early after calling
	//     StopRun; it never reaches this point.
	//
	// Guarded by state.CleanupDone for once-per-terminal-transition semantics:
	// Argo Rollouts may replay Run() during reconciliation even after a
	// terminal result, and we must suppress repeat Cleanup fires.
	//
	// state.CleanupDone is set to true REGARDLESS of Cleanup outcome --
	// best-effort per GC-03, no retry loops per REQUIREMENTS.md Out of Scope.
	// Cleanup errors are logged at slog.Warn and do NOT alter result.Phase,
	// result.Message, or the RpcError returned to the controller.
	isSuccessPathTerminal := result.State == provider.Passed ||
		result.State == provider.Failed ||
		result.State == provider.Errored
	if isSuccessPathTerminal && !state.CleanupDone && state.RunID != "" {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), providerCallTimeout)
		if cleanupErr := k.provider.Cleanup(cleanupCtx, cfg, state.RunID); cleanupErr != nil {
			slog.Warn("failed to cleanup run after terminal state",
				"runId", state.RunID,
				"rollout", cfg.RolloutName,
				"namespace", cfg.Namespace,
				"state", string(result.State),
				"error", cleanupErr,
			)
		}
		cleanupCancel()
		state.CleanupDone = true
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
func (k *K6StepPlugin) Terminate(rollout *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	k.stopActiveRun(rollout, ctx, "terminate")
	return types.RpcStepResult{}, types.RpcError{}
}

// Abort reverts actions performed during Run by stopping the active run (D-07).
func (k *K6StepPlugin) Abort(rollout *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	k.stopActiveRun(rollout, ctx, "abort")
	return types.RpcStepResult{}, types.RpcError{}
}

// stopActiveRun is a shared helper for Terminate and Abort.
// It parses config and state, then calls StopRun if a runId is present.
// Errors are logged but never returned (D-08).
func (k *K6StepPlugin) stopActiveRun(rollout *v1alpha1.Rollout, ctx *types.RpcStepContext, action string) {
	cfg, err := k.parseConfig(ctx)
	if err != nil {
		slog.Warn("failed to parse config during "+action,
			"error", err,
		)
		return
	}

	populateFromRollout(cfg, rollout, "step."+action)

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

	stopCtx, stopCancel := context.WithTimeout(context.Background(), providerCallTimeout)
	defer stopCancel()
	if err := k.provider.StopRun(stopCtx, cfg, state.RunID); err != nil {
		slog.Warn("failed to stop run during "+action,
			"runId", state.RunID,
			"error", err,
		)
	}
}

// populateFromRollout injects Rollout metadata into cfg per D-01/D-05/D-06/D-09.
//   - Namespace: rollout.Namespace wins ONLY when cfg.Namespace is empty (D-01: user cfg wins).
//   - RolloutName: from rollout.Name (D-06: the Rollout IS the parent, no owner-ref walk).
//   - RolloutUID: from string(rollout.UID) (D-05: used by parentOwnerRef for the Rollout
//     owner reference since the step plugin has no AnalysisRun).
//   - AnalysisRunName / AnalysisRunUID: intentionally NOT set -- step plugin has no AR (D-05).
//   - Nil Rollout: warn and skip all extraction (D-09). Matches the gob-boundary defense.
//     Note: nil rollout preserves pre-fix behavior but downstream StopRun behavior with
//     under-populated cfg is provider-specific (R8 from codex review).
//
// phase identifies which caller emitted the warning for log grep-ability.
func populateFromRollout(cfg *provider.PluginConfig, rollout *v1alpha1.Rollout, phase string) {
	if rollout == nil {
		slog.Warn("nil Rollout received; skipping metadata extraction",
			"phase", phase,
		)
		return
	}

	cfg.RolloutName = rollout.Name
	cfg.RolloutUID = string(rollout.UID)
	if cfg.Namespace == "" {
		cfg.Namespace = rollout.Namespace
	}
}

// parseConfig extracts and validates the plugin config from the step context.
// Validation is per-provider (per D-05). Grafana Cloud fields are only required
// when provider is empty or "grafana-cloud". k6-operator fields are validated
// via cfg.ValidateK6Operator() (centralized in config.go to prevent drift).
// Unknown providers pass parseConfig; the Router rejects them at dispatch time.
func (k *K6StepPlugin) parseConfig(ctx *types.RpcStepContext) (*provider.PluginConfig, error) {
	if ctx == nil || len(ctx.Config) == 0 {
		return nil, fmt.Errorf("step context or config is nil")
	}

	var cfg provider.PluginConfig
	if err := json.Unmarshal(ctx.Config, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal plugin config: %w", err)
	}

	// Per-provider validation (per D-05).
	if cfg.IsGrafanaCloud() {
		if err := cfg.ValidateGrafanaCloud(); err != nil {
			return nil, err
		}
	} else if cfg.Provider == "k6-operator" {
		if err := cfg.ValidateK6Operator(); err != nil {
			return nil, err
		}
	}
	// Unknown providers pass parseConfig; the Router rejects them at dispatch.

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

	if d <= 0 {
		return 0, fmt.Errorf("timeout must be positive, got %s", s)
	}

	if d > maxTimeout {
		return 0, fmt.Errorf("timeout %s exceeds maximum of 2h", d)
	}

	return d, nil
}
