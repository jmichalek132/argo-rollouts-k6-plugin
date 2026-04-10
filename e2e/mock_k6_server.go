//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// testRunResponse represents a single mock response for GET /loadtests/v2/test_runs/{id}.
type testRunResponse struct {
	StatusText string  // "running", "finished", etc.
	ResultText *string // "passed", "failed", or nil
}

// aggregateMetrics holds configurable aggregate metric values for the v5 endpoint.
type aggregateMetrics struct {
	HTTPReqFailed   float64
	HTTPReqDuration float64 // p95 value
	HTTPReqs        float64
}

// MockK6Server simulates the Grafana Cloud k6 API for e2e testing.
// Each test programs its own response sequences via the On* methods.
type MockK6Server struct {
	mu       sync.Mutex
	server   *http.Server
	listener net.Listener

	// triggerRunIDs maps testId -> runId to return from start-testrun.
	triggerRunIDs map[string]string

	// runResponses maps runId -> sequence of responses for GET test_runs/{id}.
	runResponses map[string][]testRunResponse

	// runCallCount tracks how many times GET test_runs/{id} has been called per runId.
	runCallCount map[string]int

	// aggregates maps runId -> aggregate metrics for the v5 endpoint.
	aggregates map[string]*aggregateMetrics
}

// NewMockK6Server creates and starts a new mock k6 API server.
// It listens on 0.0.0.0 (all interfaces) so it is reachable from inside a kind cluster
// via Docker host-gateway networking.
func NewMockK6Server() (*MockK6Server, error) {
	m := &MockK6Server{
		triggerRunIDs: make(map[string]string),
		runResponses:  make(map[string][]testRunResponse),
		runCallCount:  make(map[string]int),
		aggregates:    make(map[string]*aggregateMetrics),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", m.handler)

	// Listen on all interfaces with OS-assigned port.
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("mock server listen: %w", err)
	}
	m.listener = listener
	m.server = &http.Server{Handler: mux}

	go func() {
		_ = m.server.Serve(listener)
	}()

	return m, nil
}

// URL returns the base URL of the mock server (e.g., "http://0.0.0.0:12345").
func (m *MockK6Server) URL() string {
	return fmt.Sprintf("http://%s", m.listener.Addr().String())
}

// Port returns the port the mock server is listening on.
func (m *MockK6Server) Port() int {
	return m.listener.Addr().(*net.TCPAddr).Port
}

// Close shuts down the mock server.
func (m *MockK6Server) Close() {
	_ = m.server.Close()
}

// OnTriggerRun programs the run ID returned when test {testId} is triggered.
func (m *MockK6Server) OnTriggerRun(testID, runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggerRunIDs[testID] = runID
}

// OnGetRunResult programs a sequence of responses for GET test_runs/{runId}.
// The mock returns responses in order; the last response repeats forever.
func (m *MockK6Server) OnGetRunResult(runID string, responses ...testRunResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runResponses[runID] = responses
	m.runCallCount[runID] = 0
}

// OnAggregateMetrics programs the aggregate metrics returned for the v5 endpoint.
func (m *MockK6Server) OnAggregateMetrics(runID string, metrics *aggregateMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aggregates[runID] = metrics
}

func (m *MockK6Server) handler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// POST /loadtests/v2/tests/{id}/start-testrun
	if r.Method == http.MethodPost && strings.Contains(path, "/start-testrun") {
		m.handleStartTestRun(w, r, path)
		return
	}

	// POST /loadtests/v2/test_runs/{id}/stop
	if r.Method == http.MethodPost && strings.Contains(path, "/stop") {
		w.WriteHeader(http.StatusOK)
		return
	}

	// GET /cloud/v5/test_runs/{id}/query_aggregate_k6
	if r.Method == http.MethodGet && strings.Contains(path, "/query_aggregate_k6") {
		m.handleAggregateQuery(w, r, path)
		return
	}

	// GET /loadtests/v2/test_runs/{id}
	if r.Method == http.MethodGet && strings.Contains(path, "/loadtests/v2/test_runs/") {
		m.handleGetTestRun(w, r, path)
		return
	}

	http.NotFound(w, r)
}

func (m *MockK6Server) handleStartTestRun(w http.ResponseWriter, _ *http.Request, path string) {
	// Extract test ID from path: /loadtests/v2/tests/{id}/start-testrun
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var testID string
	for i, p := range parts {
		if p == "tests" && i+1 < len(parts) {
			testID = parts[i+1]
			break
		}
	}

	m.mu.Lock()
	runID, ok := m.triggerRunIDs[testID]
	m.mu.Unlock()

	if !ok {
		http.Error(w, fmt.Sprintf("test %s not configured in mock", testID), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id": mustAtoi(runID),
	})
}

func (m *MockK6Server) handleGetTestRun(w http.ResponseWriter, _ *http.Request, path string) {
	// Extract run ID from path: /loadtests/v2/test_runs/{id}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var runID string
	for i, p := range parts {
		if p == "test_runs" && i+1 < len(parts) {
			runID = parts[i+1]
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	responses, ok := m.runResponses[runID]
	if !ok {
		http.Error(w, fmt.Sprintf("run %s not configured in mock", runID), http.StatusNotFound)
		return
	}

	idx := m.runCallCount[runID]
	if idx >= len(responses) {
		idx = len(responses) - 1 // repeat last response
	}
	m.runCallCount[runID]++

	resp := responses[idx]

	// Build a response matching the k6 OpenAPI client expectations.
	// The client expects status_details.type for status and result for result.
	body := map[string]interface{}{
		"id": mustAtoi(runID),
		"status_details": map[string]interface{}{
			"type": statusTextToType(resp.StatusText),
		},
		"url": fmt.Sprintf("https://app.k6.io/runs/%s", runID),
	}
	if resp.ResultText != nil {
		body["result"] = *resp.ResultText
	} else {
		body["result"] = nil
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (m *MockK6Server) handleAggregateQuery(w http.ResponseWriter, _ *http.Request, path string) {
	// Extract run ID from path: /cloud/v5/test_runs/{id}/query_aggregate_k6(...)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var runID string
	for i, p := range parts {
		if p == "test_runs" && i+1 < len(parts) {
			runID = parts[i+1]
			break
		}
	}

	m.mu.Lock()
	metrics, ok := m.aggregates[runID]
	m.mu.Unlock()

	if !ok {
		// Return empty result (graceful degradation).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result":     []interface{}{},
			},
		})
		return
	}

	// Determine which metric was requested from the path.
	var value float64
	if strings.Contains(path, "http_req_failed") {
		value = metrics.HTTPReqFailed
	} else if strings.Contains(path, "http_req_duration") {
		value = metrics.HTTPReqDuration
	} else if strings.Contains(path, "http_reqs") {
		value = metrics.HTTPReqs
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "vector",
			"result": []interface{}{
				map[string]interface{}{
					"metric": map[string]string{},
					"values": []interface{}{
						[]interface{}{1234567890.0, fmt.Sprintf("%g", value)},
					},
				},
			},
		},
	})
}

// statusTextToType maps human-readable status to the v6 API status_details.type value.
func statusTextToType(status string) string {
	switch strings.ToLower(status) {
	case "running":
		return "running"
	case "finished", "completed":
		return "completed"
	case "aborted":
		return "aborted"
	default:
		return status
	}
}

// mustAtoi converts a string to an integer, returning 0 on failure.
func mustAtoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}
