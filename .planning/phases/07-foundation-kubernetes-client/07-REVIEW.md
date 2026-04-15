---
phase: 07-foundation-kubernetes-client
reviewed: 2026-04-15T12:00:00Z
depth: standard
files_reviewed: 12
files_reviewed_list:
  - cmd/metric-plugin/main.go
  - cmd/step-plugin/main.go
  - go.mod
  - internal/metric/metric.go
  - internal/metric/metric_test.go
  - internal/provider/config.go
  - internal/provider/operator/operator.go
  - internal/provider/operator/operator_test.go
  - internal/provider/router.go
  - internal/provider/router_test.go
  - internal/step/step.go
  - internal/step/step_test.go
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: issues_found
---

# Phase 7: Code Review Report

**Reviewed:** 2026-04-15T12:00:00Z
**Depth:** standard
**Files Reviewed:** 12
**Status:** issues_found

## Summary

Phase 7 introduces the k6-operator provider backend with lazy Kubernetes client initialization, a Router multiplexer for provider dispatch, per-provider config validation centralized in `config.go`, and updates to both metric and step plugin binaries to wire the new provider. The code is well-structured with clear separation of concerns, thorough test coverage (including concurrency safety, sync.Once caching behavior, and syntactic-only validation boundary tests), and good defensive nil-guard patterns. `go vet` passes clean.

Two warnings and three informational items found. No critical issues.

## Warnings

### WR-01: Router error message hardcodes provider names

**File:** `internal/provider/router.go:55`
**Issue:** The `resolve()` error message hardcodes `"(registered: grafana-cloud, k6-operator)"` instead of deriving registered names from `r.providers`. When a third provider is added (e.g., `k6-binary` per the project roadmap), this message will become stale and misleading to users debugging unknown-provider errors.
**Fix:**
```go
func (r *Router) resolve(cfg *PluginConfig) (Provider, error) {
	name := cfg.Provider
	if name == "" {
		name = r.fallback
	}
	p, ok := r.providers[name]
	if !ok {
		registered := make([]string, 0, len(r.providers))
		for k := range r.providers {
			registered = append(registered, k)
		}
		return nil, fmt.Errorf("unknown provider %q (registered: %v)", name, registered)
	}
	return p, nil
}
```

### WR-02: Duplicated parseConfig validation logic between metric.go and step.go

**File:** `internal/metric/metric.go:205-238` and `internal/step/step.go:251-279`
**Issue:** The per-provider validation logic (Grafana Cloud field checks for apiToken/stackId/testId, k6-operator delegation to `ValidateK6Operator()`, unknown provider pass-through) is duplicated across `metric.parseConfig` and `step.parseConfig`. The k6-operator validation was correctly centralized into `config.go:ValidateK6Operator()`, but the Grafana Cloud branch remains duplicated. If a new required field is added to Grafana Cloud config, both locations must be updated in lockstep -- this is the same drift risk that `ValidateK6Operator()` was created to solve.
**Fix:** Add a `ValidateGrafanaCloud()` method to `PluginConfig` in `config.go` (parallel to `ValidateK6Operator()`), then call it from both parseConfig functions:
```go
// In config.go:
func (c *PluginConfig) ValidateGrafanaCloud() error {
	if c.TestID == "" && c.TestRunID == "" {
		return fmt.Errorf("either testId or testRunId is required")
	}
	if c.APIToken == "" {
		return fmt.Errorf("apiToken is required (check Secret reference)")
	}
	if c.StackID == "" {
		return fmt.Errorf("stackId is required (check Secret reference)")
	}
	return nil
}
```
Note: the metric plugin additionally requires `cfg.Metric != ""` (shared validation), while the step plugin does not. That distinction is correct and should remain in the respective parseConfig functions. Only the provider-specific branch should be centralized.

## Info

### IN-01: Duplicate setupLogging functions across binaries

**File:** `cmd/metric-plugin/main.go:58-73` and `cmd/step-plugin/main.go:58-73`
**Issue:** The `setupLogging()` function is identical in both binaries. This is minor duplication given it is only two files, but could be extracted to a shared `internal/logging` package if the logging configuration grows.
**Fix:** Optional. Extract to `internal/logging/logging.go` if more logging setup is needed in future.

### IN-02: Unused parameters in Phase 7 stub methods

**File:** `internal/provider/operator/operator.go:187-194`
**Issue:** `GetRunResult` and `StopRun` stubs have named parameters (`ctx`, `cfg`, `runID`) that are unused. This is standard Go practice when implementing an interface and passes `go vet`, but using `_` for unused params in stubs signals intent more clearly.
**Fix:** Optional style preference, not actionable for Phase 7 stubs that will be replaced in Phase 8.

### IN-03: step.parseConfig does not validate cfg.Metric field

**File:** `internal/step/step.go:251-279`
**Issue:** Unlike `metric.parseConfig` which requires `cfg.Metric != ""`, the step plugin's `parseConfig` does not check this field. This is correct behavior -- the step plugin only does fire-and-wait (pass/fail based on run state) and never uses `cfg.Metric` or `cfg.Aggregation`. Noting for clarity that this asymmetry is intentional, not a missed validation.
**Fix:** None needed. Document if not already clear from context.

---

_Reviewed: 2026-04-15T12:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
