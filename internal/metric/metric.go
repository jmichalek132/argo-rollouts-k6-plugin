package metric

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Compile-time interface check.
var _ rpc.MetricProviderPlugin = (*K6MetricProvider)(nil)

// pluginName must match the name in argo-rollouts-config ConfigMap.
const pluginName = "jmichalek132/k6"

// providerCallTimeout is the maximum duration for any single provider API call.
// The HTTP client has its own 30s timeout, but this context timeout ensures the
// plugin goroutine is never blocked indefinitely if the provider hangs.
const providerCallTimeout = 60 * time.Second

// K6MetricProvider implements the RpcMetricProvider interface for k6 metrics.
// It is stateless -- all per-measurement state lives in Measurement.Metadata.
type K6MetricProvider struct {
	provider provider.Provider
}

// New creates a new K6MetricProvider with the given provider backend.
func New(p provider.Provider) *K6MetricProvider {
	return &K6MetricProvider{provider: p}
}

// InitPlugin is called once when the plugin is loaded. No initialization needed.
func (k *K6MetricProvider) InitPlugin() types.RpcError {
	return types.RpcError{}
}

// Type returns the plugin name used for registration.
func (k *K6MetricProvider) Type() string {
	return pluginName
}

// GetMetadata returns additional metadata for display. Not used by this plugin.
func (k *K6MetricProvider) GetMetadata(_ v1alpha1.Metric) map[string]string {
	return map[string]string{}
}

// GarbageCollect is called by the Argo Rollouts analysis controller when the
// number of measurements for a metric exceeds the retention limit (see
// analysis/analysis.go:775-800 in argo-rollouts v1.9.0). We use this hook as the
// success-path cleanup trigger for k6-operator TestRun CRs created by Run/Resume.
//
// Walks ar.Status.MetricResults for the entry matching metric.Name, iterates
// its Measurements, extracts Metadata["runId"] per measurement, and dispatches
// provider.Cleanup for each non-empty runID. The Router resolves the correct
// provider backend based on cfg.Provider -- grafana-cloud is a no-op, k6-operator
// issues an idempotent Delete on the TestRun CR.
//
// Per GC-03: cleanup errors are logged at slog.Warn with structured fields
// (runId, metric, analysisRun, namespace, error) but NEVER returned as RpcError.
// This mirrors how Terminate swallows StopRun errors.
//
// The `limit` parameter is used by Argo Rollouts after our return to trim
// the Measurements slice; we intentionally do not consult it here -- we clean
// up every runID in the slice. CRs for runIDs whose backing CR is already gone
// (reclaimed, or the run is still in flight) are no-ops via IsNotFound handling
// in the provider.
func (k *K6MetricProvider) GarbageCollect(ar *v1alpha1.AnalysisRun, metric v1alpha1.Metric, _ int) types.RpcError {
	if ar == nil {
		return types.RpcError{}
	}

	cfg, err := parseConfig(metric)
	if err != nil {
		// Config parse failure at GC time is non-fatal: log and return empty RpcError.
		slog.Warn("garbage collect: parseConfig failed, skipping cleanup",
			"metric", metric.Name,
			"error", err,
		)
		return types.RpcError{}
	}
	populateFromAnalysisRun(cfg, ar, "metric.GarbageCollect")

	for _, result := range ar.Status.MetricResults {
		if result.Name != metric.Name {
			continue
		}
		for _, m := range result.Measurements {
			runID := m.Metadata["runId"]
			if runID == "" {
				continue
			}
			// cancel() is called at the END of each iteration (not via defer-in-loop,
			// which would leak contexts until function return).
			ctx, cancel := context.WithTimeout(context.Background(), providerCallTimeout)
			if cleanupErr := k.provider.Cleanup(ctx, cfg, runID); cleanupErr != nil {
				slog.Warn("failed to cleanup run during garbage collect",
					"runId", runID,
					"metric", metric.Name,
					"analysisRun", cfg.AnalysisRunName,
					"namespace", cfg.Namespace,
					"error", cleanupErr,
				)
			}
			cancel()
		}
	}
	return types.RpcError{}
}

