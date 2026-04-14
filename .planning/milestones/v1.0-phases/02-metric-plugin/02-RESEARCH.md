# Phase 2: Metric Plugin - Research

**Researched:** 2026-04-09
**Domain:** RpcMetricProvider interface implementation, k6 Cloud v5 aggregate metrics API, Argo Rollouts measurement lifecycle
**Confidence:** HIGH

## Summary

Phase 2 implements the `K6MetricProvider` struct in `internal/metric/metric.go` that satisfies the Argo Rollouts `MetricProviderPlugin` interface (via `rpc.RpcMetricProviderPlugin`). The plugin uses the async `Run/Resume` lifecycle: `Run()` triggers a k6 test run (or attaches to an existing one if `testRunId` is provided), stores the runId in `Measurement.Metadata`, and returns `AnalysisPhaseRunning`. `Resume()` polls `GetRunResult()` and returns the requested metric value once the run reaches a terminal state -- or live partial values while running.

The k6 thresholds pass/fail is already available from the v6 API via `RunResult.ThresholdsPassed`. The three aggregate metrics (http_req_failed, http_req_duration, http_reqs) require hand-rolled `net/http` calls to the v5 API endpoint `GET /cloud/v5/test_runs/{id}/query_aggregate_k6(...)` because the `k6-cloud-openapi-client-go` v6 client does not expose aggregate metric endpoints. The v5 endpoint returns Prometheus-style vector results with timestamp-value pairs.

**Primary recommendation:** Implement `K6MetricProvider` with the Provider interface as a dependency injection point, add `Metric` and `Aggregation` fields to `PluginConfig`, build a thin `internal/provider/cloud/metrics.go` HTTP client for the v5 aggregate endpoint, and wire everything into `cmd/metric-plugin/main.go`. Add `github.com/argoproj/argo-rollouts v1.9.0` to go.mod.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Metric type specified via `metric` field in plugin config JSON. Valid values: `thresholds`, `http_req_failed`, `http_req_duration`, `http_reqs`.
- **D-02:** For `http_req_duration`, an `aggregation` field specifies the percentile: `p50`, `p95`, `p99`. Other metrics ignore `aggregation`.
- **D-03:** PluginConfig additions needed: `Metric string` (JSON: `metric`), `Aggregation string` (JSON: `aggregation`, omitempty).
- **D-04:** Trigger-or-poll mode -- if `testRunId` is provided, skip triggering and poll existing run directly. If only `testId`, `Run()` calls `TriggerRun`.
- **D-05:** `Measurement.Metadata` is the sole state store for runId across Run/Resume calls. Key: `"runId"`.
- **D-06:** Independent runs per Argo metric. No implicit run sharing across metrics.
- **D-07:** Users wanting multiple metrics from one run provide `testRunId` explicitly.
- **D-08:** k6 Errored -> Argo PhaseError. k6 Aborted -> Argo PhaseError.
- **D-09:** k6 Passed + metric within threshold -> Argo PhaseSuccessful. k6 Failed -> Argo PhaseFailed. k6 Running -> Argo PhaseRunning.
- **D-10:** Metadata keys: `runId`, `testRunURL`, `runState`, `metricValue` -- set on every Resume call.
- **D-11:** Metadata is set on each Resume call, not just terminal.
- **D-12:** K6MetricProvider in `internal/metric/metric.go`.
- **D-13:** `cmd/metric-plugin/main.go` registers `"RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{Impl: impl}`.
- **D-14:** GarbageCollect is a no-op for Phase 2.
- **D-15:** Terminate calls StopRun if runId present, returns PhaseError.
- **D-16:** Unit tests mock Provider interface (hand-written mock). Tests cover all paths.
- **D-17:** `go test -race -v -count=1 ./internal/metric/...` must pass. >=80% coverage.

### Claude's Discretion
- Exact error message strings and wrapping patterns
- Whether to use `context.WithTimeout` inside metric plugin or rely on cfg.Timeout
- Internal helper functions (e.g., `extractMetricValue(*RunResult, metricType, aggregation) (float64, error)`)
- Exact Measurement.Metadata key names (as long as they match D-10/D-11 semantics)

