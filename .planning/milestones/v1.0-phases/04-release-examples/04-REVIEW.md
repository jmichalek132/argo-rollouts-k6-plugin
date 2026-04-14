---
phase: "04"
phase_name: "release-examples"
depth: deep
status: findings
files_reviewed: 18
findings:
  critical: 1
  warning: 6
  info: 4
  total: 11
---

# Deep Code Review -- Round 3

Reviewed all 18 source files across cmd/, internal/, e2e/, and e2e/mock/.
Previous rounds addressed: negative timeout, HTTP client timeouts, nil Metadata guard,
ParseInt overflow, unknown status warning, parseAggregateValue unexpected type test,
nil metadata test.

---

## CRITICAL

### C-01: URL injection via unsanitized metric name and query function in v5 API call

- **File**: `internal/provider/cloud/metrics.go:37`
- **Description**: `QueryAggregateMetric` interpolates `query.MetricName` and `query.QueryFunc` directly into the URL string via `fmt.Sprintf` without any validation or escaping. While the current call sites in `populateAggregateMetrics` use hardcoded literal values, the function is exported (`QueryAggregateMetric`) and accepts arbitrary strings. A future caller (or a malicious config value if the metric name were ever user-sourced) could inject path traversal or query parameters. The single-quote wrapping provides zero protection since URL paths have no quoting semantics.

  More concretely: `MetricName` value `foo','query='evil')/../../../admin` would alter the URL path entirely. Since this is an authenticated request (Bearer token in header), a path injection could redirect the token to an unintended endpoint on the same host.

- **Suggested fix**: Validate `MetricName` and `QueryFunc` against a strict allowlist or regex (e.g., `^[a-zA-Z0-9_().]+$`). Alternatively, use `url.PathEscape` on the parameters. Since the current callers are all hardcoded, the immediate risk is low, but the exported surface makes this a latent injection point.

```go
// Example validation before interpolation:
if !isValidMetricParam(query.MetricName) || !isValidMetricParam(query.QueryFunc) {
    return 0, fmt.Errorf("invalid metric parameter characters")
}

func isValidMetricParam(s string) bool {
    for _, r := range s {
        if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '(' || r == ')') {
            return false
        }
    }
    return len(s) > 0
}
```

---

## WARNING

### W-01: Metric plugin Terminate panics on nil Metadata map

- **File**: `internal/metric/metric.go:170`
- **Description**: `Terminate()` accesses `measurement.Metadata["runId"]` without a nil guard. If the controller passes a measurement with nil Metadata (unlikely but allowed by the interface contract), this is a nil map read -- which in Go actually returns the zero value safely and does NOT panic. However, the inconsistency with `Resume()` (which has an explicit nil guard at line 106) suggests this was an oversight. If future code were to *write* to Metadata in Terminate (e.g., adding a terminated-at timestamp), it would panic.

- **Suggested fix**: Add a nil guard consistent with Resume:
```go
if measurement.Metadata == nil {
    measurement.Metadata = map[string]string{}
}
```

### W-02: context.Background() used everywhere -- no timeout/cancellation propagation

- **Files**: `internal/metric/metric.go:74,115` and `internal/step/step.go:105,129,145,227`
- **Description**: Every provider call uses `context.Background()` with no timeout or cancellation. If the Grafana Cloud k6 API is slow or hangs, the plugin goroutine blocks indefinitely. The HTTP client has a 30s timeout (good), but `context.Background()` means the plugin cannot be cancelled by the Argo Rollouts controller shutting down. This is especially relevant for the step plugin where `StopRun` is called during timeout handling -- if StopRun itself hangs, the entire timeout path blocks.

- **Suggested fix**: Accept or derive a context with a reasonable deadline. At minimum, use `context.WithTimeout(context.Background(), 60*time.Second)` for provider calls. Ideally, propagate a context from the plugin framework (though go-plugin's net/rpc path doesn't provide one natively).

### W-03: v5 response body read without size limit

- **File**: `internal/provider/cloud/metrics.go:62-69`
- **Description**: `io.ReadAll(resp.Body)` reads the entire response body into memory with no size limit. A misbehaving or compromised server could send an arbitrarily large response body, causing OOM. Both the error path (line 63) and success path (line 68) use unbounded `io.ReadAll`.

- **Suggested fix**: Use `io.LimitReader(resp.Body, 1<<20)` (1 MB limit) before ReadAll:
```go
body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
```

### W-04: Duplicated mock provider type across test packages