// Run triggers a k6 test run (or attaches to an existing one) and returns a Running measurement.
// If testRunId is provided in config, it uses that run (poll-only mode, D-04).
// Otherwise, it calls TriggerRun to start a new run.
func (k *K6MetricProvider) Run(ar *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startedAt := metav1.Now()
	measurement := v1alpha1.Measurement{
		Phase:     v1alpha1.AnalysisPhaseRunning,
		StartedAt: &startedAt,
		Metadata:  map[string]string{},
	}

	cfg, err := parseConfig(metric)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	populateFromAnalysisRun(cfg, ar, "metric.Run")

	ctx, cancel := context.WithTimeout(context.Background(), providerCallTimeout)
	defer cancel()
	var runID string

	if cfg.TestRunID != "" {
		// Poll-only mode: use provided testRunId (D-04)
		runID = cfg.TestRunID
	} else {
		// Trigger mode: start a new run
		runID, err = k.provider.TriggerRun(ctx, cfg)
		if err != nil {
			return metricutil.MarkMeasurementError(measurement, err)
		}
	}

	measurement.Metadata["runId"] = runID
	setResumeAt(&measurement, metric)
	slog.Info("metric plugin Run",
		"runId", runID,
		"metric", cfg.Metric,
		"mode", triggerMode(cfg),
		"namespace", cfg.Namespace,
		"rollout", cfg.RolloutName,
		"analysisRun", cfg.AnalysisRunName,
	)
	return measurement
}

// Resume polls GetRunResult and returns the metric value with the appropriate analysis phase.
// Sets metadata on every call (D-11/D-12): runId, testRunURL, runState, metricValue.
func (k *K6MetricProvider) Resume(ar *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	cfg, err := parseConfig(metric)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	populateFromAnalysisRun(cfg, ar, "metric.Resume")

	if measurement.Metadata == nil {
		measurement.Metadata = map[string]string{}
	}

	runID := measurement.Metadata["runId"]
	if runID == "" {
		return metricutil.MarkMeasurementError(measurement, fmt.Errorf("runId not found in measurement metadata"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), providerCallTimeout)
	defer cancel()
	result, err := k.provider.GetRunResult(ctx, cfg, runID)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	// Always update metadata (D-10/D-11)
	measurement.Metadata["testRunURL"] = result.TestRunURL
	measurement.Metadata["runState"] = string(result.State)

	// Extract metric value
	value, err := extractMetricValue(result, cfg.Metric, cfg.Aggregation)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	valueStr := strconv.FormatFloat(value, 'f', -1, 64)
	measurement.Value = valueStr
	measurement.Metadata["metricValue"] = valueStr

	// Map state to phase
	switch result.State {
	case provider.Running:
		measurement.Phase = v1alpha1.AnalysisPhaseRunning
		setResumeAt(&measurement, metric)
	case provider.Passed:
		measurement.Phase = v1alpha1.AnalysisPhaseSuccessful
		finishedAt := metav1.Now()
		measurement.FinishedAt = &finishedAt
	case provider.Failed:
		measurement.Phase = v1alpha1.AnalysisPhaseFailed
		finishedAt := metav1.Now()
		measurement.FinishedAt = &finishedAt
	case provider.Errored, provider.Aborted:
		measurement.Phase = v1alpha1.AnalysisPhaseError
		measurement.Message = fmt.Sprintf("k6 run %s: %s", result.State, runID)
		finishedAt := metav1.Now()
		measurement.FinishedAt = &finishedAt
	}

	slog.Info("metric plugin Resume",
		"runId", runID,
		"state", result.State,
		"phase", measurement.Phase,
		"value", valueStr,
		"namespace", cfg.Namespace,
		"rollout", cfg.RolloutName,
		"analysisRun", cfg.AnalysisRunName,
	)
	return measurement
}

// Terminate stops an active run and returns a PhaseError measurement (D-15/D-16).
func (k *K6MetricProvider) Terminate(ar *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	cfg, err := parseConfig(metric)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	populateFromAnalysisRun(cfg, ar, "metric.Terminate")

	if measurement.Metadata == nil {
		measurement.Metadata = map[string]string{}
	}

	runID := measurement.Metadata["runId"]
	if runID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), providerCallTimeout)
		defer cancel()
		if err := k.provider.StopRun(ctx, cfg, runID); err != nil {
			slog.Warn("failed to stop run during terminate",
				"runId", runID,
				"error", err,
			)
		}
	}

	measurement.Phase = v1alpha1.AnalysisPhaseError
	measurement.Message = "metric measurement terminated"
	finishedAt := metav1.Now()
	measurement.FinishedAt = &finishedAt
	return measurement
}

