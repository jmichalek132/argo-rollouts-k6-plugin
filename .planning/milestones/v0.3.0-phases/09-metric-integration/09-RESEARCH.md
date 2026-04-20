# Phase 9: Metric Integration - Research

**Researched:** 2026-04-16
**Domain:** k6 handleSummary JSON parsing, Kubernetes pod log retrieval, multi-pod metric aggregation
**Confidence:** HIGH

## Summary

Phase 9 extends the k6-operator provider's `GetRunResult()` to populate the detailed metric fields (`HTTPReqFailed`, `HTTPReqDuration`, `HTTPReqs`) from k6's `handleSummary()` JSON output found in runner pod logs. Currently, `GetRunResult()` only returns `State` and `ThresholdsPassed` (from exit codes). The metric plugin's `extractMetricValue()` already reads these fields -- it just needs them populated.

The implementation is a clean extension of existing patterns: the pod discovery logic from `exitcode.go` (label selector `app=k6,k6_cr=<name>,runner=true`) is reusable for log retrieval. The main technical challenges are: (1) reliably detecting JSON in mixed k6 output, (2) mapping k6's JSON key format (`"p(95)"`, `"med"`, `"rate"`) to the `RunResult` struct fields, and (3) aggregating metrics across multiple runner pods for distributed runs (`parallelism > 1`).

No new dependencies are needed. All work happens in the `internal/provider/operator` package using `kubernetes.Interface` (already available) and `encoding/json` (stdlib). The metric plugin layer (`internal/metric/metric.go`) requires zero changes.

