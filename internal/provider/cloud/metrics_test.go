package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseAggregateValue tests ---

func TestParseAggregateValue_ValidFloat(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"__name__": "http_req_duration"},
					"values": [[1684950639, 14207.5]]
				}
			]
		}
	}`)
	val, err := parseAggregateValue(body)
	require.NoError(t, err)
	assert.InDelta(t, 14207.5, val, 0.001)
}

func TestParseAggregateValue_StringValue(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"__name__": "http_req_failed"},
					"values": [[1684950639, "0.023"]]
				}
			]
		}
	}`)
	val, err := parseAggregateValue(body)
	require.NoError(t, err)
	assert.InDelta(t, 0.023, val, 0.0001)
}

func TestParseAggregateValue_EmptyResult(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": []
		}
	}`)
	val, err := parseAggregateValue(body)
	require.NoError(t, err)
	assert.Equal(t, 0.0, val)
}

func TestParseAggregateValue_ErrorStatus(t *testing.T) {
	body := []byte(`{
		"status": "error",
		"data": {
			"resultType": "vector",
			"result": []
		}
	}`)
	_, err := parseAggregateValue(body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=error")
}

func TestParseAggregateValue_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	_, err := parseAggregateValue(body)
	require.Error(t, err)
}

func TestParseAggregateValue_UnexpectedValueType(t *testing.T) {
	// A boolean value in the values array should return an error, not 0.0.
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"__name__": "http_req_duration"},
					"values": [[1684950639, true]]
				}
			]
		}
	}`)
	_, err := parseAggregateValue(body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected value type")
}

func TestParseAggregateValue_EmptyValues(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"__name__": "http_reqs"},
					"values": []
				}
			]
		}
	}`)
	val, err := parseAggregateValue(body)
	require.NoError(t, err)
	assert.Equal(t, 0.0, val)
}

// --- QueryAggregateMetric tests ---

func TestQueryAggregateMetric_URLPath(t *testing.T) {
	var capturedPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [{"metric": {}, "values": [[0, 42.5]]}]
			}
		}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	_, err := p.QueryAggregateMetric(context.Background(), cfg, "99", AggregateMetricQuery{
		MetricName: "http_req_duration",
		QueryFunc:  "histogram_quantile(0.95)",
	})
	require.NoError(t, err)
	assert.Equal(t, "/cloud/v5/test_runs/99/query_aggregate_k6(metric='http_req_duration',query='histogram_quantile(0.95)')", capturedPath)
}

func TestQueryAggregateMetric_AuthHeaders(t *testing.T) {
	var capturedAuth, capturedStackID string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedStackID = r.Header.Get("X-Stack-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {"resultType": "vector", "result": [{"metric": {}, "values": [[0, 1.0]]}]}
		}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	_, err := p.QueryAggregateMetric(context.Background(), cfg, "99", AggregateMetricQuery{
		MetricName: "http_req_failed",
		QueryFunc:  "rate",
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer test-token", capturedAuth)
	assert.Equal(t, "12345", capturedStackID)
}

func TestQueryAggregateMetric_Non200Status(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal error`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	_, err := p.QueryAggregateMetric(context.Background(), cfg, "99", AggregateMetricQuery{
		MetricName: "http_reqs",
		QueryFunc:  "rate",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestQueryAggregateMetric_ParsesResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [{"metric": {"__name__": "http_reqs"}, "values": [[1684950639, 142.3]]}]
			}
		}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	val, err := p.QueryAggregateMetric(context.Background(), cfg, "99", AggregateMetricQuery{
		MetricName: "http_reqs",
		QueryFunc:  "rate",
	})
	require.NoError(t, err)
	assert.InDelta(t, 142.3, val, 0.001)
}

// --- GetRunResult v5 integration tests ---

func TestGetRunResult_TerminalRunPopulatesAggregateMetrics(t *testing.T) {
	mux := http.NewServeMux()

	// v6 test run endpoint -- completed + passed
	mux.HandleFunc("GET /cloud/v6/test_runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := fullTestRunResponse("completed", strPtr("passed"))
		_ = json.NewEncoder(w).Encode(resp)
	})

	// v5 aggregate endpoints
	mux.HandleFunc("/cloud/v5/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		w.Header().Set("Content-Type", "application/json")

		var value float64
		switch {
		case containsMetric(path, "http_req_failed"):
			value = 0.023
		case containsMetric(path, "http_req_duration") && containsQuery(path, "0.50"):
			value = 150.0
		case containsMetric(path, "http_req_duration") && containsQuery(path, "0.95"):
			value = 234.5
		case containsMetric(path, "http_req_duration") && containsQuery(path, "0.99"):
			value = 450.0
		case containsMetric(path, "http_reqs"):
			value = 142.3
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}

		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [{"metric": {}, "values": [[0, ` + formatFloat(value) + `]]}]
			}
		}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	result, err := p.GetRunResult(context.Background(), cfg, "99")
	require.NoError(t, err)
	assert.Equal(t, provider.Passed, result.State)
	assert.InDelta(t, 0.023, result.HTTPReqFailed, 0.001)
	assert.InDelta(t, 150.0, result.HTTPReqDuration.P50, 0.1)
	assert.InDelta(t, 234.5, result.HTTPReqDuration.P95, 0.1)
	assert.InDelta(t, 450.0, result.HTTPReqDuration.P99, 0.1)
	assert.InDelta(t, 142.3, result.HTTPReqs, 0.1)
}

func TestGetRunResult_RunningSkipsV5(t *testing.T) {
	var v5Called bool
	mux := http.NewServeMux()

	mux.HandleFunc("GET /cloud/v6/test_runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := fullTestRunResponse("running", nil)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/cloud/v5/", func(w http.ResponseWriter, r *http.Request) {
		v5Called = true
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	result, err := p.GetRunResult(context.Background(), cfg, "99")
	require.NoError(t, err)
	assert.Equal(t, provider.Running, result.State)
	assert.False(t, v5Called, "v5 API should not be called for running tests")
	assert.Equal(t, 0.0, result.HTTPReqFailed)
	assert.Equal(t, 0.0, result.HTTPReqDuration.P50)
	assert.Equal(t, 0.0, result.HTTPReqs)
}

// helpers for path matching in tests
func containsMetric(path, metric string) bool {
	return len(path) > 0 && containsString(path, "metric='"+metric+"'")
}

func containsQuery(path, query string) bool {
	return containsString(path, query)
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// --- mapToRunState tests ---

func TestMapToRunState_UnknownStatus(t *testing.T) {
	// An unknown status type must return Errored (defensive default).
	state := mapToRunState("paused", nil)
	assert.Equal(t, provider.Errored, state)
}

func TestMapToRunState_KnownStatuses(t *testing.T) {
	passed := "passed"
	failed := "failed"
	errored := "error"

	tests := []struct {
		statusType string
		result     *string
		want       provider.RunState
	}{
		{"created", nil, provider.Running},
		{"queued", nil, provider.Running},
		{"initializing", nil, provider.Running},
		{"running", nil, provider.Running},
		{"processing_metrics", nil, provider.Running},
		{"completed", &passed, provider.Passed},
		{"completed", &failed, provider.Failed},
		{"completed", &errored, provider.Errored},
		{"aborted", nil, provider.Aborted},
	}
	for _, tc := range tests {
		t.Run(tc.statusType, func(t *testing.T) {
			assert.Equal(t, tc.want, mapToRunState(tc.statusType, tc.result))
		})
	}
}
