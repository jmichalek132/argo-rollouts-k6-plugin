package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// PodLogReader abstracts pod log reading so unit tests can inject a mock
// instead of relying on the fake clientset's GetLogs (which returns constant
// "fake logs" -- Pitfall 3 from RESEARCH).
type PodLogReader interface {
	ReadLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error)
}

// k8sPodLogReader implements PodLogReader using the real Kubernetes client.
type k8sPodLogReader struct {
	client kubernetes.Interface
}

func (r *k8sPodLogReader) ReadLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	req := r.client.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("stream logs for pod %s/%s: %w", namespace, podName, err)
	}
	defer stream.Close()
	buf, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("read logs for pod %s/%s: %w", namespace, podName, err)
	}
	return string(buf), nil
}

// k6Summary represents the top-level handleSummary data object.
// Only fields needed for metric extraction are included.
// Uses json.RawMessage for root_group to validate presence without parsing.
type k6Summary struct {
	Metrics   map[string]k6Metric `json:"metrics"`
	RootGroup json.RawMessage     `json:"root_group"`
}

type k6Metric struct {
	Type       string                       `json:"type"`
	Contains   string                       `json:"contains"`
	Values     map[string]float64           `json:"values"`
	Thresholds map[string]k6ThresholdResult `json:"thresholds,omitempty"`
}

type k6ThresholdResult struct {
	OK bool `json:"ok"`
}

// summaryMetrics holds extracted metrics for aggregation across pods.
type summaryMetrics struct {
	HTTPReqFailed   float64
	HTTPReqDuration provider.Percentiles
	HTTPReqs        float64
	// Fields needed for aggregation math:
	httpReqsCount      float64 // total count from "count" key (for weighting)
	httpReqFailedCount float64 // failed count derived from rate * total (for aggregation)
}

// findSummaryJSON scans log output from the end to find a handleSummary JSON object.
//
// Detection validates BOTH "metrics" AND "root_group" top-level keys (per D-02).
// This rejects console.log JSON fragments and non-handleSummary JSON.
//
// Algorithm:
//  1. Split output into lines.
//  2. Walk backward from last line to find closing '}'.
//  3. Accumulate lines walking further backward counting brace depth until matching opening '{' at depth 0.
//  4. Attempt json.Unmarshal into k6Summary.
//  5. Validate: parsed object must have non-nil Metrics map AND non-nil RootGroup.
//  6. If candidate fails validation, continue scanning backward for another candidate.
//  7. Return nil, nil if no valid handleSummary JSON found (graceful -- not an error).
//  8. Return nil, error if a candidate JSON block cannot be unmarshalled (truncated/malformed).
func findSummaryJSON(logs string) (*k6Summary, error) {
	if logs == "" {
		return nil, nil
	}

	lines := strings.Split(logs, "\n")

	// Scan backward through the log looking for JSON object candidates.
	i := len(lines) - 1

	// Track if we found any JSON-like structure (for error reporting).
	var lastParseErr error
	foundCandidate := false // true if we entered brace-counting at least once

	for i >= 0 {
		// Skip empty/whitespace-only lines from the end.
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i--
			continue
		}

		// Look for a line ending with '}' (potential end of JSON object).
		if !strings.HasSuffix(line, "}") {
			i--
			continue
		}

		// Found a potential closing brace. Walk backward accumulating lines
		// and counting brace depth to find the matching opening '{'.
		foundCandidate = true
		depth := 0
		var block []string
		j := i
		foundOpen := false

		for j >= 0 {
			block = append([]string{lines[j]}, block...)
			// Count braces in this line (simple counting, not inside strings,
			// but sufficient for structured handleSummary JSON).
			for _, ch := range lines[j] {
				switch ch {
				case '{':
					depth--
				case '}':
					depth++
				}
			}
			if depth == 0 {
				foundOpen = true
				break
			}
			j--
		}

		if !foundOpen {
			// Unbalanced braces -- truncated JSON.
			return nil, fmt.Errorf("truncated JSON in pod logs: unbalanced braces")
		}

		// Try to parse the accumulated block as JSON.
		candidate := strings.Join(block, "\n")
		var summary k6Summary
		if err := json.Unmarshal([]byte(candidate), &summary); err != nil {
			// Record the parse error but continue scanning backward.
			lastParseErr = fmt.Errorf("malformed JSON in pod logs: %w", err)
			i = j - 1
			continue
		}

		// Validate: BOTH "metrics" AND "root_group" must be present (D-02 compliance).
		if summary.Metrics != nil && summary.RootGroup != nil {
			return &summary, nil
		}

		// This JSON object didn't have both keys -- continue scanning backward.
		i = j - 1
	}

	// No valid handleSummary JSON found.
	if lastParseErr != nil {
		return nil, lastParseErr
	}
	// Detect truncated JSON: lines contain '{' but no '}'-ending line was found.
	// If foundCandidate is true, we did find balanced JSON blocks -- they just
	// weren't handleSummary. That's not truncation, it's absence.
	if !foundCandidate {
		// Check if any line contains '{' -- indicates truncated JSON.
		for _, l := range lines {
			if strings.Contains(l, "{") {
				return nil, fmt.Errorf("truncated JSON in pod logs: unbalanced braces")
			}
		}
	}
	return nil, nil
}

