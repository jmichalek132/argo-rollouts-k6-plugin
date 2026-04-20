# Phase 9: Metric Integration - Context

**Gathered:** 2026-04-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Metric plugin extracts k6 result metrics (p95, error rate, throughput, threshold results) from in-cluster runner pod logs via handleSummary() JSON, so the metric plugin's `successCondition` evaluation works identically for k6-operator and Grafana Cloud providers. Users switch providers without rewriting AnalysisTemplates.

</domain>

<decisions>
## Implementation Decisions

### handleSummary Delivery
- **D-01:** Users must export `handleSummary()` in their k6 script that writes JSON to stdout (e.g., `export function handleSummary(data) { return { stdout: JSON.stringify(data) } }`). Plugin does not inject or auto-generate handleSummary. Standard k6 pattern, zero magic, well-documented.

### Log Parsing Strategy
- **D-02:** Plugin scans runner pod logs from the end for a JSON object containing expected k6 summary top-level keys (`"metrics"`, `"root_group"`). handleSummary runs last in k6 lifecycle, so JSON is near the tail of output. No special markers or stream separation required.
- **D-03:** Pod log retrieval uses `TailLines` + `LimitBytes` on the Kubernetes pod log API request. Tail last ~100 lines or 64KB per pod. Bounded memory, fast, works for typical k6 scripts.

### Multi-Pod Aggregation
- **D-04:** For `parallelism > 1` (distributed runs), plugin reads handleSummary JSON from every runner pod and aggregates metrics: sum counts (http_reqs), weighted-average rates (http_req_failed), merge percentile values. Matches what Grafana Cloud returns for distributed runs.

### Fallback Behavior
- **D-05:** Graceful per-metric degradation. Thresholds always work via exit codes (Phase 8). Detailed metrics (http_req_failed, http_req_duration, http_reqs) return zero with slog warning when handleSummary JSON is missing or malformed. Measurement does not error. Users get partial value even without handleSummary.

### Metric Parity
- **D-06:** Strict parity with Grafana Cloud provider. Same 4 metric types: thresholds, http_req_failed, http_req_duration (p50/p95/p99), http_reqs. Extended metrics from handleSummary (iteration_duration, vus_max, data_received, etc.) are a future phase.

### Code Organization
- **D-07:** handleSummary log parsing lives in a separate file (e.g., `summary.go`) inside the operator package. Called from GetRunResult after exit code check for terminal runs. Follows existing pattern: `exitcode.go` handles pass/fail determination, `summary.go` handles detailed metric extraction.

### Claude's Discretion
- JSON detection heuristic implementation (regex, json.Decoder, or line-by-line scan)
- Aggregation math for multi-pod percentile merging (weighted average vs. max vs. merge strategy)
- Pod log reader helper function signature and error handling
- handleSummary JSON struct definition (typed vs. map[string]interface{})
- slog field names and warning message wording for missing handleSummary

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Metric Plugin (consumes RunResult)
- `internal/metric/metric.go` -- K6MetricProvider with extractMetricValue(), maps RunResult fields to measurement values
- `internal/metric/metric.go:236` -- extractMetricValue() defines the 4 metric types and how they map to RunResult fields

### Provider Interface & Types
- `internal/provider/provider.go` -- Provider interface, RunResult struct (ThresholdsPassed, HTTPReqFailed, HTTPReqDuration, HTTPReqs), RunState
- `internal/provider/config.go` -- PluginConfig with k6-operator fields (parallelism, namespace, configMapRef)

### K6-Operator Provider (extend GetRunResult)
- `internal/provider/operator/operator.go` -- K6OperatorProvider with GetRunResult stub returning only State+ThresholdsPassed, needs detailed metric population
- `internal/provider/operator/exitcode.go` -- checkRunnerExitCodes() pattern: lists runner pods by label, inspects container statuses. Summary parsing follows same pod discovery.
- `internal/provider/operator/operator_test.go` -- Test patterns with WithClient/WithDynClient fake injection

### Grafana Cloud Provider (reference implementation for metric population)
- `internal/provider/cloud/cloud.go:149` -- populateAggregateMetrics() shows how RunResult fields are populated after terminal state. Phase 9 does the equivalent from handleSummary JSON.

### Requirements
- `.planning/REQUIREMENTS.md` -- METR-01, METR-02 definitions

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `checkRunnerExitCodes()` in exitcode.go -- pod discovery pattern (label selector `app=k6,k6_cr=<name>,runner=true`) reusable for log reading
- `provider.RunResult` struct -- already has all 4 metric fields, just needs population
- `extractMetricValue()` in metric.go -- already handles all 4 metric types, no changes needed
- `kubernetes.Interface` client -- already available in K6OperatorProvider via ensureClient()

### Established Patterns
- Separate concern files in operator package: operator.go (lifecycle), exitcode.go (exit codes), testrun.go (CR construction)
- Functional options for testing: WithClient(fake), WithDynClient(fake)
- slog structured logging to stderr with JSON handler
- providerCallTimeout context timeout on every provider call

### Integration Points
- `GetRunResult()` in operator.go -- after exit code check determines pass/fail, call summary parsing to populate HTTPReqFailed, HTTPReqDuration, HTTPReqs
- Pod log API: `client.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{TailLines: &n, LimitBytes: &b})`
- No metric plugin changes needed -- extractMetricValue() already reads RunResult fields

</code_context>

<specifics>
## Specific Ideas

- handleSummary JSON is at the tail of k6 output because handleSummary() runs after the test completes and all other output has been flushed
- For multi-pod aggregation with percentiles, weighted average by request count is the most accurate approach (same as Grafana Cloud's distributed aggregation)
- The same runner pod label selector from exitcode.go can be reused for log retrieval -- pod discovery is already solved

</specifics>

<deferred>
## Deferred Ideas

None -- discussion stayed within phase scope

</deferred>

---

*Phase: 09-metric-integration*
*Context gathered: 2026-04-16*
