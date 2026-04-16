package operator

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock PodLogReader ---

type mockPodLogReader struct {
	logs map[string]string // key: "namespace/podName"
	err  error
}

func (m *mockPodLogReader) ReadLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	key := namespace + "/" + podName
	return m.logs[key], nil
}

// capturePodLogReader records the PodLogOptions passed to it.
type capturePodLogReader struct {
	capturedOpts *corev1.PodLogOptions
	logContent   string
}

func (c *capturePodLogReader) ReadLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	c.capturedOpts = opts
	return c.logContent, nil
}

// --- Test data ---

const validSummaryJSON = `{"metrics":{"http_req_duration":{"type":"trend","contains":"time","values":{"avg":234.5,"min":100.2,"med":210.3,"max":890.1,"p(90)":450.7,"p(95)":523.4,"p(99)":780.2}},"http_req_failed":{"type":"rate","contains":"default","values":{"rate":0.023,"passes":977,"fails":23}},"http_reqs":{"type":"counter","contains":"default","values":{"count":1000,"rate":33.33}}},"root_group":{"name":"","path":"","id":"","groups":[],"checks":[]}}`

// --- findSummaryJSON tests ---

func TestFindSummaryJSON_ValidJSON(t *testing.T) {
	// handleSummary JSON at end of mixed k6 output
	logs := "          /\\      |------| welcome to k6\n     /\\  / \\     |      |\n    / \\/   \\    |      |\n   /          \\   |      |\n  / __________ \\  |______|\n\nrunning (00m30s), 0/10 VUs\ndefault   [======>] 10/10 VUs  30s\n\n" + validSummaryJSON + "\n"

	summary, err := findSummaryJSON(logs)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.NotNil(t, summary.Metrics)
	assert.NotNil(t, summary.RootGroup)
	assert.Contains(t, summary.Metrics, "http_req_duration")
	assert.Contains(t, summary.Metrics, "http_req_failed")
	assert.Contains(t, summary.Metrics, "http_reqs")
}

func TestFindSummaryJSON_NoJSON(t *testing.T) {
	logs := "just some plain text log output\nno json here\n"

	summary, err := findSummaryJSON(logs)
	assert.NoError(t, err)
	assert.Nil(t, summary)
}

func TestFindSummaryJSON_MalformedJSON(t *testing.T) {
	logs := "some output\n{\"metrics\": {\"http_req_duration\": {invalid json}}\n"

	summary, err := findSummaryJSON(logs)
	assert.Error(t, err)
	assert.Nil(t, summary)
}

func TestFindSummaryJSON_MissingMetricsKey(t *testing.T) {
	// JSON without "metrics" key -- should be rejected
	logs := `{"root_group":{"name":"","path":""},"some_other_key":"value"}` + "\n"

	summary, err := findSummaryJSON(logs)
	assert.NoError(t, err)
	assert.Nil(t, summary)
}

func TestFindSummaryJSON_MissingRootGroupKey(t *testing.T) {
	// JSON with "metrics" but no "root_group" -- rejected per D-02 review fix
	logs := `{"metrics":{"http_reqs":{"type":"counter","values":{"count":100,"rate":10}}}}` + "\n"

	summary, err := findSummaryJSON(logs)
	assert.NoError(t, err)
	assert.Nil(t, summary)
}

func TestFindSummaryJSON_MultilinePrettyPrinted(t *testing.T) {
	logs := "k6 output here\n" + `{
  "metrics": {
    "http_req_duration": {
      "type": "trend",
      "contains": "time",
      "values": {
        "avg": 234.5,
        "min": 100.2,
        "med": 210.3,
        "max": 890.1,
        "p(90)": 450.7,
        "p(95)": 523.4,
        "p(99)": 780.2
      }
    },
    "http_req_failed": {
      "type": "rate",
      "contains": "default",
      "values": {
        "rate": 0.023,
        "passes": 977,
        "fails": 23
      }
    },
    "http_reqs": {
      "type": "counter",
      "contains": "default",
      "values": {
        "count": 1000,
        "rate": 33.33
      }
    }
  },
  "root_group": {
    "name": "",
    "path": "",
    "id": "",
    "groups": [],
    "checks": []
  }
}
`

	summary, err := findSummaryJSON(logs)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Contains(t, summary.Metrics, "http_req_duration")
	assert.NotNil(t, summary.RootGroup)
}