### Deferred Ideas (OUT OF SCOPE)
- Implicit run sharing (in-memory testId -> runId map)
- Custom k6 metric support (METR2-01) -- v2 requirement
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PLUG-01 | Metric plugin binary implements full RpcMetricProvider interface (InitPlugin, Run, Resume, Terminate, GarbageCollect, Type, GetMetadata) | Exact interface signatures verified from argo-rollouts v1.9.0 source. MetricProviderPlugin = InitPlugin() + RpcMetricProvider (7 methods total). |
| METR-01 | Return k6 threshold pass/fail as boolean (`metric: thresholds`) | Already available from RunResult.ThresholdsPassed (v6 API). Return "1" (pass) or "0" (fail) as Measurement.Value. |
| METR-02 | Return HTTP error rate as float 0.0-1.0 (`metric: http_req_failed`) | Requires v5 aggregate API: `query_aggregate_k6(metric='http_req_failed',query='rate')`. http_req_failed is a k6 Rate metric. |
| METR-03 | Return HTTP latency percentiles in ms (`metric: http_req_duration`, `aggregation: p50\|p95\|p99`) | Requires v5 aggregate API: `query_aggregate_k6(metric='http_req_duration',query='histogram_quantile(0.95)')`. http_req_duration is a k6 Trend metric. |
| METR-04 | Return HTTP throughput as req/s (`metric: http_reqs`, `aggregation: rate`) | Requires v5 aggregate API: `query_aggregate_k6(metric='http_reqs',query='rate')`. http_reqs is a k6 Counter metric. |
| METR-05 | Return test run URL and status in Measurement.Metadata for kubectl debugging | D-10/D-11: set runId, testRunURL, runState, metricValue on every measurement. |
| TEST-01 | Unit tests >=80% coverage on internal packages | Hand-written mock Provider, test all config combos, Run/Resume paths, error paths. |
| TEST-03 | Concurrent AnalysisRun test -- no cross-contamination (`go test -race`) | Plugin is stateless by design (all state in Measurement.Metadata). Race test verifies no shared mutable state. |
</phase_requirements>

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | 1.24 (go.mod) / 1.26.1 (system) | Language runtime | Already in go.mod. argo-rollouts v1.9.0 requires go 1.24.9+. |
| `github.com/argoproj/argo-rollouts` | v1.9.0 | Plugin interface types, v1alpha1 CRD types, RPC wrapper | **Must be added to go.mod.** Provides `MetricProviderPlugin` interface, `v1alpha1.Measurement`, `v1alpha1.AnalysisPhase*` constants, `metricproviders/plugin/rpc.RpcMetricProviderPlugin`. |
| `github.com/hashicorp/go-plugin` | v1.6.3 | Plugin host framework | Already in go.mod. Matches argo-rollouts v1.9.0 dependency. |
| `github.com/grafana/k6-cloud-openapi-client-go` | v0.0.0-20251022100644 | k6 Cloud v6 API client | Already in go.mod. Used by GrafanaCloudProvider for test run lifecycle. |
| `log/slog` | stdlib | Structured logging | Locked decision D-12 from Phase 1. Already in use. |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `net/http` | stdlib | Hand-rolled v5 aggregate metrics client | Querying `/cloud/v5/test_runs/{id}/query_aggregate_k6(...)` for METR-02/03/04 |
| `encoding/json` | stdlib | Config parsing + v5 API response parsing | Parsing PluginConfig from `metric.Provider.Plugin`, parsing v5 API responses |
| `fmt` | stdlib | Value formatting | `strconv.FormatFloat` or `fmt.Sprintf` for Measurement.Value strings |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions | Already in go.mod. Unit tests. |
| `github.com/argoproj/argo-rollouts/utils/metric` | (from v1.9.0) | `MarkMeasurementError` helper | Marking error measurements with proper Phase + FinishedAt timestamp |

### Adding argo-rollouts to go.mod

```bash
GOPATH="$HOME/go" go get github.com/argoproj/argo-rollouts@v1.9.0
GOPATH="$HOME/go" go mod tidy
```

This will pull in a large transitive dependency tree (k8s client-go, api-machinery, etc.). Expected.

## Architecture Patterns

### Project Structure (Phase 2 additions)

```
internal/
  metric/
    metric.go           # K6MetricProvider struct implementing MetricProviderPlugin
    metric_test.go      # Unit tests with mock Provider
  provider/
    config.go           # ADD Metric + Aggregation fields
    cloud/
      metrics.go        # Hand-rolled v5 aggregate metrics HTTP client
      metrics_test.go   # Tests for v5 aggregate endpoint parsing
cmd/
  metric-plugin/
    main.go             # Fill plugin map with RpcMetricProviderPlugin
```

### Pattern 1: MetricProviderPlugin Implementation

**What:** K6MetricProvider struct that implements the `rpc.MetricProviderPlugin` interface (which embeds `types.RpcMetricProvider` + `InitPlugin()`).

**When to use:** This is the core implementation pattern for Phase 2.

**Exact interface to implement (verified from argo-rollouts v1.9.0 source):**

```go
// rpc.MetricProviderPlugin = InitPlugin() + types.RpcMetricProvider
type MetricProviderPlugin interface {
    InitPlugin() types.RpcError
    types.RpcMetricProvider
}

// types.RpcMetricProvider (utils/plugin/types/types.go)
type RpcMetricProvider interface {
    Run(*v1alpha1.AnalysisRun, v1alpha1.Metric) v1alpha1.Measurement
    Resume(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
    Terminate(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
    GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError
    Type() string
    GetMetadata(metric v1alpha1.Metric) map[string]string
}
```

**Implementation skeleton:**

