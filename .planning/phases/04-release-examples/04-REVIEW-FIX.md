---
phase: "04"
fixed_at: 2026-04-14T12:00:00Z
review_path: .planning/phases/04-release-examples/04-REVIEW.md
iteration: 1
findings_in_scope: 7
fixed: 7
skipped: 0
status: all_fixed
---

# Phase 04: Code Review Fix Report

**Fixed at:** 2026-04-14T12:00:00Z
**Source review:** .planning/phases/04-release-examples/04-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 7
- Fixed: 7
- Skipped: 0

## Fixed Issues

### C-01: URL injection via unsanitized metric name and query function in v5 API call

**Files modified:** `internal/provider/cloud/metrics.go`
**Commit:** 096af37 (validation), 8cedb9a (lint fix)
**Applied fix:** Added `isValidMetricParam()` validation function that rejects any MetricName or QueryFunc containing characters outside `[a-zA-Z0-9_.()]`. Validation runs before URL interpolation in `QueryAggregateMetric`. Returns descriptive error on invalid input. Follow-up commit restructured the condition to satisfy staticcheck QF1001 (De Morgan's law).

### W-01: Metric plugin Terminate panics on nil Metadata map

**Files modified:** `internal/metric/metric.go`
**Commit:** 43117a4
**Applied fix:** Added nil guard `if measurement.Metadata == nil { measurement.Metadata = map[string]string{} }` in `Terminate()` before accessing `measurement.Metadata["runId"]`, consistent with the existing pattern in `Resume()` at line 106-108.

### W-02: context.Background() used everywhere -- no timeout/cancellation propagation

**Files modified:** `internal/metric/metric.go`, `internal/step/step.go`
**Commit:** 8b88e9a
**Applied fix:** Added `providerCallTimeout = 60 * time.Second` constant to both packages. Replaced all bare `context.Background()` calls with `context.WithTimeout(context.Background(), providerCallTimeout)` plus `defer cancel()`. This covers 3 call sites in metric.go (Run, Resume, Terminate) and 4 call sites in step.go (TriggerRun, StopRun on timeout, GetRunResult poll, StopRun in stopActiveRun).

### W-03: v5 response body read without size limit

**Files modified:** `internal/provider/cloud/metrics.go`
**Commit:** 5dde3e0
**Applied fix:** Added `maxResponseBodySize = 1 << 20` (1 MB) constant. Wrapped both `io.ReadAll(resp.Body)` calls in `QueryAggregateMetric` with `io.LimitReader(resp.Body, maxResponseBodySize)` -- both the error path (non-200 status) and the success path.

### W-04: Duplicated mock provider type across test packages

**Files modified:** `internal/provider/providertest/mock.go` (new), `internal/metric/metric_test.go`, `internal/step/step_test.go`
**Commit:** 71104d8
**Applied fix:** Created shared `internal/provider/providertest/mock.go` with exported `MockProvider` struct. Updated both test files to import the shared mock via a type alias (`type mockProvider = providertest.MockProvider`) to keep all test callsites unchanged. Renamed field references from lowercase (`triggerRunFn`) to exported (`TriggerRunFn`).

### W-05: Mock server uses global mutable state with atomic counters -- no reset between e2e tests

**Files modified:** `e2e/mock/main.go`
**Commit:** 3081557
**Applied fix:** Added `POST /reset` endpoint to the mock server that iterates all `runConfigs` and calls `cfg.counter.Store(0)` to reset atomic counters. E2e tests can call this endpoint between test scenarios to ensure clean state.

### W-06: liveCredentials helper defined but not used by all live tests

**Files modified:** `e2e/live_test.go`
**Commit:** a1e48c9
**Applied fix:** Refactored `TestLiveMetricPlugin` and `TestLiveStepPlugin` to call `liveCredentials(t, "K6_TEST_ID")` instead of manually reading `K6_CLOUD_TOKEN`, `K6_TEST_ID`, and `K6_STACK_ID` env vars inline. Now all four live test functions use the shared helper consistently.

---

_Fixed: 2026-04-14T12:00:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
