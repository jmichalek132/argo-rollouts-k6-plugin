package provider

import "context"

// RunState represents the state of a test run from the provider's perspective.
type RunState string

const (
	Running RunState = "Running"
	Passed  RunState = "Passed"
	Failed  RunState = "Failed"
	Errored RunState = "Errored"
	Aborted RunState = "Aborted"
)

// IsTerminal returns true if the run has reached a final state.
func (s RunState) IsTerminal() bool {
	return s != Running
}

// Percentiles holds HTTP request duration percentile values in milliseconds.
type Percentiles struct {
	P50 float64
	P95 float64
	P99 float64
}

// RunResult holds the outcome of a test run query.
// During active runs (State == Running), metric fields contain partial/live values.
// After terminal state, all fields reflect final values.
type RunResult struct {
	State            RunState
	TestRunURL       string
	ThresholdsPassed bool
	HTTPReqFailed    float64     // 0.0-1.0 fraction of failed requests
	HTTPReqDuration  Percentiles // milliseconds
	HTTPReqs         float64     // requests per second
}

// Provider defines the interface for k6 execution backends.
// Implementations are stateless -- credentials are passed via PluginConfig on every call.
type Provider interface {
	// TriggerRun starts a new k6 test run. Returns the run ID as a string.
	TriggerRun(ctx context.Context, cfg *PluginConfig) (runID string, err error)

	// GetRunResult returns current status and metrics for a run.
	// Returns partial metrics during active runs (State == Running).
	GetRunResult(ctx context.Context, cfg *PluginConfig, runID string) (*RunResult, error)

	// StopRun requests cancellation of a running test.
	// No-op if the run is already in a terminal state.
	StopRun(ctx context.Context, cfg *PluginConfig, runID string) error

	// Cleanup releases any backend resources associated with a terminal run.
	// Called by K6MetricProvider.GarbageCollect for each runID present in
	// ar.Status.MetricResults[*].Measurements[*].Metadata["runId"] when the
	// Argo Rollouts analysis controller trims measurements past its retention
	// limit. Idempotent: NotFound / already-cleaned-up MUST return nil.
	//
	// Distinct from StopRun (which means "cancel an in-flight run"):
	// semantically, Cleanup means "release resources after a terminal state."
	// On the k6-operator backend they happen to share the delete code path
	// today; future providers may differentiate.
	Cleanup(ctx context.Context, cfg *PluginConfig, runID string) error

	// Name returns the provider identifier for logging.
	Name() string
}