```go
// internal/metric/metric.go
package metric

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "strconv"
    "time"

    "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
    "github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
    "github.com/argoproj/argo-rollouts/utils/plugin/types"
    metricutil "github.com/argoproj/argo-rollouts/utils/metric"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    "github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Compile-time interface check.
var _ rpc.MetricProviderPlugin = (*K6MetricProvider)(nil)

// pluginName must match the name in argo-rollouts-config ConfigMap.
const pluginName = "jmichalek132/k6"

type K6MetricProvider struct {
    provider provider.Provider
}

func New(p provider.Provider) *K6MetricProvider {
    return &K6MetricProvider{provider: p}
}
```

**Source:** [argo-rollouts v1.9.0 utils/plugin/types/types.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/utils/plugin/types/types.go), [metricproviders/plugin/rpc/rpc.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/metricproviders/plugin/rpc/rpc.go)

### Pattern 2: Key Type Definitions (Argo Rollouts v1alpha1)

**Measurement struct (what Run/Resume/Terminate return):**

```go
type Measurement struct {
    Phase      AnalysisPhase     `json:"phase"`
    Message    string            `json:"message,omitempty"`
    StartedAt  *metav1.Time      `json:"startedAt,omitempty"`
    FinishedAt *metav1.Time      `json:"finishedAt,omitempty"`
    Value      string            `json:"value,omitempty"`
    Metadata   map[string]string `json:"metadata,omitempty"`
    ResumeAt   *metav1.Time      `json:"resumeAt,omitempty"`
}
```

**AnalysisPhase constants:**

```go
const (
    AnalysisPhasePending      AnalysisPhase = "Pending"
    AnalysisPhaseRunning      AnalysisPhase = "Running"
    AnalysisPhaseSuccessful   AnalysisPhase = "Successful"
    AnalysisPhaseFailed       AnalysisPhase = "Failed"
    AnalysisPhaseError        AnalysisPhase = "Error"
    AnalysisPhaseInconclusive AnalysisPhase = "Inconclusive"
)
```

**MetricProvider.Plugin field (how config is accessed):**

```go
type MetricProvider struct {
    // ... other providers ...
    Plugin map[string]json.RawMessage `json:"plugin,omitempty"`
}
```

**Source:** [argo-rollouts v1.9.0 pkg/apis/rollouts/v1alpha1/analysis_types.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/pkg/apis/rollouts/v1alpha1/analysis_types.go)

### Pattern 3: Config Parsing from AnalysisTemplate

**What:** Parse the plugin-specific config from `metric.Provider.Plugin["pluginName"]`.

```go
func (k *K6MetricProvider) parseConfig(metric v1alpha1.Metric) (*provider.PluginConfig, error) {
    rawCfg, ok := metric.Provider.Plugin[pluginName]
    if !ok {
        return nil, fmt.Errorf("plugin config not found for %s", pluginName)
    }
    var cfg provider.PluginConfig
    if err := json.Unmarshal(rawCfg, &cfg); err != nil {
        return nil, fmt.Errorf("unmarshal plugin config: %w", err)
    }
    // Validate required fields
    if cfg.TestID == "" && cfg.TestRunID == "" {
        return nil, fmt.Errorf("either testId or testRunId is required")
    }
    if cfg.APIToken == "" {
        return nil, fmt.Errorf("apiToken is required (check Secret reference)")
    }
    if cfg.StackID == "" {
        return nil, fmt.Errorf("stackId is required (check Secret reference)")
    }
    if cfg.Metric == "" {
        return nil, fmt.Errorf("metric field is required (thresholds|http_req_failed|http_req_duration|http_reqs)")
    }
    return &cfg, nil
}
```

### Pattern 4: Run/Resume Lifecycle

**Run() -- trigger or attach:**

```go
func (k *K6MetricProvider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
    startedAt := metav1.Now()
    measurement := v1alpha1.Measurement{
        Phase:     v1alpha1.AnalysisPhaseRunning,
        StartedAt: &startedAt,
        Metadata:  map[string]string{},
    }

    cfg, err := k.parseConfig(metric)
    if err != nil {
        return metricutil.MarkMeasurementError(measurement, err)
    }

    ctx := context.Background()
    var runID string

    if cfg.TestRunID != "" {
        // Poll-only mode: use provided testRunId
        runID = cfg.TestRunID
    } else {
        // Trigger mode: start a new run
        runID, err = k.provider.TriggerRun(ctx, cfg)
        if err != nil {
            return metricutil.MarkMeasurementError(measurement, err)
        }
    }

    measurement.Metadata["runId"] = runID
    slog.Info("metric plugin Run", "runId", runID, "metric", cfg.Metric)
    return measurement
}
```

**Resume() -- poll and extract metric:**