func TestFindSummaryJSON_TrailingNewlines(t *testing.T) {
	logs := validSummaryJSON + "\n\n\n"

	summary, err := findSummaryJSON(logs)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Contains(t, summary.Metrics, "http_reqs")
}

func TestFindSummaryJSON_ConsoleLogJSONRejected(t *testing.T) {
	// console.log JSON without "metrics"/"root_group" keys -- rejected
	logs := `{"level":"info","msg":"test started","timestamp":"2024-01-01T00:00:00Z"}` + "\n"

	summary, err := findSummaryJSON(logs)
	assert.NoError(t, err)
	assert.Nil(t, summary)
}

func TestFindSummaryJSON_MultipleJSONObjects(t *testing.T) {
	// console.log JSON BEFORE the handleSummary JSON -- should find the handleSummary one
	logs := `{"level":"info","msg":"test started"}` + "\n" +
		"running (00m30s), 10/10 VUs\n" +
		validSummaryJSON + "\n"

	summary, err := findSummaryJSON(logs)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Contains(t, summary.Metrics, "http_req_duration")
	assert.NotNil(t, summary.RootGroup)
}

func TestFindSummaryJSON_TruncatedJSON(t *testing.T) {
	// Opening brace but no closing brace
	logs := `{"metrics":{"http_req_duration":{"type":"trend"` + "\n"

	summary, err := findSummaryJSON(logs)
	assert.Error(t, err)
	assert.Nil(t, summary)
}

func TestFindSummaryJSON_EmptyLogs(t *testing.T) {
	summary, err := findSummaryJSON("")
	assert.NoError(t, err)
	assert.Nil(t, summary)
}

// --- extractMetricsFromSummary tests ---

func TestExtractMetrics_HTTPReqFailed(t *testing.T) {
	summary := &k6Summary{
		Metrics: map[string]k6Metric{
			"http_req_failed": {
				Type: "rate",
				Values: map[string]float64{
					"rate":   0.023,
					"passes": 977,
					"fails":  23,
				},
			},
			"http_reqs": {
				Type: "counter",
				Values: map[string]float64{
					"count": 1000,
					"rate":  33.33,
				},
			},
		},
	}

	m := extractMetricsFromSummary(summary)
	assert.InDelta(t, 0.023, m.HTTPReqFailed, 0.001)
}

func TestExtractMetrics_HTTPReqDuration(t *testing.T) {
	summary := &k6Summary{
		Metrics: map[string]k6Metric{
			"http_req_duration": {
				Type: "trend",
				Values: map[string]float64{
					"avg":    234.5,
					"min":    100.2,
					"med":    210.3,
					"max":    890.1,
					"p(90)":  450.7,
					"p(95)":  523.4,
					"p(99)":  780.2,
				},
			},
		},
	}

	m := extractMetricsFromSummary(summary)
	assert.InDelta(t, 210.3, m.HTTPReqDuration.P50, 0.001, "P50 from 'med' key")
	assert.InDelta(t, 523.4, m.HTTPReqDuration.P95, 0.001)
	assert.InDelta(t, 780.2, m.HTTPReqDuration.P99, 0.001)
}

func TestExtractMetrics_P50FallbackFromP50Key(t *testing.T) {
	// "med" is absent, use "p(50)" as fallback
	summary := &k6Summary{
		Metrics: map[string]k6Metric{
			"http_req_duration": {
				Type: "trend",
				Values: map[string]float64{
					"avg":   234.5,
					"p(50)": 215.0,
					"p(95)": 523.4,
					"p(99)": 780.2,
				},
			},
		},
	}

	m := extractMetricsFromSummary(summary)
	assert.InDelta(t, 215.0, m.HTTPReqDuration.P50, 0.001, "P50 from 'p(50)' fallback")
}

