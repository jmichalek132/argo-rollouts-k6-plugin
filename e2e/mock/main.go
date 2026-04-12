package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

// Static routing: testId → runId.
// testIds must be valid integers (the k6 Cloud API uses int32 test IDs).
var testToRun = map[string]int{
	"100": 1001, // metric-pass
	"101": 1002, // metric-fail
	"200": 2001, // step-pass
	"201": 2002, // step-fail
}

type runResponse struct {
	statusType string
	result     *string
}

type runConfig struct {
	responses []runResponse
	counter   atomic.Int64
}

var (
	strPassed = strPtr("passed")
	strFailed = strPtr("failed")
)

// runConfigs maps runId → sequential response list.
// atomic.Int64 must not be copied — use pointer map values only.
var runConfigs = map[int]*runConfig{
	1001: {responses: []runResponse{
		{statusType: "running"},
		{statusType: "completed", result: strPassed},
	}},
	1002: {responses: []runResponse{
		{statusType: "running"},
		{statusType: "completed", result: strFailed},
	}},
	2001: {responses: []runResponse{
		{statusType: "running"},
		{statusType: "running"},
		{statusType: "completed", result: strPassed},
	}},
	2002: {responses: []runResponse{
		{statusType: "running"},
		{statusType: "completed", result: strFailed},
	}},
}

type aggregateValues struct {
	httpReqFailed   float64
	httpReqDuration float64
	httpReqs        float64
}

var runAggregates = map[int]*aggregateValues{
	1001: {httpReqFailed: 0.001, httpReqDuration: 150.0, httpReqs: 500.0},
	1002: {httpReqFailed: 0.15, httpReqDuration: 2500.0, httpReqs: 50.0},
	2001: {httpReqFailed: 0.0, httpReqDuration: 100.0, httpReqs: 1000.0},
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	log.Println("mock-k6 server listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	log.Printf("%s %s", r.Method, path)

	// POST /cloud/v6/load_tests/{id}/start  (TriggerRun)
	if r.Method == http.MethodPost && strings.Contains(path, "/load_tests/") && strings.HasSuffix(path, "/start") {
		handleStartTestRun(w, path)
		return
	}

	// POST /cloud/v6/test_runs/{id}/abort  (StopRun)
	if r.Method == http.MethodPost && strings.Contains(path, "/test_runs/") && strings.HasSuffix(path, "/abort") {
		w.WriteHeader(http.StatusOK)
		return
	}

	// GET /cloud/v5/test_runs/{id}/query_aggregate_k6(...)  (aggregate metrics, hand-rolled)
	// Must be checked before the generic test_runs GET below.
	if r.Method == http.MethodGet && strings.Contains(path, "/query_aggregate_k6") {
		fullURL := path
		if q := r.URL.RawQuery; q != "" {
			fullURL = path + "?" + q
		}
		handleAggregateQuery(w, fullURL, path)
		return
	}

	// GET /cloud/v6/test_runs/{id}  (GetRunResult)
	if r.Method == http.MethodGet && strings.Contains(path, "/test_runs/") {
		handleGetTestRun(w, path)
		return
	}

	log.Printf("404: %s %s", r.Method, path)
	http.NotFound(w, r)
}

func handleStartTestRun(w http.ResponseWriter, path string) {
	testID := segmentAfter(path, "load_tests")
	runID, ok := testToRun[testID]
	if !ok {
		http.Error(w, fmt.Sprintf("test %q not configured", testID), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(testRunBody(runID, "created", nil))
}

func handleGetTestRun(w http.ResponseWriter, path string) {
	var runID int
	fmt.Sscanf(segmentAfter(path, "test_runs"), "%d", &runID)

	cfg, ok := runConfigs[runID]
	if !ok {
		http.Error(w, fmt.Sprintf("run %d not configured", runID), http.StatusNotFound)
		return
	}

	idx := int(cfg.counter.Add(1)) - 1
	if idx >= len(cfg.responses) {
		idx = len(cfg.responses) - 1
	}
	resp := cfg.responses[idx]

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(testRunBody(runID, resp.statusType, resp.result))
}

// testRunBody builds a complete TestRunApiModel response matching the k6 OpenAPI v6 schema.
// The k6 OpenAPI client requires several fields to unmarshal without error.
func testRunBody(id int, statusType string, result *string) map[string]interface{} {
	var resultVal interface{} // nil encodes as JSON null
	if result != nil {
		resultVal = *result
	}
	return map[string]interface{}{
		"id":      id,
		"test_id": 0,
		"status":  statusType,
		"status_details": map[string]interface{}{
			"type":    statusType,
			"entered": "2026-01-01T00:00:00Z",
		},
		"status_history": []map[string]interface{}{
			{"type": statusType, "entered": "2026-01-01T00:00:00Z"},
		},
		"project_id":       1,
		"started_by":       nil,
		"created":          "2026-01-01T00:00:00Z",
		"ended":            nil,
		"note":             "",
		"retention_expiry": nil,
		"cost":             nil,
		"distribution":     []interface{}{},
		"result":           resultVal,
		"result_details":   map[string]interface{}{},
		"options":          map[string]interface{}{},
		"k6_dependencies":  map[string]string{},
		"k6_versions":      map[string]string{},
	}
}

func handleAggregateQuery(w http.ResponseWriter, fullURL, path string) {
	var runID int
	fmt.Sscanf(segmentAfter(path, "test_runs"), "%d", &runID)

	metrics, ok := runAggregates[runID]
	if !ok {
		writeEmptyAggregateResult(w)
		return
	}

	var value float64
	switch {
	case strings.Contains(fullURL, "http_req_failed"):
		value = metrics.httpReqFailed
	case strings.Contains(fullURL, "http_req_duration"):
		value = metrics.httpReqDuration
	case strings.Contains(fullURL, "http_reqs"):
		value = metrics.httpReqs
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

func writeEmptyAggregateResult(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "vector",
			"result":     []interface{}{},
		},
	})
}

// segmentAfter extracts the path segment immediately following key.
func segmentAfter(path, key string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, p := range parts {
		if p == key && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func strPtr(s string) *string { return &s }
