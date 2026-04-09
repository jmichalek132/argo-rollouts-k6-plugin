package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		TestID:   "42",
		APIToken: "test-token",
		StackID:  "12345",
	}
}

func strPtr(s string) *string {
	return &s
}

// --- TriggerRun tests ---

func TestTriggerRun_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud/v6/load_tests/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		// Verify auth headers
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "12345", r.Header.Get("X-Stack-Id"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      99,
			"test_id": 42,
			"status":  "created",
			"status_details": map[string]interface{}{
				"type":    "created",
				"entered": "2026-04-09T00:00:00Z",
			},
			"status_history": []map[string]interface{}{
				{"type": "created", "entered": "2026-04-09T00:00:00Z"},
			},
			"project_id":       1,
			"started_by":       nil,
			"created":          "2026-04-09T00:00:00Z",
			"ended":            nil,
			"note":             "",
			"retention_expiry": nil,
			"cost":             nil,
			"distribution":     []interface{}{},
			"result":           nil,
			"result_details":   map[string]interface{}{},
			"options":          map[string]interface{}{},
			"k6_dependencies":  map[string]string{},
			"k6_versions":      map[string]string{},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	runID, err := p.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "99", runID)
}

func TestTriggerRun_InvalidTestID(t *testing.T) {
	p := NewGrafanaCloudProvider()
	cfg := &provider.PluginConfig{
		TestID:   "not-a-number",
		APIToken: "test-token",
		StackID:  "12345",
	}
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid testId")
}

func TestTriggerRun_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud/v6/load_tests/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal server error"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
}

// --- GetRunResult tests ---

func testGetRunResultWithResponse(t *testing.T, responseBody map[string]interface{}) *provider.RunResult {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /cloud/v6/test_runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "12345", r.Header.Get("X-Stack-Id"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responseBody)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	result, err := p.GetRunResult(context.Background(), cfg, "99")
	require.NoError(t, err)
	return result
}

func fullTestRunResponse(statusType string, result *string) map[string]interface{} {
	resp := map[string]interface{}{
		"id":      99,
		"test_id": 42,
		"status":  statusType,
		"status_details": map[string]interface{}{
			"type":    statusType,
			"entered": "2026-04-09T00:00:00Z",
		},
		"status_history": []map[string]interface{}{
			{"type": statusType, "entered": "2026-04-09T00:00:00Z"},
		},
		"project_id":       1,
		"started_by":       nil,
		"created":          "2026-04-09T00:00:00Z",
		"ended":            nil,
		"note":             "",
		"retention_expiry": nil,
		"cost":             nil,
		"distribution":     []interface{}{},
		"result":           result,
		"result_details":   map[string]interface{}{},
		"options":          map[string]interface{}{},
		"k6_dependencies":  map[string]string{},
		"k6_versions":      map[string]string{},
	}
	return resp
}

func TestGetRunResult_Running(t *testing.T) {
	result := testGetRunResultWithResponse(t, fullTestRunResponse("running", nil))
	assert.Equal(t, provider.Running, result.State)
}

func TestGetRunResult_Passed(t *testing.T) {
	result := testGetRunResultWithResponse(t, fullTestRunResponse("completed", strPtr("passed")))
	assert.Equal(t, provider.Passed, result.State)
	assert.True(t, result.ThresholdsPassed)
}

func TestGetRunResult_Failed(t *testing.T) {
	result := testGetRunResultWithResponse(t, fullTestRunResponse("completed", strPtr("failed")))
	assert.Equal(t, provider.Failed, result.State)
	assert.False(t, result.ThresholdsPassed)
}

func TestGetRunResult_Errored(t *testing.T) {
	result := testGetRunResultWithResponse(t, fullTestRunResponse("completed", strPtr("error")))
	assert.Equal(t, provider.Errored, result.State)
}

func TestGetRunResult_Aborted(t *testing.T) {
	result := testGetRunResultWithResponse(t, fullTestRunResponse("aborted", nil))
	assert.Equal(t, provider.Aborted, result.State)
}

func TestGetRunResult_AllNonTerminalStatuses(t *testing.T) {
	nonTerminal := []string{"created", "queued", "initializing", "running", "processing_metrics"}
	for _, status := range nonTerminal {
		t.Run(status, func(t *testing.T) {
			result := testGetRunResultWithResponse(t, fullTestRunResponse(status, nil))
			assert.Equal(t, provider.Running, result.State)
		})
	}
}

func TestGetRunResult_BuildsTestRunURL(t *testing.T) {
	result := testGetRunResultWithResponse(t, fullTestRunResponse("running", nil))
	assert.NotEmpty(t, result.TestRunURL)
	assert.Contains(t, result.TestRunURL, "99")
}

// --- StopRun tests ---

func TestStopRun_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud/v6/test_runs/{id}/abort", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "12345", r.Header.Get("X-Stack-Id"))

		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	err := p.StopRun(context.Background(), cfg, "99")
	require.NoError(t, err)
}

func TestStopRun_InvalidRunID(t *testing.T) {
	p := NewGrafanaCloudProvider()
	cfg := defaultConfig()
	err := p.StopRun(context.Background(), cfg, "not-a-number")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid runId")
}

// --- Name test ---

func TestName(t *testing.T) {
	p := NewGrafanaCloudProvider()
	assert.Equal(t, "grafana-cloud-k6", p.Name())
}

// --- Auth verification tests ---

func TestAuth_BearerToken(t *testing.T) {
	var capturedAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud/v6/load_tests/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fullTestRunResponse("created", nil))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	_, _ = p.TriggerRun(context.Background(), cfg)
	assert.Equal(t, "Bearer test-token", capturedAuth)
}

func TestAuth_StackIdHeader(t *testing.T) {
	var capturedStackID string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud/v6/load_tests/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		capturedStackID = r.Header.Get("X-Stack-Id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fullTestRunResponse("created", nil))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()
	_, _ = p.TriggerRun(context.Background(), cfg)
	assert.Equal(t, "12345", capturedStackID)
}