func TestExtractMetrics_P99MissingReturnsZero(t *testing.T) {
	// p(99) not in summaryTrendStats -- graceful per D-05
	summary := &k6Summary{
		Metrics: map[string]k6Metric{
			"http_req_duration": {
				Type: "trend",
				Values: map[string]float64{
					"avg":   234.5,
					"med":   210.3,
					"p(95)": 523.4,
					// no p(99) key
				},
			},
		},
	}

	m := extractMetricsFromSummary(summary)
	assert.InDelta(t, 0.0, m.HTTPReqDuration.P99, 0.001, "missing p(99) returns 0.0")
}

func TestExtractMetrics_HTTPReqs(t *testing.T) {
	summary := &k6Summary{
		Metrics: map[string]k6Metric{
			"http_reqs": {
				Type: "counter",
				Values: map[string]float64{
					"count": 1000,
					"rate":  33.33,
				},
			},
		},
	}

	m := extractMetricsFromSummary(summary)
	assert.InDelta(t, 33.33, m.HTTPReqs, 0.001)
}

func TestExtractMetrics_EmptyMetrics(t *testing.T) {
	summary := &k6Summary{
		Metrics: map[string]k6Metric{},
	}

	m := extractMetricsFromSummary(summary)
	assert.InDelta(t, 0.0, m.HTTPReqFailed, 0.001)
	assert.InDelta(t, 0.0, m.HTTPReqDuration.P50, 0.001)
	assert.InDelta(t, 0.0, m.HTTPReqDuration.P95, 0.001)
	assert.InDelta(t, 0.0, m.HTTPReqDuration.P99, 0.001)
	assert.InDelta(t, 0.0, m.HTTPReqs, 0.001)
}

// --- aggregateMetrics tests ---

func TestAggregateMetrics_SinglePod(t *testing.T) {
	metrics := []summaryMetrics{
		{
			HTTPReqFailed:   0.023,
			HTTPReqDuration: durationPercentiles(210.3, 523.4, 780.2),
			HTTPReqs:        33.33,
			httpReqsCount:   1000,
		},
	}

	result := aggregateMetrics(metrics)
	assert.InDelta(t, 0.023, result.HTTPReqFailed, 0.001)
	assert.InDelta(t, 210.3, result.HTTPReqDuration.P50, 0.001)
	assert.InDelta(t, 523.4, result.HTTPReqDuration.P95, 0.001)
	assert.InDelta(t, 780.2, result.HTTPReqDuration.P99, 0.001)
	assert.InDelta(t, 33.33, result.HTTPReqs, 0.001)
}

func TestAggregateMetrics_TwoPods_SumHTTPReqs(t *testing.T) {
	metrics := []summaryMetrics{
		{HTTPReqs: 30.0, httpReqsCount: 900},
		{HTTPReqs: 20.0, httpReqsCount: 600},
	}

	result := aggregateMetrics(metrics)
	assert.InDelta(t, 50.0, result.HTTPReqs, 0.001, "rates summed across pods")
}

func TestAggregateMetrics_TwoPods_WeightedHTTPReqFailed(t *testing.T) {
	metrics := []summaryMetrics{
		{HTTPReqFailed: 0.01, httpReqsCount: 1000, httpReqFailedCount: 10},
		{HTTPReqFailed: 0.05, httpReqsCount: 500, httpReqFailedCount: 25},
	}

	result := aggregateMetrics(metrics)
	// Weighted: (10 + 25) / (1000 + 500) = 35/1500 = 0.02333...
	assert.InDelta(t, 0.02333, result.HTTPReqFailed, 0.001)
}

