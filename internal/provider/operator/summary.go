package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

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

// findSummaryJSON scans log output to find a handleSummary JSON object.
// RED PHASE STUB -- returns nil, nil always.
func findSummaryJSON(logs string) (*k6Summary, error) {
	return nil, nil
}

// extractMetricsFromSummary extracts metric values from a parsed k6Summary.
// RED PHASE STUB -- returns zero-value summaryMetrics.
func extractMetricsFromSummary(summary *k6Summary) summaryMetrics {
	return summaryMetrics{}
}

// aggregateMetrics combines per-pod summaryMetrics into a single result.
// RED PHASE STUB -- returns zero-value summaryMetrics.
func aggregateMetrics(metrics []summaryMetrics) summaryMetrics {
	return summaryMetrics{}
}

// readPodLogs reads logs from a single pod with bounded options.
// RED PHASE STUB -- calls reader directly without setting options.
func readPodLogs(ctx context.Context, reader PodLogReader, namespace, podName string) (string, error) {
	return reader.ReadLogs(ctx, namespace, podName, &corev1.PodLogOptions{})
}

// parseSummaryFromPods reads logs from all runner pods and extracts/aggregates metrics.
// RED PHASE STUB -- returns zero-value summaryMetrics.
func parseSummaryFromPods(ctx context.Context, logReader PodLogReader, client kubernetes.Interface, ns, testRunName string) (summaryMetrics, error) {
	return summaryMetrics{}, nil
}

// Suppress unused import warnings for RED phase.
var (
	_ = slog.Debug
	_ = metav1.ListOptions{}
)