// populateFromAnalysisRun injects AR metadata into cfg per D-01/D-03/D-04/D-09.
//   - Namespace: ar.Namespace wins ONLY when cfg.Namespace is empty (D-01: user cfg wins).
//   - RolloutName: walked from ar.OwnerReferences for the Rollout controller ref (D-03).
//     The Argo Rollouts controller stamps exactly one such ref on every AR it creates
//     (verified: argo-rollouts rollout/analysis.go:502 NewControllerRef(c.rollout, ...)).
//     Only the entry with Controller==true wins; a plain Kind==Rollout ref without the
//     Controller flag must NOT hijack RolloutName.
//     Standalone ARs (kubectl-applied fixtures, manual troubleshooting) have no owner ref;
//     we log a warning and leave RolloutName empty (D-04).
//   - AnalysisRunName and AnalysisRunUID: always set from AR ObjectMeta when ar is non-nil.
//   - Nil AR: warn and skip all extraction (D-09). Matches the gob-boundary defense.
//
// phase identifies which caller emitted the warning for log grep-ability.
func populateFromAnalysisRun(cfg *provider.PluginConfig, ar *v1alpha1.AnalysisRun, phase string) {
	if ar == nil {
		slog.Warn("nil AnalysisRun received; skipping metadata extraction",
			"phase", phase,
		)
		return
	}

	cfg.AnalysisRunName = ar.Name
	cfg.AnalysisRunUID = string(ar.UID)
	if cfg.Namespace == "" {
		cfg.Namespace = ar.Namespace
	}

	for _, owner := range ar.OwnerReferences {
		if owner.Kind == "Rollout" && owner.Controller != nil && *owner.Controller {
			cfg.RolloutName = owner.Name
			return
		}
	}

	slog.Warn("standalone AnalysisRun (no Rollout owner ref); RolloutName left empty",
		"analysisRun", ar.Name,
		"namespace", ar.Namespace,
	)
}

// parseConfig extracts and validates the plugin config from the metric spec.
// Validation is per-provider (per D-05). Grafana Cloud fields are only required
// when provider is empty or "grafana-cloud". k6-operator fields are validated
// via cfg.ValidateK6Operator() (centralized in config.go to prevent drift).
// Unknown providers pass parseConfig; the Router rejects them at dispatch time.
func parseConfig(metric v1alpha1.Metric) (*provider.PluginConfig, error) {
	rawCfg, ok := metric.Provider.Plugin[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin config not found for %s", pluginName)
	}
	var cfg provider.PluginConfig
	if err := json.Unmarshal(rawCfg, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal plugin config: %w", err)
	}

	// Shared validation: metric field required for all providers.
	if cfg.Metric == "" {
		return nil, fmt.Errorf("metric field is required (thresholds|http_req_failed|http_req_duration|http_reqs)")
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

// extractMetricValue extracts the appropriate metric value from a RunResult.
func extractMetricValue(result *provider.RunResult, metricType, aggregation string) (float64, error) {
	switch metricType {
	case "thresholds":
		if result.ThresholdsPassed {
			return 1, nil
		}
		return 0, nil
	case "http_req_failed":
		return result.HTTPReqFailed, nil
	case "http_req_duration":
		switch aggregation {
		case "p50":
			return result.HTTPReqDuration.P50, nil
		case "p95":
			return result.HTTPReqDuration.P95, nil
		case "p99":
			return result.HTTPReqDuration.P99, nil
		case "":
			return 0, fmt.Errorf("aggregation is required for http_req_duration (p50|p95|p99)")
		default:
			return 0, fmt.Errorf("unsupported aggregation %q for http_req_duration (use p50|p95|p99)", aggregation)
		}
	case "http_reqs":
		return result.HTTPReqs, nil
	default:
		return 0, fmt.Errorf("unsupported metric type %q (use thresholds|http_req_failed|http_req_duration|http_reqs)", metricType)
	}
}

// setResumeAt sets measurement.ResumeAt based on the metric interval so the Argo Rollouts
// analysis controller schedules a timely reconcile for in-progress measurements.
// Without ResumeAt, the controller only re-queues when the AnalysisRun status changes
// (no-op on identical Resume() results) or at the 15-minute informer resync.
func setResumeAt(measurement *v1alpha1.Measurement, metric v1alpha1.Metric) {
	if metric.Interval == "" {
		return
	}
	d, err := metric.Interval.Duration()
	if err != nil {
		return
	}
	resumeAt := metav1.NewTime(time.Now().Add(d))
	measurement.ResumeAt = &resumeAt
}

// triggerMode returns a string describing whether this is a trigger or poll-only run.
func triggerMode(cfg *provider.PluginConfig) string {
	if cfg.TestRunID != "" {
		return "poll-only"
	}
	return "trigger"
}