```go
func (k *K6MetricProvider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
    cfg, err := k.parseConfig(metric)
    if err != nil {
        return metricutil.MarkMeasurementError(measurement, err)
    }

    runID := measurement.Metadata["runId"]
    if runID == "" {
        return metricutil.MarkMeasurementError(measurement, fmt.Errorf("runId not found in metadata"))
    }

    ctx := context.Background()
    result, err := k.provider.GetRunResult(ctx, cfg, runID)
    if err != nil {
        return metricutil.MarkMeasurementError(measurement, err)
    }

    // Always update metadata (D-11/D-12)
    measurement.Metadata["testRunURL"] = result.TestRunURL
    measurement.Metadata["runState"] = string(result.State)

    // Extract metric value
    value, err := extractMetricValue(result, cfg.Metric, cfg.Aggregation)
    if err != nil {
        return metricutil.MarkMeasurementError(measurement, err)
    }
    valueStr := strconv.FormatFloat(value, 'f', -1, 64)
    measurement.Value = valueStr
    measurement.Metadata["metricValue"] = valueStr

    // Map state to phase
    switch result.State {
    case provider.Running:
        measurement.Phase = v1alpha1.AnalysisPhaseRunning
    case provider.Passed:
        measurement.Phase = v1alpha1.AnalysisPhaseSuccessful
        finishedAt := metav1.Now()
        measurement.FinishedAt = &finishedAt
    case provider.Failed:
        measurement.Phase = v1alpha1.AnalysisPhaseFailed
        finishedAt := metav1.Now()
        measurement.FinishedAt = &finishedAt
    case provider.Errored, provider.Aborted:
        measurement.Phase = v1alpha1.AnalysisPhaseError
        measurement.Message = fmt.Sprintf("k6 run %s: %s", result.State, runID)
        finishedAt := metav1.Now()
        measurement.FinishedAt = &finishedAt
    }

    return measurement
}
```

### Pattern 5: Metric Value Extraction

```go
func extractMetricValue(result *provider.RunResult, metricType, aggregation string) (float64, error) {
    switch metricType {
    case "thresholds":
        if result.ThresholdsPassed {
            return 1, nil
        }
        return 0, nil
    case "http_req_failed":
        return result.HTTPReqFailed, nil
    case "http_req_duration":
        switch aggregation {
        case "p50":
            return result.HTTPReqDuration.P50, nil
        case "p95":
            return result.HTTPReqDuration.P95, nil
        case "p99":
            return result.HTTPReqDuration.P99, nil
        case "":
            return 0, fmt.Errorf("aggregation is required for http_req_duration (p50|p95|p99)")
        default:
            return 0, fmt.Errorf("unsupported aggregation %q for http_req_duration (use p50|p95|p99)", aggregation)
        }
    case "http_reqs":
        return result.HTTPReqs, nil
    default:
        return 0, fmt.Errorf("unsupported metric type %q (use thresholds|http_req_failed|http_req_duration|http_reqs)", metricType)
    }
}
```

### Pattern 6: main.go Wiring

```go
// cmd/metric-plugin/main.go
import (
    rolloutsPlugin "github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
    goPlugin "github.com/hashicorp/go-plugin"

    "github.com/jmichalek132/argo-rollouts-k6-plugin/internal/metric"
    "github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/cloud"
)

func main() {
    setupLogging()

    p := cloud.NewGrafanaCloudProvider()
    impl := metric.New(p)

    goPlugin.Serve(&goPlugin.ServeConfig{
        HandshakeConfig: handshakeConfig,
        Plugins: map[string]goPlugin.Plugin{
            "RpcMetricProviderPlugin": &rolloutsPlugin.RpcMetricProviderPlugin{Impl: impl},
        },
    })
}
```

**Source:** [rollouts-plugin-metric-sample-prometheus main.go](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus/blob/main/main.go)

### Anti-Patterns to Avoid

- **Storing state in K6MetricProvider struct fields:** All state between Run/Resume lives in `Measurement.Metadata`. The struct must be stateless for concurrent safety (Pitfall 7 from PITFALLS.md).
- **Triggering k6 run in Resume():** Only `Run()` triggers. `Resume()` only polls. Never trigger a second run.
- **Using `metric.Provider.Plugin` with wrong key:** The key must match the plugin name registered in `argo-rollouts-config` ConfigMap exactly.
- **Returning AnalysisPhaseSuccessful/Failed from Run():** `Run()` should almost always return `AnalysisPhaseRunning` with metadata. The controller calls `Resume()` for the actual result.
- **Not setting FinishedAt on terminal measurements:** The controller expects `FinishedAt` to be set on terminal phases. Use `metav1.Now()`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Measurement error helper | Custom error-to-measurement logic | `metricutil.MarkMeasurementError(m, err)` from argo-rollouts | Sets Phase=Error, Message=err.Error(), FinishedAt correctly |
| k6 test run lifecycle | Direct HTTP for trigger/poll/stop | `provider.Provider` interface + `cloud.GrafanaCloudProvider` | Already built in Phase 1, tested, handles auth |
| Plugin RPC transport | Custom net/rpc server | `rpc.RpcMetricProviderPlugin{Impl: impl}` | Argo Rollouts provides the server wrapper; just supply the Impl |
| Config parsing boilerplate | Manual JSON decode per method | Single `parseConfig()` helper called from Run/Resume/Terminate | DRY, consistent validation |

**Key insight:** The k6-cloud-openapi-client-go covers test run lifecycle but NOT aggregate metric queries. For METR-02/03/04, we must use hand-rolled `net/http` to the v5 API. For METR-01 (thresholds), `RunResult.ThresholdsPassed` from the v6 API is sufficient.