func TestAggregateMetrics_TwoPods_WeightedP95(t *testing.T) {
	metrics := []summaryMetrics{
		{
			HTTPReqDuration: durationPercentiles(200, 500, 700),
			httpReqsCount:   1000,
		},
		{
			HTTPReqDuration: durationPercentiles(300, 600, 800),
			httpReqsCount:   500,
		},
	}

	result := aggregateMetrics(metrics)
	// Weighted P95: (500*1000 + 600*500) / 1500 = 800000/1500 = 533.33...
	assert.InDelta(t, 533.333, result.HTTPReqDuration.P95, 0.01)
}

func TestAggregateMetrics_EmptySlice(t *testing.T) {
	result := aggregateMetrics(nil)
	assert.InDelta(t, 0.0, result.HTTPReqFailed, 0.001)
	assert.InDelta(t, 0.0, result.HTTPReqDuration.P50, 0.001)
	assert.InDelta(t, 0.0, result.HTTPReqs, 0.001)
}

func TestAggregateMetrics_ZeroRequestCount(t *testing.T) {
	metrics := []summaryMetrics{
		{HTTPReqFailed: 0.0, httpReqsCount: 0},
		{HTTPReqFailed: 0.0, httpReqsCount: 0},
	}

	result := aggregateMetrics(metrics)
	assert.InDelta(t, 0.0, result.HTTPReqFailed, 0.001)
	assert.InDelta(t, 0.0, result.HTTPReqs, 0.001)
}

// --- readPodLogs tests ---

func TestReadPodLogs_SetsTailLinesAndLimitBytes(t *testing.T) {
	reader := &capturePodLogReader{logContent: "some logs"}

	_, err := readPodLogs(context.Background(), reader, "ns", "pod-0")
	require.NoError(t, err)
	require.NotNil(t, reader.capturedOpts)
	require.NotNil(t, reader.capturedOpts.TailLines)
	require.NotNil(t, reader.capturedOpts.LimitBytes)
	assert.Equal(t, int64(100), *reader.capturedOpts.TailLines)
	assert.Equal(t, int64(65536), *reader.capturedOpts.LimitBytes)
}

// --- parseSummaryFromPods tests ---

func TestParseSummaryFromPods_OnePodValid(t *testing.T) {
	pod := testRunnerPod("ns", "my-test", 0)
	client := k8sfake.NewSimpleClientset(pod)
	reader := &mockPodLogReader{
		logs: map[string]string{
			"ns/" + pod.Name: "k6 output\n" + validSummaryJSON + "\n",
		},
	}

	m, err := parseSummaryFromPods(context.Background(), reader, client, "ns", "my-test")
	require.NoError(t, err)
	assert.InDelta(t, 0.023, m.HTTPReqFailed, 0.001)
	assert.InDelta(t, 210.3, m.HTTPReqDuration.P50, 0.001)
	assert.InDelta(t, 523.4, m.HTTPReqDuration.P95, 0.001)
	assert.InDelta(t, 780.2, m.HTTPReqDuration.P99, 0.001)
	assert.InDelta(t, 33.33, m.HTTPReqs, 0.001)
}

func TestParseSummaryFromPods_NoJSON_GracefulDegradation(t *testing.T) {
	pod := testRunnerPod("ns", "my-test", 0)
	client := k8sfake.NewSimpleClientset(pod)
	reader := &mockPodLogReader{
		logs: map[string]string{
			"ns/" + pod.Name: "just plain k6 output, no handleSummary JSON\n",
		},
	}

	m, err := parseSummaryFromPods(context.Background(), reader, client, "ns", "my-test")
	require.NoError(t, err, "graceful degradation: no error when no JSON found")
	assert.InDelta(t, 0.0, m.HTTPReqFailed, 0.001)
	assert.InDelta(t, 0.0, m.HTTPReqs, 0.001)
}

