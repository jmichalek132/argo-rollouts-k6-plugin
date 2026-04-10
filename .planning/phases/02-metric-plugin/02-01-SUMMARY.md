---
phase: 02-metric-plugin
plan: 01
subsystem: metric-plugin
tags: [argo-rollouts, k6, metric-provider, rpc, v5-api, tdd]

# Dependency graph
requires:
  - phase: 01-foundation-provider
    provides: Provider interface, RunResult/RunState types, GrafanaCloudProvider, PluginConfig
provides:
  - K6MetricProvider implementing full MetricProviderPlugin interface (7 methods)
  - v5 aggregate metrics HTTP client (QueryAggregateMetric, parseAggregateValue)
  - Extended PluginConfig with Metric and Aggregation fields
  - Extended GetRunResult populating HTTPReqFailed/Duration/Reqs from v5 API
affects: [02-metric-plugin-wave2, 03-step-plugin]

# Tech tracking
tech-stack:
  added: [github.com/argoproj/argo-rollouts@v1.9.0, k8s.io/apimachinery]
  patterns: [stateless-metric-provider, measurement-metadata-state, v5-aggregate-http-client, graceful-degradation-v5]

key-files:
  created:
    - internal/metric/metric.go
    - internal/metric/metric_test.go
    - internal/provider/cloud/metrics.go
    - internal/provider/cloud/metrics_test.go
  modified:
    - internal/provider/config.go
    - internal/provider/cloud/cloud.go
    - go.mod
    - go.sum

key-decisions:
  - "Used metricutil.MarkMeasurementError for all error returns (sets Phase, Message, FinishedAt correctly)"
  - "v5 aggregate failures are gracefully degraded (logged at Warn, fields left at zero) rather than failing GetRunResult"
  - "extractMetricValue is an unexported helper for testability and single-responsibility"
  - "parseConfig validates all required fields upfront with clear error messages"

patterns-established:
  - "Stateless metric provider: all per-measurement state in Measurement.Metadata, no struct fields"
  - "Hand-written mock provider with function fields for testing (no framework)"
  - "testMetric() helper to build v1alpha1.Metric with plugin config JSON"
  - "Graceful degradation: v5 API failures logged but don't block v6 status/thresholds"

requirements-completed: [PLUG-01, METR-01, METR-02, METR-03, METR-04, METR-05, TEST-01, TEST-03]

# Metrics
duration: 7min
completed: 2026-04-10
---

# Phase 02 Plan 01: K6MetricProvider with Run/Resume lifecycle, 4 metric types, and v5 aggregate client

**K6MetricProvider implementing all 7 RpcMetricProvider methods with async Run/Resume lifecycle, 4 metric types (thresholds/http_req_failed/http_req_duration/http_reqs), v5 aggregate HTTP client, and 91.7% test coverage**

## Performance

- **Duration:** 7 min
- **Started:** 2026-04-10T08:05:37Z
- **Completed:** 2026-04-10T08:12:31Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments
- K6MetricProvider implements all 7 MetricProviderPlugin methods (InitPlugin, Run, Resume, Terminate, GarbageCollect, Type, GetMetadata)
- Run/Resume async lifecycle: Run() triggers or polls, Resume() extracts metric values and maps k6 states to Argo phases
- All 4 metric types working: thresholds ("1"/"0"), http_req_failed (float 0-1), http_req_duration (ms with p50/p95/p99), http_reqs (req/s)
- v5 aggregate metrics HTTP client populates HTTPReqFailed, HTTPReqDuration, HTTPReqs for terminal runs
- 53 tests total (27 metric + 26 cloud) all passing with -race flag, 91.7% coverage on internal/metric/

## Task Commits

Each task was committed atomically:

1. **Task 1: Wave 0 deps + PluginConfig extension + v5 aggregate metrics client** - `c7a7442` (feat)
2. **Task 2: K6MetricProvider implementation with full test suite** - `cca2683` (feat)