## Grafana Cloud k6 v5 Aggregate Metrics API

**Confidence: MEDIUM** -- Verified from Grafana docs and API reference. Exact response field names need runtime validation.

### Endpoint

```
GET https://api.k6.io/cloud/v5/test_runs/{testRunId}/query_aggregate_k6({parameters})
```

Parameters are a comma-separated list in `key=value` format. String values must be single-quoted.

### Authentication

Same as v6 API: `Authorization: Bearer <token>` + `X-Stack-Id: <stack-id>`.

**Confidence: HIGH** -- The v5 and v6 APIs share the same auth infrastructure. Bearer token confirmed in Phase 1 research.

### Query Examples for Each Metric Type

**http_req_failed (Rate metric -- fraction of failed requests):**
```
GET /cloud/v5/test_runs/{id}/query_aggregate_k6(metric='http_req_failed',query='rate')
```

**http_req_duration (Trend metric -- percentile in ms):**
```
GET /cloud/v5/test_runs/{id}/query_aggregate_k6(metric='http_req_duration',query='histogram_quantile(0.95)')
GET /cloud/v5/test_runs/{id}/query_aggregate_k6(metric='http_req_duration',query='histogram_quantile(0.50)')
GET /cloud/v5/test_runs/{id}/query_aggregate_k6(metric='http_req_duration',query='histogram_quantile(0.99)')
```

**http_reqs (Counter metric -- requests per second):**
```
GET /cloud/v5/test_runs/{id}/query_aggregate_k6(metric='http_reqs',query='rate')
```

### Response Format (Prometheus-style vector)

```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {
          "__name__": "http_req_duration",
          "test_run_id": "152779"
        },
        "values": [
          [1684950639, 14207]
        ]
      }
    ]
  }
}
```

The value at `data.result[0].values[0][1]` is the aggregate numeric value. For overall aggregation (no `by (...)` clause), there should be exactly one result entry.

### Implementation: internal/provider/cloud/metrics.go

```go
// AggregateMetricQuery defines a v5 aggregate metric query.
type AggregateMetricQuery struct {
    MetricName string // "http_req_failed", "http_req_duration", "http_reqs"
    QueryFunc  string // "rate", "histogram_quantile(0.95)", etc.
}

// QueryAggregateMetric calls the v5 aggregate endpoint.
func (p *GrafanaCloudProvider) QueryAggregateMetric(
    ctx context.Context,
    cfg *provider.PluginConfig,
    runID string,
    query AggregateMetricQuery,
) (float64, error) {
    // Build URL: baseURL/cloud/v5/test_runs/{runID}/query_aggregate_k6(metric='{name}',query='{func}')
    // Auth: Bearer token + X-Stack-Id headers
    // Parse response: data.result[0].values[0][1]
}
```

The base URL for v5 calls should default to `https://api.k6.io` and be overridable via the same `WithBaseURL` option already on GrafanaCloudProvider (for test mocking).

### Extending RunResult with v5 Metrics

Two approaches:

**Option A (recommended):** Extend `GetRunResult` to also call the v5 aggregate endpoint and populate HTTPReqFailed, HTTPReqDuration, HTTPReqs fields in RunResult. Currently these are zero-valued.

**Option B:** Add a separate `QueryAggregateMetric` method to the Provider interface and call it from K6MetricProvider.Resume() separately.

**Recommendation: Option A.** Keep the Provider interface at 4 methods (D-01 from Phase 1). Extend `GetRunResult` in GrafanaCloudProvider to call v5 aggregate for all three metric types in parallel, populating the existing RunResult fields. This is a backward-compatible change -- the RunResult struct already has the fields; they just get populated now.

### Important: Only query v5 when run is completed

The v5 aggregate metrics are only reliable after the test run finishes (`state == completed`). During `Running` state, partial values may be available but the aggregate may not be computed. The implementation should:
1. If `state == Running` or other non-terminal: return zero values for HTTPReqFailed/HTTPReqDuration/HTTPReqs (or attempt the query and gracefully handle empty results).
2. If `state == completed`: query all three aggregate metrics and populate RunResult.

This means `extractMetricValue` for METR-02/03/04 may return 0.0 during active runs. The thresholds metric (METR-01) is always available from the v6 API.

## PluginConfig Extensions

Add to `internal/provider/config.go`:

```go
type PluginConfig struct {
    TestRunID   string `json:"testRunId,omitempty"`
    TestID      string `json:"testId"`
    APIToken    string `json:"apiToken"`
    StackID     string `json:"stackId"`
    Timeout     string `json:"timeout,omitempty"`
    Metric      string `json:"metric"`                  // NEW: thresholds|http_req_failed|http_req_duration|http_reqs
    Aggregation string `json:"aggregation,omitempty"`    // NEW: p50|p95|p99 (for http_req_duration)
}
```

## Common Pitfalls

### Pitfall 1: Non-Idempotent Run() Triggers Duplicate k6 Runs

