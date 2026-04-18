# Phase 9: Metric Integration - Pattern Map

**Mapped:** 2026-04-16
**Files analyzed:** 3 (1 new, 2 modified)
**Analogs found:** 3 / 3

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/provider/operator/summary.go` | service | transform | `internal/provider/operator/exitcode.go` | exact |
| `internal/provider/operator/summary_test.go` | test | transform | `internal/provider/operator/exitcode_test.go` | exact |
| `internal/provider/operator/operator.go` | controller | request-response | `internal/provider/cloud/cloud.go` (GetRunResult + populateAggregateMetrics) | exact |

## Pattern Assignments

### `internal/provider/operator/summary.go` (NEW -- service, transform)

**Analog:** `internal/provider/operator/exitcode.go`

This is the primary new file. It parses handleSummary JSON from runner pod logs and populates RunResult metric fields. Follows the same separation-of-concerns pattern as `exitcode.go`: a standalone file in the operator package with pure functions called from `operator.go`.

**Imports pattern** (exitcode.go lines 1-10):
```go
package operator

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)
```

New file will additionally need:
- `"encoding/json"` -- parsing handleSummary JSON
- `"io"` -- reading pod log stream
- `"strings"` -- scanning log output for JSON
- `corev1 "k8s.io/api/core/v1"` -- PodLogOptions struct
- Remove `metav1` if not needed (pod listing uses it, but log reading uses PodLogOptions from corev1)

**Core pattern: package-level function with client + context** (exitcode.go lines 75-86):
```go
// checkRunnerExitCodes inspects runner pod exit codes for a given TestRun.
func checkRunnerExitCodes(ctx context.Context, client kubernetes.Interface, ns, testRunName string) (provider.RunState, error) {
	selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return provider.Errored, fmt.Errorf("list runner pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return provider.Errored, fmt.Errorf("no runner pods found for TestRun %s", testRunName)
	}
```

New `parseSummaryFromPods` function should follow this exact signature pattern:
- Accept `ctx context.Context, logReader PodLogReader, client kubernetes.Interface, ns, testRunName string`
- Return a struct with metric fields (not `provider.RunState`) and `error`
- Reuse the same label selector for pod discovery
- Same guard for zero pods

**Pod discovery pattern -- label selector** (exitcode.go line 76):
```go
selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)
```

Reuse this exact selector. The pod discovery code can be shared or duplicated (both files are in the same package).

**Worst-case aggregation pattern across pods** (exitcode.go lines 87-118):
```go
worst := provider.Passed
for _, pod := range pods.Items {
	// Guard: pod exists but containers have not started yet
	if len(pod.Status.ContainerStatuses) == 0 {
		return provider.Running, nil
	}
	for _, cs := range pod.Status.ContainerStatuses {
		// ...check each container, apply precedence...
		state := exitCodeToRunState(cs.State.Terminated.ExitCode)
		if state == provider.Errored {
			return provider.Errored, nil
		}
		if state == provider.Failed {
			worst = provider.Failed
		}
	}
}
```

For summary parsing, iterate pods similarly but collect per-pod summaries into a slice, then aggregate.

**Error handling pattern** (exitcode.go lines 81-85):
```go
if err != nil {
	return provider.Errored, fmt.Errorf("list runner pods: %w", err)
}
if len(pods.Items) == 0 {
	return provider.Errored, fmt.Errorf("no runner pods found for TestRun %s", testRunName)
}
```

Wrap all errors with `fmt.Errorf` context. Return zero-value struct on error (caller handles graceful degradation per D-05).

**Logging pattern** (exitcode.go lines 98-104, lines 120-126):
```go
slog.Warn("runner container restarted unexpectedly",
	"pod", pod.Name,
	"container", cs.Name,
	"restartCount", cs.RestartCount,
	"testRunName", testRunName,
)

slog.Debug("checked runner pod exit codes",
	"testRunName", testRunName,
	"namespace", ns,
	"podCount", len(pods.Items),
	"result", worst,
)
```

Follow same slog pattern: Warn for degraded paths, Debug for normal flow. Include `testRunName`, `namespace`, `pod` name as structured fields.

---

### `internal/provider/operator/summary_test.go` (NEW -- test, transform)

**Analog:** `internal/provider/operator/exitcode_test.go`

**Imports pattern** (exitcode_test.go lines 1-15):
```go
package operator

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

