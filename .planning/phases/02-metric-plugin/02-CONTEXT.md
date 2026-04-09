# Phase 2: Metric Plugin — Context

**Gathered:** 2026-04-09
**Status:** Ready for planning

<domain>
## Phase Boundary

A fully functional `RpcMetricProvider` implementation in `cmd/metric-plugin/main.go` backed by `internal/metric/` logic. The plugin implements the async `Run/Resume` measurement lifecycle: `Run()` triggers a k6 test run (or polls an existing one if `testRunId` is provided), stores the runId in `Measurement.Metadata`, returns `PhaseRunning`. `Resume()` polls `GetRunResult` and returns the requested metric value once the run reaches a terminal state — or live partial values while it's running (per D-03 from Phase 1). All four metric types are supported. Unit tests cover config parsing, metric calculations, Run/Resume state machine, error handling, and concurrent safety.

Phase 3 (step plugin) depends on the Provider interface established in Phase 1. This phase does NOT implement step plugin logic.

</domain>

<decisions>
## Implementation Decisions

### Metric dispatch key
- **D-01:** Metric type specified via `metric` field in plugin config JSON. Valid values: `thresholds`, `http_req_failed`, `http_req_duration`, `http_reqs`.
- **D-02:** For `http_req_duration`, an `aggregation` field specifies the percentile: `p50`, `p95`, `p99`. Other metrics ignore `aggregation`.
- **D-03:** PluginConfig additions needed: `Metric string` (JSON: `metric`), `Aggregation string` (JSON: `aggregation`, omitempty). These are added in `internal/provider/config.go`.

### Trigger vs. poll-only
- **D-04:** Trigger-or-poll mode — if `testRunId` is provided in PluginConfig, the metric plugin skips triggering and polls that existing run directly. If only `testId` is provided (and `testRunId` is empty), `Run()` calls `TriggerRun` and stores the resulting runId in `Measurement.Metadata["runId"]`.
- **D-05:** `Measurement.Metadata` is the sole state store for runId across `Run` → `Resume` calls. Key: `"runId"`. No plugin-level shared state.

### Multi-metric run isolation
- **D-06:** Independent runs per Argo metric. Each metric manages its own k6 run lifecycle independently. No implicit run sharing across metrics in the same AnalysisTemplate.
- **D-07:** Users who want multiple metrics from one run should provide `testRunId` explicitly to all metrics (via step plugin output or AnalysisTemplate arg). This is the intended coordination pattern for Phase 3+.

### Error/Aborted phase mapping
- **D-08:** k6 `Errored` (script crash, infra failure) → Argo `PhaseError` (inconclusive — increments errorLimit counter, does not directly count toward failureCondition).
- **D-09:** k6 `Aborted` (stopped externally) → Argo `PhaseError` (inconclusive — same rationale as Errored).
- **D-10:** k6 `Passed` + metric within threshold → Argo `PhaseSuccessful`. k6 `Failed` (threshold breached) → Argo `PhaseFailed`. k6 `Running` → Argo `PhaseRunning` (with partial metric value populated).

### Measurement.Metadata content
- **D-11:** Metadata keys populated on every measurement (Run, Resume, terminal):
  - `"runId"` — k6 Cloud test run ID (for coordination and debugging)
  - `"testRunURL"` — full URL to k6 Cloud dashboard for this run (from RunResult.TestRunURL)
  - `"runState"` — current RunState string (e.g., "Running", "Passed", "Failed")
  - `"metricValue"` — the numeric value returned as measurement (for human-readable debugging)
- **D-12:** Metadata is set on each Resume call, not just on terminal state, so `kubectl get analysisrun -o yaml` shows live progress.

### Package layout
- **D-13:** RpcMetricProvider implementation lives in `internal/metric/metric.go`. Struct: `K6MetricProvider` implementing the `v1alpha1.MetricProvider` interface (which the RPC layer wraps).
- **D-14:** `cmd/metric-plugin/main.go` registers `"RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{Impl: impl}` in the plugin map. The `impl` is a `*metric.K6MetricProvider` initialized with a `*cloud.GrafanaCloudProvider`.

### GarbageCollect behavior
- **D-15:** GarbageCollect is a no-op for Phase 2. The Terminate method handles stopping active runs. GarbageCollect receives an AnalysisRun but the plugin has no persistent state outside of Measurement.Metadata (which Argo owns).

### Terminate behavior
- **D-16:** `Terminate()` calls `StopRun` on the provider if a `runId` is present in `run.Metadata["runId"]`. Returns the measurement with `PhaseError` phase (was interrupted, not a clean result).