func TestParseSummaryFromPods_LogReadError_GracefulDegradation(t *testing.T) {
	pod := testRunnerPod("ns", "my-test", 0)
	client := k8sfake.NewSimpleClientset(pod)
	reader := &mockPodLogReader{
		err: errors.New("connection refused"),
	}

	m, err := parseSummaryFromPods(context.Background(), reader, client, "ns", "my-test")
	require.NoError(t, err, "graceful degradation: no error when log read fails")
	assert.InDelta(t, 0.0, m.HTTPReqFailed, 0.001)
}

func TestParseSummaryFromPods_TwoPods_Aggregated(t *testing.T) {
	pod1 := testRunnerPod("ns", "my-test", 0)
	pod1.Name = "k6-my-test-runner-0"
	pod2 := testRunnerPod("ns", "my-test", 0)
	pod2.Name = "k6-my-test-runner-1"

	client := k8sfake.NewSimpleClientset(pod1, pod2)

	// Pod 1: 1000 reqs at 33.33 rps, 2.3% failed, P95=523.4
	pod1JSON := `{"metrics":{"http_req_duration":{"type":"trend","contains":"time","values":{"med":210.3,"p(95)":523.4,"p(99)":780.2}},"http_req_failed":{"type":"rate","contains":"default","values":{"rate":0.023,"passes":977,"fails":23}},"http_reqs":{"type":"counter","contains":"default","values":{"count":1000,"rate":33.33}}},"root_group":{"name":"","path":""}}`
	// Pod 2: 500 reqs at 16.67 rps, 5% failed, P95=600
	pod2JSON := `{"metrics":{"http_req_duration":{"type":"trend","contains":"time","values":{"med":300.0,"p(95)":600.0,"p(99)":850.0}},"http_req_failed":{"type":"rate","contains":"default","values":{"rate":0.05,"passes":475,"fails":25}},"http_reqs":{"type":"counter","contains":"default","values":{"count":500,"rate":16.67}}},"root_group":{"name":"","path":""}}`

	reader := &mockPodLogReader{
		logs: map[string]string{
			"ns/k6-my-test-runner-0": pod1JSON + "\n",
			"ns/k6-my-test-runner-1": pod2JSON + "\n",
		},
	}

	m, err := parseSummaryFromPods(context.Background(), reader, client, "ns", "my-test")
	require.NoError(t, err)

	// HTTPReqs: sum of rates = 33.33 + 16.67 = 50.0
	assert.InDelta(t, 50.0, m.HTTPReqs, 0.01)

	// HTTPReqFailed: (23 + 25) / (1000 + 500) = 48/1500 = 0.032
	assert.InDelta(t, 0.032, m.HTTPReqFailed, 0.001)

	// P95 weighted: (523.4*1000 + 600*500) / 1500 = 823400/1500 = 548.93
	assert.InDelta(t, 548.93, m.HTTPReqDuration.P95, 0.1)
}

func TestParseSummaryFromPods_OneValidOneFailed(t *testing.T) {
	pod1 := testRunnerPod("ns", "my-test", 0)
	pod1.Name = "k6-my-test-runner-0"
	pod2 := testRunnerPod("ns", "my-test", 0)
	pod2.Name = "k6-my-test-runner-1"

	client := k8sfake.NewSimpleClientset(pod1, pod2)
	reader := &mockPodLogReader{
		logs: map[string]string{
			"ns/k6-my-test-runner-0": validSummaryJSON + "\n",
			"ns/k6-my-test-runner-1": "no json here\n", // invalid -- no handleSummary
		},
	}

	m, err := parseSummaryFromPods(context.Background(), reader, client, "ns", "my-test")
	require.NoError(t, err)
	// Only pod1's metrics should be used
	assert.InDelta(t, 0.023, m.HTTPReqFailed, 0.001)
	assert.InDelta(t, 33.33, m.HTTPReqs, 0.001)
}

// --- helpers ---

func durationPercentiles(p50, p95, p99 float64) provider.Percentiles {
	return provider.Percentiles{P50: p50, P95: p95, P99: p99}
}