**What goes wrong:** The controller may call `Run()` multiple times during reconciliation. If `Run()` always calls `TriggerRun()`, it creates duplicate k6 test runs.
**Why it happens:** The controller reconciles on a schedule and can call `Run()` again if the first measurement hasn't been persisted yet.
**How to avoid:** In `Run()`, return `AnalysisPhaseRunning` with the runId in Metadata. The controller only calls `Run()` once per measurement cycle -- if the Measurement is already `Running`, it calls `Resume()` next. But if the AnalysisRun is re-created, a new `Run()` is called. This is by design -- a new AnalysisRun means a new measurement.
**Warning signs:** Multiple k6 test runs appear in Grafana Cloud for a single AnalysisRun.

### Pitfall 2: Concurrent Plugin Calls Share Mutable State

**What goes wrong:** K6MetricProvider is a single instance serving all concurrent AnalysisRuns. Storing anything in struct fields (maps, caches) without synchronization causes data races.
**Why it happens:** The go-plugin process is long-lived; each RPC call is a separate goroutine.
**How to avoid:** K6MetricProvider struct must be stateless. All per-measurement state goes in `Measurement.Metadata`. The Provider interface is stateless (creates HTTP clients per call). No shared mutable state exists by design.
**Warning signs:** `go test -race` failures; flaky measurement results under load.

### Pitfall 3: Missing FinishedAt on Terminal Measurements

**What goes wrong:** Returning a terminal phase (Successful/Failed/Error) without setting `FinishedAt` causes the controller to treat the measurement as incomplete.
**Why it happens:** Easy to forget when constructing Measurement manually.
**How to avoid:** Use `metricutil.MarkMeasurementError()` for errors (it sets FinishedAt). For Successful/Failed, always set `FinishedAt = &metav1.Now()`.
**Warning signs:** AnalysisRun stuck in "Running" despite plugin returning terminal phase.

### Pitfall 4: Wrong Plugin Key in metric.Provider.Plugin

**What goes wrong:** `metric.Provider.Plugin["wrong-name"]` returns nil, and the plugin returns an error for every measurement.
**Why it happens:** The key must exactly match the plugin name in `argo-rollouts-config` ConfigMap.
**How to avoid:** Define `pluginName` as a constant, use it everywhere. Test with the actual key used in example AnalysisTemplates.
**Warning signs:** All measurements fail with "plugin config not found".

### Pitfall 5: v5 API Returns Empty Results for Running Tests

**What goes wrong:** Querying aggregate metrics while a test is still running returns empty `data.result` array or zero values.
**Why it happens:** The v5 aggregate endpoint computes metrics over the full test duration. During execution, aggregation may not be computed.
**How to avoid:** For running tests, return partial values (0.0) for aggregate metrics. Only the thresholds metric is reliable during execution (from v6 API). Document this behavior.
**Warning signs:** Metric values are 0 during active runs, jump to real values at completion.

### Pitfall 6: go.mod Dependency Explosion When Adding argo-rollouts

**What goes wrong:** Adding `github.com/argoproj/argo-rollouts v1.9.0` pulls in k8s client-go, api-machinery, and hundreds of transitive dependencies. Build times increase. go.sum becomes huge.
**Why it happens:** argo-rollouts depends on the entire Kubernetes API surface.
**How to avoid:** This is unavoidable -- the plugin must import v1alpha1 types. Just run `go mod tidy` and commit the result. Binary size will be ~100MB+ with debug info, ~40MB with `-ldflags "-s -w"`.
**Warning signs:** Long `go mod tidy` time on first run.

## Code Examples

### MarkMeasurementError Helper

```go
// Source: github.com/argoproj/argo-rollouts/utils/metric/metric.go
func MarkMeasurementError(m v1alpha1.Measurement, err error) v1alpha1.Measurement {
    m.Phase = v1alpha1.AnalysisPhaseError
    m.Message = err.Error()
    if m.FinishedAt == nil {
        now := timeutil.MetaNow()
        m.FinishedAt = &now
    }
    return m
}
```

This is a value-receiver function (takes Measurement by value, returns modified copy). Safe for concurrent use.

### v5 Aggregate Response Parsing

```go
type aggregateResponse struct {
    Status string `json:"status"`
    Data   struct {
        ResultType string `json:"resultType"`
        Result     []struct {
            Metric map[string]string `json:"metric"`
            Values [][]interface{}   `json:"values"` // [[timestamp, value], ...]
        } `json:"result"`
    } `json:"data"`
}

func parseAggregateValue(body []byte) (float64, error) {
    var resp aggregateResponse
    if err := json.Unmarshal(body, &resp); err != nil {
        return 0, fmt.Errorf("parse aggregate response: %w", err)
    }
    if resp.Status != "success" {
        return 0, fmt.Errorf("aggregate query failed: status=%s", resp.Status)
    }
    if len(resp.Data.Result) == 0 {
        return 0, nil // No data yet (test still running or no traffic)
    }
    values := resp.Data.Result[0].Values
    if len(values) == 0 || len(values[0]) < 2 {
        return 0, nil
    }
    // Value is a number (may be float or int in JSON)
    switch v := values[0][1].(type) {
    case float64:
        return v, nil
    case string:
        return strconv.ParseFloat(v, 64)
    default:
        return 0, fmt.Errorf("unexpected value type %T", v)
    }
}
```

