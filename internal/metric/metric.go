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

// GarbageCollect is a no-op for this plugin (D-14).
// All state lives in Measurement.Metadata, which Argo Rollouts owns.
func (k *K6MetricProvider) GarbageCollect(_ *v1alpha1.AnalysisRun, _ v1alpha1.Metric, _ int) types.RpcError {
	return types.RpcError{}
}

// Run triggers a k6 test run (or attaches to an existing one) and returns a Running measurement.
// If testRunId is provided in config, it uses that run (poll-only mode, D-04).
// Otherwise, it calls TriggerRun to start a new run.
func (k *K6MetricProvider) Run(_ *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
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

	ctx := context.Background()
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
	)
	return measurement
}

// Resume polls GetRunResult and returns the metric value with the appropriate analysis phase.
// Sets metadata on every call (D-11/D-12): runId, testRunURL, runState, metricValue.
func (k *K6MetricProvider) Resume(_ *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	cfg, err := parseConfig(metric)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	runID := measurement.Metadata["runId"]
	if runID == "" {
		return metricutil.MarkMeasurementError(measurement, fmt.Errorf("runId not found in measurement metadata"))
	}

	ctx := context.Background()
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
	)
	return measurement
}

// Terminate stops an active run and returns a PhaseError measurement (D-15/D-16).
func (k *K6MetricProvider) Terminate(_ *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	cfg, err := parseConfig(metric)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	runID := measurement.Metadata["runId"]
	if runID != "" {
		ctx := context.Background()
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

// parseConfig extracts and validates the plugin config from the metric spec.
func parseConfig(metric v1alpha1.Metric) (*provider.PluginConfig, error) {
	rawCfg, ok := metric.Provider.Plugin[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin config not found for %s", pluginName)
	}
	var cfg provider.PluginConfig
	if err := json.Unmarshal(rawCfg, &cfg); err != nil {
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
	if cfg.Metric == "" {
		return nil, fmt.Errorf("metric field is required (thresholds|http_req_failed|http_req_duration|http_reqs)")
	}
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