### Test approach
- **D-17:** Unit tests mock the `Provider` interface (using a hand-written mock struct, not a framework). Tests cover: config parsing (all metric types, missing fields, invalid aggregation), Run() → trigger path, Run() → poll-only path (testRunId provided), Resume() with live RunState, Resume() with each terminal RunState, Terminate(), concurrent Resume() calls (race detector).
- **D-18:** `go test -race -v -count=1 ./internal/metric/...` must pass. >=80% coverage on `internal/metric/` package.

### Claude's Discretion
- Exact error message strings and wrapping patterns
- Whether to use `context.WithTimeout` inside metric plugin or rely on the cfg.Timeout field
- Internal helper functions (e.g., `extractMetricValue(*RunResult, metricType, aggregation) (float64, error)`)
- Exact Measurement.Metadata key names (as long as they match D-11 semantics)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project planning
- `.planning/PROJECT.md` — Project goals, constraints, out-of-scope decisions
- `.planning/REQUIREMENTS.md` — Phase 2 requirements: PLUG-01, METR-01, METR-02, METR-03, METR-04, METR-05, TEST-01, TEST-03

### Phase 1 artifacts
- `.planning/phases/01-foundation-provider/01-CONTEXT.md` — All locked decisions D-01–D-17 (provider interface, RunResult shape, PluginConfig, slog, Go 1.24)
- `internal/provider/provider.go` — Provider interface and RunResult/RunState/Percentiles types
- `internal/provider/config.go` — PluginConfig struct (to be extended with Metric + Aggregation fields)
- `internal/provider/cloud/cloud.go` — GrafanaCloudProvider implementation (Phase 2 uses it directly)
- `cmd/metric-plugin/main.go` — Existing stub; Phase 2 fills in the plugin map

### Research (Phase 1)
- `.planning/research/STACK.md` — RpcMetricProvider interface signatures, RPC layer details
- `.planning/research/PITFALLS.md` — Non-idempotent Run() pitfall (must not trigger twice), concurrent state races
- `.planning/phases/01-foundation-provider/01-RESEARCH.md` — v5 metrics API endpoints, auth details, RunState mappings

### External references
- `https://github.com/argoproj/argo-rollouts/tree/master/metricproviders/plugin/rpc/rpc.go` — RpcMetricProvider RPC wrapper (how Run/Resume/Terminate args are passed)
- `https://github.com/argoproj/argo-rollouts/tree/master/test/cmd/metrics-plugin-sample` — Reference metric plugin binary structure
- `https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types` — MetricProvider interface definition

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/provider/provider.go` — Provider interface, RunResult, RunState, Percentiles, IsTerminal() — use directly
- `internal/provider/config.go` — PluginConfig struct — extend with `Metric` and `Aggregation` fields
- `internal/provider/cloud/cloud.go` — GrafanaCloudProvider — instantiate in `cmd/metric-plugin/main.go`
- `cmd/metric-plugin/main.go` — setupLogging() + handshakeConfig already present; fill in plugin map
- Established patterns: `WithBaseURL` option on provider (for testing), slog structured logging, httptest mock server

### Integration Points
- `cmd/metric-plugin/main.go` line ~30: empty `Plugins: map[string]goPlugin.Plugin{}` → replace with `"RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{Impl: impl}`
- `internal/provider/config.go` — add `Metric string json:"metric"` and `Aggregation string json:"aggregation,omitempty"`
- `internal/metric/metric.go` (new) — K6MetricProvider struct with Run/Resume/Terminate/GarbageCollect/Type/GetMetadata/InitPlugin methods

### Established Patterns
- `slog.With("provider", p.Name())` for structured logging (set in Phase 1)
- `var _ SomeInterface = (*SomeStruct)(nil)` compile-time interface check
- `WithBaseURL` option pattern for injecting test servers

</code_context>

<specifics>
## Specific Ideas

- For `metric: thresholds`, the value returned as `Measurement.Value` should be `"1"` (pass) or `"0"` (fail) so AnalysisTemplate can use `successCondition: result == "1"` or the raw numeric form.
- For `metric: http_req_failed`, value is a float string like `"0.023"` (fraction, 0.0–1.0). AnalysisTemplate: `successCondition: result < 0.05`.
- For `metric: http_req_duration`, value is milliseconds like `"234.5"`. AnalysisTemplate: `successCondition: result < 500`.
- For `metric: http_reqs`, value is req/s like `"142.3"`. AnalysisTemplate: `successCondition: result >= 100`.
- Live partial values during active runs come from Phase 1's D-03 (GetRunResult returns current metric values even during Running state).

</specifics>

<deferred>
## Deferred Ideas

- Implicit run sharing (in-memory testId → runId map) — reconsidered and deferred; explicit testRunId coordination is cleaner
- Custom k6 metric support (METR2-01) — v2 requirement, out of Phase 2 scope

</deferred>

---

*Phase: 02-metric-plugin*
*Context gathered: 2026-04-09*