// extractMetricsFromSummary extracts metric values from a parsed k6Summary.
//
// Key mappings (per D-06, RESEARCH Example 2):
//   - http_req_failed: "rate" key from rate metric values
//   - http_req_duration: "med" for P50 (NOT "p(50)" -- Pitfall 1), with "p(50)" fallback
//   - http_req_duration: "p(95)" for P95, "p(99)" for P99 (0.0 if absent, graceful per D-05)
//   - http_reqs: "rate" key from counter metric values
func extractMetricsFromSummary(summary *k6Summary) summaryMetrics {
	var m summaryMetrics

	// http_req_failed: rate metric.
	if metric, ok := summary.Metrics["http_req_failed"]; ok {
		m.HTTPReqFailed = metric.Values["rate"]
	}

	// http_req_duration: trend metric.
	if metric, ok := summary.Metrics["http_req_duration"]; ok {
		// P50: "med" key (NOT "p(50)") per Pitfall 1.
		if v, ok := metric.Values["med"]; ok {
			m.HTTPReqDuration.P50 = v
		} else if v, ok := metric.Values["p(50)"]; ok {
			m.HTTPReqDuration.P50 = v
		}
		m.HTTPReqDuration.P95 = metric.Values["p(95)"]
		m.HTTPReqDuration.P99 = metric.Values["p(99)"] // 0.0 if not in summaryTrendStats
	}

	// http_reqs: counter metric.
	if metric, ok := summary.Metrics["http_reqs"]; ok {
		m.HTTPReqs = metric.Values["rate"]
		m.httpReqsCount = metric.Values["count"]
	}

	// Derived: failed count for multi-pod aggregation.
	if metric, ok := summary.Metrics["http_reqs"]; ok {
		m.httpReqFailedCount = m.HTTPReqFailed * metric.Values["count"]
	}

	return m
}

// aggregateMetrics combines per-pod summaryMetrics into a single result for distributed runs.
//
// Aggregation rules (matches Grafana Cloud's distributed aggregation approach):
//   - http_reqs (counter): sum rates across pods
//   - http_req_failed (rate): weighted average by request count = sum(failed) / sum(total)
//   - http_req_duration (trend percentiles): weighted average by request count
//
// NOTE: Weighted-average percentile aggregation is an APPROXIMATION. True percentile
// merging requires the full data distribution, which handleSummary does not provide.
// This matches Grafana Cloud's behavior for distributed runs (D-04). With even VU
// distribution across pods (k6-operator default), the approximation is close enough
// for rollout gating decisions. Users needing exact percentiles should use parallelism=1
// or configure k6 to output to Prometheus and use the Prometheus metric provider.
func aggregateMetrics(metrics []summaryMetrics) summaryMetrics {
	if len(metrics) == 0 {
		return summaryMetrics{}
	}
	if len(metrics) == 1 {
		return metrics[0]
	}

	var totalCount float64
	var totalFailedCount float64
	var totalRate float64
	var weightedP50, weightedP95, weightedP99 float64

	for _, m := range metrics {
		totalCount += m.httpReqsCount
		totalFailedCount += m.httpReqFailedCount
		totalRate += m.HTTPReqs
		weightedP50 += m.HTTPReqDuration.P50 * m.httpReqsCount
		weightedP95 += m.HTTPReqDuration.P95 * m.httpReqsCount
		weightedP99 += m.HTTPReqDuration.P99 * m.httpReqsCount
	}

	// Guard: if totalCount == 0, return zero-value (prevents divide-by-zero).
	if totalCount == 0 {
		return summaryMetrics{}
	}

	return summaryMetrics{
		HTTPReqFailed: totalFailedCount / totalCount,
		HTTPReqDuration: provider.Percentiles{
			P50: weightedP50 / totalCount,
			P95: weightedP95 / totalCount,
			P99: weightedP99 / totalCount,
		},
		HTTPReqs:      totalRate,
		httpReqsCount: totalCount,
	}
}