- **Files**: `internal/metric/metric_test.go:21-48` and `internal/step/step_test.go:19-47`
- **Description**: The `mockProvider` struct is copy-pasted identically across both test packages. This is a maintenance burden -- any interface change to `provider.Provider` requires updating both copies. Consider extracting to an `internal/provider/providertest` package.

- **Suggested fix**: Create `internal/provider/providertest/mock.go` with a shared mock and import it in both test files.

### W-05: Mock server uses global mutable state with atomic counters -- no reset between e2e tests

- **File**: `e2e/mock/main.go:38-56`
- **Description**: `runConfigs` uses `atomic.Int64` counters that increment monotonically across all requests. If e2e tests run in sequence and share the same mock server instance, the counter from a previous test carries over. For example, if the metric-pass test polls runId 1001 twice (counter goes to 2), and later another test also uses runId 1001, it would see the "completed" response immediately instead of "running" first. The current test suite works because each test uses different runIds, but this is fragile.

- **Suggested fix**: Add a `/reset` endpoint to the mock server, or make the counters per-test by accepting a test scenario identifier.

### W-06: `liveCredentials` helper defined but not used by all live tests

- **File**: `e2e/live_test.go:196-211` vs `e2e/live_test.go:19-35,86-99`
- **Description**: `TestLiveMetricPlugin` and `TestLiveStepPlugin` manually read env vars and check them, while `TestLiveMetricPluginFail` and `TestLiveStepPluginFail` use the `liveCredentials` helper. This inconsistency is a minor DRY violation and could lead to divergent validation behavior.

- **Suggested fix**: Refactor `TestLiveMetricPlugin` and `TestLiveStepPlugin` to use `liveCredentials(t, "K6_TEST_ID")`.

---

## INFO

### I-01: `setupLogging()` is duplicated between both main.go files

- **Files**: `cmd/metric-plugin/main.go:49-64` and `cmd/step-plugin/main.go:49-64`
- **Description**: Identical `setupLogging()` function in both entry points. Could be extracted to a shared `internal/logging` package to reduce duplication.

### I-02: Step plugin `Run()` returns `types.RpcError` for infrastructure errors vs `PhaseFailed` for config errors

- **File**: `internal/step/step.go:68-93`
- **Description**: Config validation errors (parseConfig, parseTimeout) return `PhaseFailed` with empty `RpcError`, while infrastructure errors (TriggerRun, GetRunResult, unmarshal state) return non-empty `RpcError`. This is a deliberate design choice (config errors are user errors, infra errors are retryable), but the distinction is not documented. Adding a comment would help future maintainers understand the error classification strategy.

### I-03: `containsString`/`searchString` helpers in test reinvent `strings.Contains`

- **File**: `internal/provider/cloud/metrics_test.go:318-329`
- **Description**: `containsString` and `searchString` are manual reimplementations of `strings.Contains`. These were likely written to avoid importing `strings` in the test file, but the test file is not particularly import-constrained.

- **Suggested fix**: Replace with `strings.Contains`.

### I-04: Hardcoded Grafana Cloud stack ID in live tests

- **File**: `e2e/live_test.go:35,99`
- **Description**: Default `stackID = "1313689"` is hardcoded for the "jurajm stack". This is fine for the project author's workflow but should be documented as such (a comment exists for `TestLiveMetricPlugin` but not `TestLiveStepPlugin`).

---

## Items verified as correct (no finding)

1. **Auth token handling**: API token is passed via context value for the v6 client and via Authorization header for v5. Neither is logged at Info level. The token flows from PluginConfig (sourced from K8s Secret) and is never written to Metadata or stdout. Correct.

2. **Race conditions**: Both plugin types are stateless (no shared mutable state). The metric plugin's concurrent safety test confirms this. The step plugin relies on Argo Rollouts' serialized reconciliation loop. Mock provider in tests has no shared state between goroutines. No races found.

3. **Response body closure**: All v6 API responses properly close `resp.Body` (checked in cloud.go:78-79, 112-113, 207-208). The v5 response uses `defer` closure (metrics.go:59). Correct.

4. **Integer overflow guards**: All `ParseInt` calls use `math.MaxInt32` bounds checking before int32 cast. Correct.

5. **Negative timeout rejection**: `parseTimeout` rejects `d <= 0`. Correct.

6. **nil Metadata in Resume**: Guard at metric.go:106-108 initializes map before use. Correct.

7. **Unknown k6 status mapping**: `mapToRunState` logs a warning and returns `Errored` for unknown statuses. Correct.

8. **HTTP client timeout**: Both v6 client (cloud.go:52) and v5 client (metrics.go:54) have 30s timeouts. Correct.