Note: summary_test.go will NOT use `k8s.io/client-go/kubernetes/fake` for GetLogs testing (Pitfall 3). Instead, it will use a mock `PodLogReader` interface. The fake clientset is still needed for pod listing in integration-level tests.

**Test helper pattern** (exitcode_test.go lines 67-91):
```go
func testRunnerPod(ns, testRunName string, exitCode int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("k6-%s-runner-0", testRunName),
			Namespace: ns,
			Labels: map[string]string{
				"app":    "k6",
				"k6_cr":  testRunName,
				"runner": "true",
			},
		},
		Status: corev1.PodStatus{...},
	}
}
```

New test file should define a mock PodLogReader:
```go
type mockPodLogReader struct {
	logs map[string]string // key: "namespace/podName" -> log content
	err  error
}

func (m *mockPodLogReader) ReadLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	key := namespace + "/" + podName
	return m.logs[key], nil
}
```

**Table-driven test pattern** (exitcode_test.go lines 19-37):
```go
func TestExitCodeToRunState(t *testing.T) {
	tests := []struct {
		name     string
		code     int32
		expected provider.RunState
	}{
		{"exit 0 -> Passed", 0, provider.Passed},
		{"exit 99 -> Failed (thresholds breached)", 99, provider.Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, exitCodeToRunState(tt.code))
		})
	}
}
```

Use table-driven tests for:
- `findSummaryJSON` with various log outputs (valid JSON, no JSON, malformed JSON, mixed output)
- `extractMetricsFromSummary` with various metric types present/absent
- `aggregateMetrics` with single pod and multiple pod scenarios
- Key mapping edge cases (med vs p(50), missing p(99))

**Assertion pattern** (exitcode_test.go lines 152-162):
```go
state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
require.NoError(t, err)
assert.Equal(t, provider.Passed, state)
```

Use `require.NoError` for fatal errors, `assert.Equal` for value checks. For float comparisons (metric values), use `assert.InDelta` as in `metrics_test.go` line 273:
```go
assert.InDelta(t, 0.023, result.HTTPReqFailed, 0.001)
```

**Reuse existing test helpers from exitcode_test.go** (same package):
- `testRunnerPod(ns, testRunName, exitCode)` -- for pods with terminated containers
- `testRunningPod(ns, testRunName)` -- for pods still running
- These are already exported within the package (same `operator` package)

---

### `internal/provider/operator/operator.go` (MODIFIED -- controller, request-response)

**Analog:** `internal/provider/cloud/cloud.go` lines 131-143 (GetRunResult calling populateAggregateMetrics)

**Integration point pattern** (cloud.go lines 131-143):
```go
runResult := &provider.RunResult{
	State:            state,
	TestRunURL:       fmt.Sprintf("https://app.k6.io/runs/%s", runID),
	ThresholdsPassed: isThresholdPassed(result),
}

// Populate aggregate metrics from v5 API for terminal Passed/Failed runs.
if state == provider.Passed || state == provider.Failed {
	p.populateAggregateMetrics(ctx, cfg, runID, runResult)
}

return runResult, nil
```

Apply this same pattern to `operator.go` `GetRunResult` at lines 304-324. After `checkRunnerExitCodes` determines the exit state, call `parseSummaryFromPods` for terminal (Passed/Failed) states and populate result fields:

**Current code to modify** (operator.go lines 304-325):
```go
// For terminal stages, check runner pod exit codes (per D-05, issue #577 workaround).
if stage == "finished" {
	exitState, exitErr := checkRunnerExitCodes(ctx, client, ns, name)
	if exitErr != nil {
		slog.Warn("failed to check runner exit codes, treating as error",
			"name", name,
			"namespace", ns,
			"error", exitErr,
			"provider", p.Name(),
		)
		return &provider.RunResult{
			State: provider.Errored,
		}, nil
	}
	state = exitState
}

result := &provider.RunResult{
	State:            state,
	ThresholdsPassed: state == provider.Passed,
}
return result, nil
```

**Graceful degradation pattern from cloud provider** (cloud.go lines 149-188 -- populateAggregateMetrics):
```go
// Failures are logged at Warn level but do not cause GetRunResult to fail
// (graceful degradation -- v6 status/thresholds are the primary data).
func (p *GrafanaCloudProvider) populateAggregateMetrics(...) {
	if val, err := p.QueryAggregateMetric(...); err != nil {
		slog.Warn("v5 aggregate query failed", "metric", "http_req_failed", "runId", runID, "error", err)
	} else {
		result.HTTPReqFailed = val
	}
}
```