### Test Pattern: Hand-Written Mock Provider

```go
// internal/metric/metric_test.go
type mockProvider struct {
    triggerRunFn   func(ctx context.Context, cfg *provider.PluginConfig) (string, error)
    getRunResultFn func(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error)
    stopRunFn      func(ctx context.Context, cfg *provider.PluginConfig, runID string) error
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
    if m.triggerRunFn != nil {
        return m.triggerRunFn(ctx, cfg)
    }
    return "mock-run-123", nil
}

func (m *mockProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
    if m.getRunResultFn != nil {
        return m.getRunResultFn(ctx, cfg, runID)
    }
    return &provider.RunResult{State: provider.Running}, nil
}

func (m *mockProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
    if m.stopRunFn != nil {
        return m.stopRunFn(ctx, cfg, runID)
    }
    return nil
}
```

### Test Pattern: Building v1alpha1.Metric with Plugin Config

```go
func testMetric(cfg map[string]interface{}) v1alpha1.Metric {
    rawCfg, _ := json.Marshal(cfg)
    return v1alpha1.Metric{
        Name: "k6-test",
        Provider: v1alpha1.MetricProvider{
            Plugin: map[string]json.RawMessage{
                pluginName: rawCfg,
            },
        },
    }
}

// Usage:
metric := testMetric(map[string]interface{}{
    "testId":   "42",
    "apiToken": "test-token",
    "stackId":  "12345",
    "metric":   "thresholds",
})
```

### Test Pattern: Concurrent Race Detection