// readPodLogs reads logs from a single pod with bounded options (per D-03).
// TailLines=100 and LimitBytes=64KB prevent unbounded memory allocation
// from large pod logs (T-09-01 mitigation).
func readPodLogs(ctx context.Context, reader PodLogReader, namespace, podName string) (string, error) {
	tailLines := int64(100)
	limitBytes := int64(64 * 1024) // 64KB
	opts := &corev1.PodLogOptions{
		TailLines:  &tailLines,
		LimitBytes: &limitBytes,
	}
	return reader.ReadLogs(ctx, namespace, podName, opts)
}

// parseSummaryFromPods reads logs from all runner pods for a TestRun and
// extracts/aggregates handleSummary metrics.
//
// Uses the same label selector as checkRunnerExitCodes: app=k6,k6_cr=<name>,runner=true.
//
// Graceful degradation (per D-05):
//   - Per-pod failures (log read error, no JSON, parse error) are logged as warnings
//     with structured fields and skipped.
//   - If zero pods yield valid summaries, returns zero metrics with no error.
//   - This matches the cloud provider's populateAggregateMetrics pattern.
func parseSummaryFromPods(ctx context.Context, logReader PodLogReader, client kubernetes.Interface, ns, testRunName string) (summaryMetrics, error) {
	selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return summaryMetrics{}, fmt.Errorf("list runner pods for summary: %w", err)
	}
	if len(pods.Items) == 0 {
		return summaryMetrics{}, fmt.Errorf("no runner pods found for TestRun %s", testRunName)
	}

	var collected []summaryMetrics
	for _, pod := range pods.Items {
		logs, readErr := readPodLogs(ctx, logReader, ns, pod.Name)
		if readErr != nil {
			slog.Warn("failed to read pod logs",
				"pod", pod.Name,
				"namespace", ns,
				"name", testRunName,
				"error", readErr,
			)
			continue
		}

		summary, parseErr := findSummaryJSON(logs)
		if parseErr != nil {
			slog.Warn("failed to parse handleSummary JSON from pod logs",
				"pod", pod.Name,
				"namespace", ns,
				"name", testRunName,
				"error", parseErr,
				"affectedMetrics", "http_req_failed,http_req_duration,http_reqs",
			)
			continue
		}
		if summary == nil {
			slog.Warn("no handleSummary JSON found in pod logs, ensure k6 script exports handleSummary()",
				"pod", pod.Name,
				"namespace", ns,
				"name", testRunName,
				"affectedMetrics", "http_req_failed,http_req_duration,http_reqs",
			)
			continue
		}

		m := extractMetricsFromSummary(summary)
		collected = append(collected, m)
	}

	if len(collected) == 0 {
		slog.Warn("no pods produced valid handleSummary data, all detailed metrics will be zero",
			"name", testRunName,
			"namespace", ns,
			"podCount", len(pods.Items),
			"affectedMetrics", "http_req_failed,http_req_duration,http_reqs",
		)
		return summaryMetrics{}, nil
	}

	result := aggregateMetrics(collected)

	slog.Debug("aggregated handleSummary metrics",
		"name", testRunName,
		"namespace", ns,
		"podCount", len(pods.Items),
		"podsWithSummary", len(collected),
		"httpReqFailed", result.HTTPReqFailed,
		"httpReqDurationP50", result.HTTPReqDuration.P50,
		"httpReqDurationP95", result.HTTPReqDuration.P95,
		"httpReqDurationP99", result.HTTPReqDuration.P99,
		"httpReqs", result.HTTPReqs,
	)

	return result, nil
}