**Primary recommendation:** Create a `summary.go` file in the operator package that parses handleSummary JSON from pod logs, with a `PodLogReader` interface to enable unit testing (fake clientset's `GetLogs` does not support custom log content).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Users must export `handleSummary()` in their k6 script that writes JSON to stdout. Plugin does not inject or auto-generate handleSummary. Standard k6 pattern.
- **D-02:** Plugin scans runner pod logs from the end for a JSON object containing expected k6 summary top-level keys (`"metrics"`, `"root_group"`). handleSummary runs last in k6 lifecycle, so JSON is near the tail of output. No special markers or stream separation required.
- **D-03:** Pod log retrieval uses `TailLines` + `LimitBytes` on the Kubernetes pod log API request. Tail last ~100 lines or 64KB per pod. Bounded memory, fast.
- **D-04:** For `parallelism > 1` (distributed runs), plugin reads handleSummary JSON from every runner pod and aggregates metrics: sum counts (http_reqs), weighted-average rates (http_req_failed), merge percentile values.
- **D-05:** Graceful per-metric degradation. Thresholds always work via exit codes (Phase 8). Detailed metrics return zero with slog warning when handleSummary JSON is missing or malformed. Measurement does not error.
- **D-06:** Strict parity with Grafana Cloud provider. Same 4 metric types: thresholds, http_req_failed, http_req_duration (p50/p95/p99), http_reqs.
- **D-07:** handleSummary log parsing lives in a separate file (e.g., `summary.go`) inside the operator package.

### Claude's Discretion
- JSON detection heuristic implementation (regex, json.Decoder, or line-by-line scan)
- Aggregation math for multi-pod percentile merging (weighted average vs. max vs. merge strategy)
- Pod log reader helper function signature and error handling
- handleSummary JSON struct definition (typed vs. map[string]interface{})
- slog field names and warning message wording for missing handleSummary

### Deferred Ideas (OUT OF SCOPE)
None -- discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| METR-01 | Metric plugin extracts metric values (p95, error rate, throughput, threshold results) from k6 handleSummary() JSON in runner pod logs | k6 handleSummary JSON structure documented with exact key paths; pod log API verified; summary.go parsing pattern designed |
| METR-02 | Metric plugin works with k6-operator provider for successCondition evaluation, not just step plugin | extractMetricValue() already reads RunResult fields -- no metric plugin changes needed; only operator provider GetRunResult needs metric population |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| handleSummary JSON parsing | API / Backend (operator provider) | -- | Parsing happens in the plugin binary, which is the provider layer |
| Pod log retrieval | API / Backend (operator provider) | Database / Storage (K8s API) | Provider calls K8s API to read pod logs; K8s stores the logs |
| Multi-pod aggregation | API / Backend (operator provider) | -- | Math runs in the provider after collecting all pod logs |
| Metric value extraction | API / Backend (metric plugin) | -- | Already implemented in extractMetricValue(); no changes needed |
| successCondition evaluation | API / Backend (Argo Rollouts controller) | -- | Argo Rollouts evaluates successCondition against measurement.Value |

## Standard Stack

### Core (already in go.mod -- no new dependencies)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `encoding/json` | stdlib | Parse handleSummary JSON from pod logs | No external JSON library needed; k6 output is well-formed JSON |
| `k8s.io/client-go` | v0.34.1 | Pod log retrieval via `CoreV1().Pods().GetLogs()` | Already a direct dependency from Phase 7 |
| `k8s.io/api` | v0.34.1 | `corev1.PodLogOptions` struct | Already a direct dependency |
| `log/slog` | stdlib | Structured warnings for missing/malformed handleSummary | Locked decision from D-12 |

### Supporting (already in go.mod)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.11.1 | Test assertions | Unit tests for summary parsing and aggregation |
| `k8s.io/client-go/kubernetes/fake` | v0.34.1 | Fake K8s client for tests | Pod listing in tests (but NOT for GetLogs -- see Pitfall 3) |

**Installation:** No new packages needed. All dependencies already in `go.mod`.

## Architecture Patterns

### System Architecture Diagram

```
                    AnalysisRun (metric=http_req_duration, aggregation=p95)
                                        |
                                        v
                    +-------------------+-------------------+
                    |       metric.go (extractMetricValue)  |
                    |  reads RunResult.HTTPReqDuration.P95  |
                    +-------------------+-------------------+
                                        |
                            GetRunResult(ctx, cfg, runID)
                                        |
                                        v
            +---------------------------+---------------------------+
            |              operator.go (GetRunResult)               |
            |  1. GET TestRun CR -> check stage                     |
            |  2. If "finished": checkRunnerExitCodes() -> state    |
            |  3. If terminal: parseSummaryFromPods() -> metrics    | <-- NEW
            |  4. Return RunResult{State, Thresholds, Metrics}      |
            +---------------------------+---------------------------+
                                        |
                        +---------------+---------------+
                        |                               |
                        v                               v
            +----------+----------+          +---------+---------+
            |  exitcode.go        |          |  summary.go (NEW) |
            |  checkRunnerExitCodes|         |  parseSummaryFromPods|
            |  -> Passed/Failed   |          |  readPodLogs       |
            +---------------------+          |  parseHandleSummary |
                                             |  aggregateMetrics   |
                                             +---------+---------+
                                                       |
                                     +-----------------+-----------------+
                                     |                                   |
                                     v                                   v
                          Pod 0 logs (tail 100)              Pod 1 logs (tail 100)
                          {...handleSummary JSON...}         {...handleSummary JSON...}
```

### Recommended Project Structure (changes only)

```
internal/provider/operator/
  operator.go          # Modified: GetRunResult calls parseSummaryFromPods after exit code check
  exitcode.go          # Unchanged
  summary.go           # NEW: handleSummary JSON parsing, pod log reading, aggregation
  summary_test.go      # NEW: Unit tests for all summary parsing logic
  testrun.go           # Unchanged
```

### Pattern 1: PodLogReader Interface for Testability

**What:** Define a small interface for reading pod logs to decouple from `kubernetes.Interface` and enable unit testing without the fake clientset's broken `GetLogs`.
**When to use:** Always -- the fake clientset returns constant "fake logs" from GetLogs, making it impossible to test custom log content. [VERIFIED: kubernetes/kubernetes#125590]

```go
// Source: Pattern derived from kubernetes/client-go pod log API
// and fake clientset limitation kubernetes/kubernetes#125590

// PodLogReader abstracts pod log retrieval for testability.
// The fake clientset's GetLogs returns constant "fake logs" (issue #125590),
// so unit tests inject a mock implementation instead.
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
```

### Pattern 2: handleSummary JSON Structure (typed structs)

**What:** Typed Go structs matching k6's handleSummary JSON format for the fields we need.
**When to use:** Parsing pod log JSON. Using typed structs over `map[string]interface{}` for compile-time safety and clearer field access.

```go
// Source: https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/
// [VERIFIED: official k6 documentation]

// k6Summary represents the top-level handleSummary data object.
// Only fields needed for metric extraction are included.
type k6Summary struct {
    Metrics map[string]k6Metric `json:"metrics"`
}

// k6Metric represents a single metric in the handleSummary output.
type k6Metric struct {
    Type       string                       `json:"type"`    // "trend", "rate", "counter", "gauge"
    Contains   string                       `json:"contains"` // "time", "default", etc.
    Values     map[string]float64           `json:"values"`  // key format varies by type
    Thresholds map[string]k6ThresholdResult `json:"thresholds,omitempty"`
}

// k6ThresholdResult holds the pass/fail status of a single threshold.
type k6ThresholdResult struct {
    OK bool `json:"ok"`
}
```

### Pattern 3: JSON Detection in Mixed Output

**What:** Scan pod log output from the end to find the handleSummary JSON blob.
**When to use:** Pod logs contain k6 console output (progress bars, check results, etc.) mixed with the handleSummary JSON at the tail.

```go
// Recommended approach: line-by-line reverse scan for JSON object
// that contains expected top-level keys.
//
// Why not json.Decoder: k6 output contains partial JSON-like fragments
// (progress output, check names with braces). json.Decoder would attempt
// to parse every '{' character.
//
// Why not regex: brittle with nested JSON objects.
//
// Approach: scan lines from end, find opening '{', attempt json.Unmarshal
// on accumulated text, validate presence of "metrics" key.

func findSummaryJSON(logOutput string) (*k6Summary, error) {
    // Split into lines, scan from end
    lines := strings.Split(logOutput, "\n")
    
    // Walk backward to find the JSON blob
    // handleSummary output is the LAST thing written by k6
    // It may span multiple lines if pretty-printed
    
    // Strategy: find last line containing closing '}',
    // then walk backward to find matching opening '{',
    // attempt to unmarshal the block
    // ...
}
```

### Pattern 4: Multi-Pod Aggregation (D-04)

**What:** Aggregate handleSummary metrics from multiple runner pods for distributed runs.
**When to use:** When `parallelism > 1` in the k6-operator config.

```go
// Aggregation rules (matches Grafana Cloud's distributed aggregation):
//
// http_reqs (counter): sum counts, compute aggregate rate
//   - rate = sum(pod_count) / max(pod_duration) -- or sum rates if durations equal
//
// http_req_failed (rate): weighted average by request count
//   - aggregate = sum(pod_failed_count) / sum(pod_total_count)
//   - From handleSummary: rate * count gives failed count per pod
//
// http_req_duration (trend, percentiles): weighted average by request count
//   - p95_aggregate = sum(pod_p95 * pod_count) / sum(pod_count)
//   - Not mathematically precise for percentiles, but matches Grafana Cloud behavior
//   - Acceptable because k6 distributed runs split VUs evenly across pods
//
// thresholds: already handled by exit codes (Phase 8), not from handleSummary
```

### Anti-Patterns to Avoid

- **Parsing all pod logs synchronously in sequence:** For `parallelism > 1`, read logs from all pods before processing. Do NOT read one pod, parse, then read next -- this increases total latency. However, keep it simple: sequential reads are fine for typical parallelism (2-4 pods). Only optimize to concurrent reads if profiling shows this is a bottleneck (it won't be).

- **Modifying metric.go:** The metric plugin layer (`extractMetricValue()`) already handles all 4 metric types. Adding code there is wrong -- the fix is in the provider layer that populates `RunResult`.

- **Using json.Decoder on raw log output:** k6 console output contains braces in check names, progress bars, and other non-JSON text. A streaming decoder would produce confusing errors. Use `json.Unmarshal` on isolated JSON blocks.

- **Requiring specific summaryTrendStats config:** The plugin MUST work with default k6 options. Default `summaryTrendStats` is `['avg', 'min', 'med', 'max', 'p(90)', 'p(95)']`. Note: `p(99)` and `p(50)` are NOT in the default set. See Pitfall 1.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Pod log retrieval | Custom HTTP client to kubelet | `client.CoreV1().Pods(ns).GetLogs()` | Standard K8s API; handles auth, TLS, proxy automatically |
| JSON parsing | Custom tokenizer/parser | `encoding/json` stdlib | k6 handleSummary output is valid JSON; stdlib handles it perfectly |
| handleSummary injection | Script modification to inject handleSummary | User writes their own handleSummary (D-01) | Zero magic, user controls output format, standard k6 pattern |
| Threshold evaluation | Custom threshold parser | Exit codes from Phase 8 | k6 exit code 99 = thresholds failed. Already implemented. handleSummary thresholds are supplementary |

**Key insight:** The metric extraction is entirely within the provider layer. The metric plugin already knows how to read `RunResult` fields. The only gap is the provider not populating them for k6-operator runs.

## Common Pitfalls

### Pitfall 1: summaryTrendStats Key Mismatch
**What goes wrong:** Plugin looks for `"p(50)"` or `"p(99)"` in handleSummary JSON, but they don't exist in the default output.
**Why it happens:** k6's default `summaryTrendStats` is `['avg', 'min', 'med', 'max', 'p(90)', 'p(95)']`. The key for median is `"med"`, NOT `"p(50)"`. The key `"p(99)"` is NOT present by default -- only `"p(99.99)"` or custom values if configured. [VERIFIED: grafana.com/docs/k6/latest/using-k6/k6-options/reference/]
**How to avoid:** Map `RunResult.HTTPReqDuration.P50` from `values["med"]` (or `values["p(50)"]` as fallback). Map `P95` from `values["p(95)"]`. Map `P99` from `values["p(99)"]`. If a key is missing, return 0.0 and log a warning (per D-05 graceful degradation). Document that users need `summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)']` in their k6 options to get P99.
**Warning signs:** P50 always zero (looked for wrong key), P99 always zero (not in default stats).

### Pitfall 2: handleSummary JSON Not in Default k6 Output
**What goes wrong:** Plugin expects handleSummary JSON in every k6 run's pod logs, but k6 does NOT write JSON to stdout by default.
**Why it happens:** `handleSummary()` must be explicitly exported by the user's k6 script. Without it, k6 writes its default text summary to stdout, which is not JSON-parseable.
**How to avoid:** Per D-01 and D-05, treat missing handleSummary as graceful degradation. Return zero-value metrics with slog warning. Thresholds still work via exit codes. Documentation (Phase 10) must show the required `handleSummary()` export.
**Warning signs:** JSON parsing always fails; all detailed metrics are zero.

### Pitfall 3: Fake Clientset GetLogs Returns Constant "fake logs"
**What goes wrong:** Unit tests using `fake.NewSimpleClientset()` cannot test custom pod log content because `GetLogs()` always returns "fake logs". [VERIFIED: kubernetes/kubernetes#125590]
**Why it happens:** The fake clientset's `GetLogs` implementation in `fake_pod_expansion.go` discards reactor results and returns a hardcoded response.
**How to avoid:** Define a `PodLogReader` interface. Production code uses a real client implementation. Tests inject a mock that returns controlled log content. This also makes the log reading concern testable in isolation.
**Warning signs:** Tests pass but don't actually verify JSON parsing because they always get "fake logs".

### Pitfall 4: JSON Detection in Multi-Line Output
**What goes wrong:** JSON detection grabs a partial JSON object or a non-handleSummary JSON fragment from k6 output.
**Why it happens:** k6 output can contain JSON-like fragments from `console.log()` calls in user scripts, or from `--out json` if configured. The handleSummary JSON may span multiple lines.
**How to avoid:** After finding a candidate JSON block, validate it has the expected top-level keys (`"metrics"` AND `"root_group"`). This combination is unique to handleSummary output. Reject partial parses.
**Warning signs:** Incorrect metric values from wrong JSON; parse errors on valid handleSummary output.

### Pitfall 5: Percentile Aggregation Across Pods is Approximate
**What goes wrong:** Weighted-average of percentiles across pods is mathematically incorrect (percentiles are not linear).
**Why it happens:** True percentile merging requires the full data distribution, which handleSummary does not provide.
**How to avoid:** Accept the approximation -- it is what Grafana Cloud does for distributed runs (D-04). With even VU distribution across pods (k6-operator default), the approximation is close enough for rollout gating decisions. Document the limitation.
**Warning signs:** Aggregated P95 slightly differs from what a single-node run would report. This is expected and acceptable.

### Pitfall 6: Pod Logs Truncated or Missing
**What goes wrong:** TailLines/LimitBytes truncates the handleSummary JSON if the k6 script produces very verbose output after the test completes.
**Why it happens:** User scripts may have `console.log()` in `teardown()` or handleSummary itself may be large (many custom metrics).
**How to avoid:** Use generous limits (D-03: ~100 lines / 64KB). handleSummary JSON for typical k6 scripts (4 standard metrics + thresholds) is 2-5KB. Log a clear warning if JSON is truncated (starts with `{` but no closing `}`). Users can increase script efficiency or the plugin can increase limits in future.
**Warning signs:** JSON parse error on what looks like valid start-of-JSON.

## Code Examples

### Example 1: Pod Log Retrieval with TailLines + LimitBytes

```go
// Source: https://pkg.go.dev/k8s.io/api/core/v1#PodLogOptions
// [VERIFIED: k8s.io/api v0.34.1 already in go.mod]

func readPodLogs(ctx context.Context, reader PodLogReader, namespace, podName string) (string, error) {
    tailLines := int64(100)
    limitBytes := int64(64 * 1024) // 64KB
    opts := &corev1.PodLogOptions{
        TailLines:  &tailLines,
        LimitBytes: &limitBytes,
    }
    return reader.ReadLogs(ctx, namespace, podName, opts)
}
```

### Example 2: Extract Metrics from handleSummary JSON

```go
// Source: https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/
// [VERIFIED: official k6 documentation, JSON key format confirmed]

func extractMetricsFromSummary(summary *k6Summary) (httpReqFailed float64, duration provider.Percentiles, httpReqs float64) {
    // http_req_failed: rate metric, key is "rate"
    if m, ok := summary.Metrics["http_req_failed"]; ok {
        httpReqFailed = m.Values["rate"] // 0.0-1.0 fraction
    }

    // http_req_duration: trend metric, keys use parentheses format
    if m, ok := summary.Metrics["http_req_duration"]; ok {
        // P50: k6 uses "med" key (median), NOT "p(50)"
        // Fall back to "p(50)" if user configured summaryTrendStats with it
        if v, ok := m.Values["med"]; ok {
            duration.P50 = v
        } else if v, ok := m.Values["p(50)"]; ok {
            duration.P50 = v
        }
        duration.P95 = m.Values["p(95)"]
        duration.P99 = m.Values["p(99)"] // may be 0 if not in summaryTrendStats
    }

    // http_reqs: counter metric, key is "rate" (requests per second)
    if m, ok := summary.Metrics["http_reqs"]; ok {
        httpReqs = m.Values["rate"]
    }

    return httpReqFailed, duration, httpReqs
}
```

### Example 3: Integration Point in GetRunResult

```go
// Source: existing operator.go pattern
// [VERIFIED: codebase internal/provider/operator/operator.go]

// In GetRunResult, after exit code check determines state:
if state == provider.Passed || state == provider.Failed {
    // Populate detailed metrics from handleSummary (METR-01).
    // Graceful degradation per D-05: failures log warning, return zero metrics.
    metrics, err := parseSummaryFromPods(ctx, logReader, client, ns, name)
    if err != nil {
        slog.Warn("failed to parse handleSummary from pod logs, detailed metrics unavailable",
            "name", name,
            "namespace", ns,
            "error", err,
            "provider", p.Name(),
        )
        // metrics is zero-valued -- result returns with State + ThresholdsPassed only
    } else {
        result.HTTPReqFailed = metrics.HTTPReqFailed
        result.HTTPReqDuration = metrics.HTTPReqDuration
        result.HTTPReqs = metrics.HTTPReqs
    }
}
```

### Example 4: k6 Script with handleSummary (for documentation)

```javascript
// Required k6 script pattern for metric integration (D-01).
// Users MUST export handleSummary to enable detailed metrics.
import http from 'k6/http';

export const options = {
    thresholds: {
        http_req_duration: ['p(95)<500'],
        http_req_failed: ['rate<0.01'],
    },
    // Optional: add p(99) to get P99 percentile in metrics
    // Default summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)']
    summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export default function () {
    http.get('https://test.k6.io');
}

// REQUIRED: Export handleSummary to write JSON to stdout
export function handleSummary(data) {
    return {
        stdout: JSON.stringify(data),
    };
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `--summary-export` flag | `handleSummary()` function | k6 v0.30.0 (2020) | `--summary-export` deprecated; handleSummary is the standard way to export end-of-test summary as JSON [VERIFIED: k6 docs] |
| Fixed summaryTrendStats | Configurable via options | k6 v0.30.0+ | Users control which trend stats appear in handleSummary JSON values; plugin must handle missing keys gracefully |
| k6-operator status.result | Exit code workaround | k6-operator issue #577 (open) | k6-operator does not propagate test results to CR status; exit code check in Phase 8 is the established workaround |

## k6 handleSummary JSON Reference

### Top-Level Structure [VERIFIED: grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/]

```json
{
    "metrics": { ... },
    "root_group": { ... },
    "options": { ... },
    "state": { "testRunDurationMs": 30000, ... }
}
```

### Metric Value Keys by Type [VERIFIED: grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/]

| Metric Type | k6 JSON Key | Values Keys | Maps to RunResult Field |
|-------------|------------|-------------|------------------------|
| `http_req_failed` | `metrics.http_req_failed` | `"rate"` (0.0-1.0) | `HTTPReqFailed` |
| `http_req_duration` | `metrics.http_req_duration` | `"med"` (P50), `"p(95)"`, `"p(99)"` | `HTTPReqDuration.P50/P95/P99` |
| `http_reqs` | `metrics.http_reqs` | `"rate"` (req/s), `"count"` (total) | `HTTPReqs` |
| thresholds | embedded in each metric's `thresholds` field | `"ok"` (bool) | `ThresholdsPassed` (from exit codes) |

### Default summaryTrendStats [VERIFIED: grafana.com/docs/k6/latest/using-k6/k6-options/reference/]

Default: `['avg', 'min', 'med', 'max', 'p(90)', 'p(95)']`

**Implication:** `"p(99)"` is NOT present by default. `"med"` (not `"p(50)"`) is the median key. Users must add `p(99)` to their `summaryTrendStats` option if they want P99 metrics. The plugin must gracefully handle missing keys (return 0.0).

### summaryTrendStats Affects handleSummary JSON [VERIFIED: github.com/grafana/k6/issues/1779]

The keys in `metrics.<name>.values` are determined by the `summaryTrendStats` option. If the user changes this option, different keys appear. The plugin must not assume any specific key exists beyond what the user configured.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Weighted-average percentile aggregation matches Grafana Cloud behavior for distributed runs | Code Examples / Pattern 4 | Aggregated P95/P99 values would differ between k6-operator and Grafana Cloud providers; users switching providers might see different metric values. Low risk -- even if the exact algorithm differs, both are approximations |
| A2 | handleSummary JSON for typical k6 scripts fits within 64KB | Pitfall 6 | JSON would be truncated, parsing fails. Mitigation: graceful degradation (D-05) ensures this is a warning, not an error |
| A3 | k6-operator runner pods always write handleSummary output to the pod's main container stdout | Pattern 3 | If handleSummary writes to a file instead, pod logs won't contain the JSON. Mitigation: D-01 explicitly requires `stdout: JSON.stringify(data)` in the handleSummary return |

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify v1.11.1 |
| Config file | None (Go convention: `go test` discovers `*_test.go`) |
| Quick run command | `go test -race -v -count=1 ./internal/provider/operator/...` |
| Full suite command | `make test` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| METR-01a | Parse handleSummary JSON from single pod logs | unit | `go test -race -run TestParseSummary -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-01b | Extract http_req_failed rate from summary | unit | `go test -race -run TestExtractHTTPReqFailed -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-01c | Extract http_req_duration percentiles (P50/P95/P99) | unit | `go test -race -run TestExtractHTTPReqDuration -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-01d | Extract http_reqs rate from summary | unit | `go test -race -run TestExtractHTTPReqs -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-01e | Graceful degradation when handleSummary missing | unit | `go test -race -run TestMissingSummary -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-01f | Graceful degradation when JSON malformed | unit | `go test -race -run TestMalformedSummary -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-01g | Handle missing summaryTrendStats keys (p99 absent) | unit | `go test -race -run TestMissingTrendStats -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-02a | GetRunResult populates RunResult metric fields | unit | `go test -race -run TestGetRunResult_WithSummary -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-02b | Multi-pod aggregation for parallelism > 1 | unit | `go test -race -run TestAggregateMetrics -count=1 ./internal/provider/operator/...` | Wave 0 |
| METR-02c | extractMetricValue works unchanged with populated RunResult | unit | `go test -race -run TestResume_HTTP -count=1 ./internal/metric/...` | Exists (metric_test.go) |

### Sampling Rate
- **Per task commit:** `go test -race -v -count=1 ./internal/provider/operator/...`
- **Per wave merge:** `make test`
- **Phase gate:** Full suite green + `make lint` before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/provider/operator/summary_test.go` -- covers METR-01a through METR-01g, METR-02b
- [ ] Test helper: mock `PodLogReader` implementation for injecting custom log content

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | -- |
| V3 Session Management | no | -- |
| V4 Access Control | no | Pod log access controlled by RBAC (already configured in Phase 7) |
| V5 Input Validation | yes | Validate handleSummary JSON structure before extracting values; reject unexpected shapes |
| V6 Cryptography | no | -- |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Malicious JSON in pod logs (DoS via large allocations) | Denial of Service | `LimitBytes` on pod log request (64KB cap per D-03); `json.Unmarshal` on bounded input |
| JSON injection via k6 script console.log | Tampering | Validate top-level keys (`"metrics"` + `"root_group"`) before trusting parsed data |
| Metric value spoofing (user script returns fake metrics) | Tampering | Acceptable risk: user controls their own k6 script. The plugin trusts handleSummary output by design (D-01). Exit code-based thresholds provide independent verification. |

## Sources

### Primary (HIGH confidence)
- [k6 Custom Summary (handleSummary) docs](https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/) - JSON structure, metric value keys, thresholds format
- [k6 Options Reference (summaryTrendStats)](https://grafana.com/docs/k6/latest/using-k6/k6-options/reference/) - Default trend stats, available stat names, "med" key
- [Codebase: internal/provider/operator/operator.go](internal/provider/operator/operator.go) - GetRunResult implementation, integration point
- [Codebase: internal/provider/operator/exitcode.go](internal/provider/operator/exitcode.go) - Pod discovery pattern (label selector), exit code handling
- [Codebase: internal/metric/metric.go](internal/metric/metric.go) - extractMetricValue() implementation, RunResult field mapping
- [Codebase: internal/provider/provider.go](internal/provider/provider.go) - RunResult struct, Percentiles struct
- [Codebase: internal/provider/cloud/cloud.go](internal/provider/cloud/cloud.go) - populateAggregateMetrics() reference implementation

### Secondary (MEDIUM confidence)
- [k6 GitHub issue #1779 (summaryTrendStats and JSON export)](https://github.com/grafana/k6/issues/1779) - Confirms summaryTrendStats affects handleSummary JSON keys
- [Kubernetes client-go GetLogs API](https://pkg.go.dev/k8s.io/client-go/kubernetes/typed/core/v1) - Pod log retrieval API
- [Kubernetes fake clientset GetLogs issue #125590](https://github.com/kubernetes/kubernetes/issues/125590) - Confirms fake GetLogs limitation

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - all dependencies already in go.mod, no new packages needed
- Architecture: HIGH - clean extension of existing patterns, integration points are well-defined
- Pitfalls: HIGH - k6 JSON format verified against official docs, fake clientset limitation verified against upstream issue

**Research date:** 2026-04-16
**Valid until:** 2026-05-16 (stable -- k6 handleSummary format has been stable since v0.30.0)