```go
func TestResume_ConcurrentSafety(t *testing.T) {
    mock := &mockProvider{
        getRunResultFn: func(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
            return &provider.RunResult{
                State:            provider.Passed,
                ThresholdsPassed: true,
                TestRunURL:       "https://app.k6.io/runs/" + runID,
            }, nil
        },
    }
    k := metric.New(mock)

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            runID := fmt.Sprintf("run-%d", idx)
            m := v1alpha1.Measurement{
                Phase:    v1alpha1.AnalysisPhaseRunning,
                Metadata: map[string]string{"runId": runID},
            }
            mt := testMetric(map[string]interface{}{
                "testId":   "42",
                "apiToken": "test-token",
                "stackId":  "12345",
                "metric":   "thresholds",
            })
            result := k.Resume(nil, mt, m)
            assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, result.Phase)
            assert.Equal(t, runID, result.Metadata["runId"])
        }(i)
    }
    wg.Wait()
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| k6 API auth: `Token <key>` | k6 API auth: `Bearer <key>` | v6 API (2024) | Auth handled by k6-cloud-openapi-client-go automatically |
| Numeric status codes (v2 API) | String status types (v6 API) | v6 API (2024) | mapToRunState uses string matching, already implemented in Phase 1 |
| Manual threshold check via API | ThresholdsPassed bool from v6 API | v6 API (2024) | METR-01 is trivial -- just read RunResult.ThresholdsPassed |
| logrus (ecosystem convention) | slog (stdlib) | Phase 1 D-12 decision | No external logging dependency |

## Open Questions

1. **v5 API response for running tests**
   - What we know: The aggregate endpoint returns Prometheus-style vector results for completed tests.
   - What's unclear: Whether it returns partial data during active runs or an empty result set.
   - Recommendation: Handle empty results gracefully (return 0.0 for aggregate metrics while running). Thresholds via v6 API are always available. Document that aggregate metrics (METR-02/03/04) are only accurate after test completion.

2. **Exact v5 API base URL consistency**
   - What we know: v6 API is at `api.k6.io/cloud/v6/`. The v5 path is `/cloud/v5/`.
   - What's unclear: Whether the same base URL works for both v5 and v6.
   - Recommendation: Use the same base URL for both. The `WithBaseURL` option covers testing. If the paths diverge in production, the provider can construct them separately.

3. **Plugin name string**
   - What we know: Must match ConfigMap entry. Prometheus sample uses `argoproj-labs/sample-prometheus`.
   - What's unclear: The exact org/name the project will use.
   - Recommendation: Use `jmichalek132/k6` as the plugin name constant. This can be changed later before v1 release. Define as a package-level const.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | Build, test | Yes | 1.26.1 | -- |
| golangci-lint | Lint | Yes (from Phase 1 setup) | v2.x | -- |
| make | Build | Yes | System | -- |
| testify | Tests | Yes | v1.11.1 (in go.mod) | -- |
| argo-rollouts module | Plugin types | Not yet in go.mod | v1.9.0 (to add) | Must `go get` |

**Missing dependencies with no fallback:**
- `github.com/argoproj/argo-rollouts v1.9.0` must be added to go.mod before any Phase 2 code compiles.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go testing + testify v1.11.1 |
| Config file | None (go test uses convention) |
| Quick run command | `GOPATH="$HOME/go" go test -race -v -count=1 ./internal/metric/...` |
| Full suite command | `GOPATH="$HOME/go" go test -race -v -count=1 -coverprofile=coverage.out ./internal/...` |
| Coverage check | `GOPATH="$HOME/go" go tool cover -func=coverage.out \| grep total` |

### Phase Requirements -> Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PLUG-01 | All 7 MetricProviderPlugin methods implemented | unit | `go test -v -run TestInitPlugin\|TestRun\|TestResume\|TestTerminate\|TestGarbageCollect\|TestType\|TestGetMetadata ./internal/metric/...` | Wave 0 |
| METR-01 | Thresholds metric returns "1" or "0" | unit | `go test -v -run TestResume_Thresholds ./internal/metric/...` | Wave 0 |
| METR-02 | HTTP error rate returns float 0.0-1.0 | unit | `go test -v -run TestResume_HTTPReqFailed ./internal/metric/...` | Wave 0 |
| METR-03 | HTTP latency percentiles in ms | unit | `go test -v -run TestResume_HTTPReqDuration ./internal/metric/...` | Wave 0 |
| METR-04 | HTTP throughput as req/s | unit | `go test -v -run TestResume_HTTPReqs ./internal/metric/...` | Wave 0 |
| METR-05 | Metadata contains testRunURL and status | unit | `go test -v -run TestResume.*Metadata ./internal/metric/...` | Wave 0 |
| TEST-01 | >=80% coverage on internal/metric | unit + coverage | `go test -race -coverprofile=c.out ./internal/metric/... && go tool cover -func=c.out` | Wave 0 |
| TEST-03 | No concurrent cross-contamination | race | `go test -race -v -run TestResume_ConcurrentSafety ./internal/metric/...` | Wave 0 |

### Sampling Rate

- **Per task commit:** `GOPATH="$HOME/go" go test -race -v -count=1 ./internal/metric/...`
- **Per wave merge:** `GOPATH="$HOME/go" go test -race -v -count=1 -coverprofile=coverage.out ./internal/... && GOPATH="$HOME/go" go tool cover -func=coverage.out`
- **Phase gate:** Full suite green + >=80% coverage on `internal/metric/` before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `internal/metric/metric_test.go` -- all unit tests for K6MetricProvider (PLUG-01, METR-01--05, TEST-01, TEST-03)
- [ ] `internal/provider/cloud/metrics_test.go` -- tests for v5 aggregate endpoint parsing
- [ ] `github.com/argoproj/argo-rollouts v1.9.0` added to go.mod -- required before any test compiles

**Estimated test runtime:** < 5 seconds for unit tests (no network, all mocked). Race detection adds ~2x overhead. Total: ~10 seconds.

## Sources

### Primary (HIGH confidence)
- [argo-rollouts v1.9.0 utils/plugin/types/types.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/utils/plugin/types/types.go) -- RpcMetricProvider interface, RpcError type
- [argo-rollouts v1.9.0 metricproviders/plugin/rpc/rpc.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/metricproviders/plugin/rpc/rpc.go) -- MetricProviderPlugin interface, RpcMetricProviderPlugin struct
- [argo-rollouts v1.9.0 analysis_types.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/pkg/apis/rollouts/v1alpha1/analysis_types.go) -- Measurement struct, AnalysisPhase constants, MetricProvider.Plugin field
- [argo-rollouts v1.9.0 utils/metric/metric.go](https://github.com/argoproj/argo-rollouts/blob/v1.9.0/utils/metric/metric.go) -- MarkMeasurementError helper
- [rollouts-plugin-metric-sample-prometheus](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus) -- Reference implementation for wiring main.go, config parsing pattern
- Existing codebase: `internal/provider/provider.go`, `internal/provider/cloud/cloud.go`, `cmd/metric-plugin/main.go`

### Secondary (MEDIUM confidence)
- [Grafana Cloud k6 Metrics REST API](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/metrics/) -- v5 query_aggregate_k6 endpoint docs
- [Grafana Cloud k6 aggregation methods](https://grafana.com/docs/grafana-cloud/testing/k6/reference/query-types/metric-query-aggregation-methods/) -- histogram_quantile, rate query syntax
- [k6 built-in metrics reference](https://grafana.com/docs/k6/latest/using-k6/metrics/reference/) -- http_req_failed (Rate), http_req_duration (Trend), http_reqs (Counter)

### Tertiary (LOW confidence)
- v5 aggregate response JSON format -- verified from docs/search but needs runtime validation with actual API calls

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all libraries verified in go.mod or argo-rollouts source
- Architecture: HIGH -- interface signatures verified from v1.9.0 source, sample plugin pattern confirmed
- Pitfalls: HIGH -- from PITFALLS.md + verified against actual interface contracts
- v5 aggregate API: MEDIUM -- endpoint format confirmed from docs but response parsing needs runtime validation

**Research date:** 2026-04-09
**Valid until:** 2026-05-09 (stable libraries, interface unlikely to change within argo-rollouts v1.9.x)