The operator provider must follow the same graceful degradation: log at Warn, continue with zero-value metrics. Exit code-based thresholds remain the primary data.

**PodLogReader injection pattern** -- The operator.go file needs a `logReader PodLogReader` field on the `K6OperatorProvider` struct, initialized lazily alongside the client. Follow the existing `WithClient` functional option pattern (operator.go lines 37-43):

```go
// WithClient injects a Kubernetes client, bypassing lazy InClusterConfig (per D-07).
func WithClient(c kubernetes.Interface) Option {
	return func(p *K6OperatorProvider) {
		p.client = c
		p.clientOnce.Do(func() {})
	}
}
```

Add a `WithLogReader` option for tests:
```go
func WithLogReader(r PodLogReader) Option {
	return func(p *K6OperatorProvider) {
		p.logReader = r
	}
}
```

Production path: if `logReader` is nil after `ensureClient()`, create `k8sPodLogReader{client: p.client}`.

---

## Shared Patterns

### Structured Logging (slog)
**Source:** `internal/provider/operator/exitcode.go` lines 98-104, `internal/provider/operator/operator.go` lines 294-301
**Apply to:** `summary.go` (all log statements)

```go
slog.Warn("failed to parse handleSummary from pod logs, detailed metrics unavailable",
	"name", name,
	"namespace", ns,
	"error", err,
	"provider", p.Name(),
)

slog.Debug("parsed handleSummary metrics from pod logs",
	"name", name,
	"namespace", ns,
	"podCount", len(pods.Items),
	"httpReqFailed", result.HTTPReqFailed,
	"httpReqs", result.HTTPReqs,
)
```

Convention: always include `"name"` (testRunName) and `"namespace"` as fields. Use `"provider"` when called from a method with receiver. Use Warn for degraded paths, Debug for normal flow, Info for significant events.

### Graceful Degradation (D-05)
**Source:** `internal/provider/cloud/cloud.go` lines 149-188
**Apply to:** `operator.go` (GetRunResult) and `summary.go` (all parsing failures)

Pattern: failures in metric extraction log at Warn and return zero values. The measurement does not error. Exit code-based thresholds are the primary data source; handleSummary metrics are supplementary.

### Functional Options for Test Injection
**Source:** `internal/provider/operator/operator.go` lines 33-52
**Apply to:** `operator.go` (add WithLogReader option), `summary_test.go` (inject mock)

```go
type Option func(*K6OperatorProvider)

func WithClient(c kubernetes.Interface) Option {
	return func(p *K6OperatorProvider) {
		p.client = c
		p.clientOnce.Do(func() {})
	}
}
```

### Pod Discovery Label Selector
**Source:** `internal/provider/operator/exitcode.go` line 76
**Apply to:** `summary.go` (same label selector for pod log retrieval)

```go
selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)
```

### Test Assertions with InDelta for Floats
**Source:** `internal/provider/cloud/metrics_test.go` lines 273-277
**Apply to:** `summary_test.go` (all metric value assertions)

```go
assert.InDelta(t, 0.023, result.HTTPReqFailed, 0.001)
assert.InDelta(t, 234.5, result.HTTPReqDuration.P95, 0.1)
assert.InDelta(t, 142.3, result.HTTPReqs, 0.1)
```

### RunResult Struct Field Population
**Source:** `internal/provider/provider.go` lines 22-38
**Apply to:** `summary.go` (return type must match these fields exactly)

```go
type Percentiles struct {
	P50 float64
	P95 float64
	P99 float64
}

type RunResult struct {
	State            RunState
	TestRunURL       string
	ThresholdsPassed bool
	HTTPReqFailed    float64     // 0.0-1.0 fraction of failed requests
	HTTPReqDuration  Percentiles // milliseconds
	HTTPReqs         float64     // requests per second
}
```

The `parseSummaryFromPods` function should return a partial RunResult (or a dedicated metrics struct) containing only HTTPReqFailed, HTTPReqDuration, and HTTPReqs. The caller in operator.go copies these fields onto the full RunResult.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | -- | -- | All files have exact analogs in the codebase |

## Metadata

**Analog search scope:** `internal/provider/operator/`, `internal/provider/cloud/`, `internal/provider/`, `internal/metric/`
**Files scanned:** 12 (operator.go, exitcode.go, testrun.go, operator_test.go, exitcode_test.go, provider.go, config.go, cloud.go, metrics_test.go, cloud_test.go, metric.go, metric_test.go)
**Pattern extraction date:** 2026-04-16