## Files Created/Modified
- `internal/metric/metric.go` - K6MetricProvider implementing MetricProviderPlugin interface (242 lines)
- `internal/metric/metric_test.go` - 27 unit tests covering all methods, metric types, states, config validation, concurrent safety (518 lines)
- `internal/provider/cloud/metrics.go` - v5 aggregate HTTP client with QueryAggregateMetric and parseAggregateValue (110 lines)
- `internal/provider/cloud/metrics_test.go` - 10 tests for v5 parsing, URL construction, auth headers, GetRunResult integration (314 lines)
- `internal/provider/config.go` - Added Metric and Aggregation fields to PluginConfig
- `internal/provider/cloud/cloud.go` - Extended GetRunResult to populate aggregate metrics from v5 API for terminal runs
- `go.mod` - Added github.com/argoproj/argo-rollouts@v1.9.0 with transitive k8s dependencies
- `.gitignore` - Added metric-plugin and step-plugin binary artifacts

## Decisions Made
- Used `metricutil.MarkMeasurementError` from argo-rollouts for all error returns -- sets Phase=Error, Message, FinishedAt correctly
- v5 aggregate failures are gracefully degraded: logged at Warn level, fields left at zero, GetRunResult still returns successfully. Rationale: v6 status/thresholds are the primary data; v5 metrics are supplementary
- extractMetricValue is an unexported function for single-responsibility and direct testability
- Concurrent safety achieved by design: K6MetricProvider struct has no mutable state fields, all per-measurement state in Measurement.Metadata

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed errcheck lint issue in metrics.go**
- **Found during:** Task 2 verification (lint check)
- **Issue:** `resp.Body.Close()` return value not checked in QueryAggregateMetric
- **Fix:** Changed `defer resp.Body.Close()` to `defer func() { _ = resp.Body.Close() }()`
- **Files modified:** internal/provider/cloud/metrics.go
- **Verification:** `golangci-lint run` passes with 0 issues
- **Committed in:** cca2683 (Task 2 commit)

**2. [Rule 1 - Bug] Fixed goconst lint issue in metric_test.go**
- **Found during:** Task 2 verification (lint check)
- **Issue:** String "http_req_duration" repeated 4 times in test file
- **Fix:** Extracted to `metricHTTPReqDuration` constant
- **Files modified:** internal/metric/metric_test.go
- **Verification:** `golangci-lint run` passes with 0 issues
- **Committed in:** cca2683 (Task 2 commit)

**3. [Rule 3 - Blocking] Added build artifacts to .gitignore**
- **Found during:** Task 2 commit preparation
- **Issue:** `go build ./cmd/metric-plugin` outputs binary to cwd, showing as untracked
- **Fix:** Added `metric-plugin` and `step-plugin` to `.gitignore`
- **Files modified:** .gitignore
- **Committed in:** cca2683 (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (2 bug fixes, 1 blocking)
**Impact on plan:** All auto-fixes necessary for lint compliance and clean git status. No scope creep.

## Issues Encountered
- argo-rollouts v1.9.0 was initially added to go.mod via `go get` but removed by `go mod tidy` because no code imported it yet. Resolved by creating the metric.go file first, then running `go mod tidy`.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- K6MetricProvider is ready to be wired into cmd/metric-plugin/main.go (Wave 2, plan 02-02)
- Provider interface unchanged -- step plugin (Phase 3) can use the same GrafanaCloudProvider
- All existing Phase 1 tests still pass (backward compatible)

## Self-Check: PASSED

- [x] internal/metric/metric.go exists
- [x] internal/metric/metric_test.go exists
- [x] internal/provider/cloud/metrics.go exists
- [x] internal/provider/cloud/metrics_test.go exists
- [x] Commit c7a7442 exists (Task 1)
- [x] Commit cca2683 exists (Task 2)

---
*Phase: 02-metric-plugin*
*Completed: 2026-04-10*
