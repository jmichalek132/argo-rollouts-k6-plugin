---
phase: 07-foundation-kubernetes-client
plan: 01
subsystem: provider-routing
tags: [router, config, validation, k6-operator, backward-compat]
dependency_graph:
  requires: []
  provides: [provider-router, plugin-config-k6-operator, per-provider-validation]
  affects: [internal/provider, internal/metric, internal/step]
tech_stack:
  added: []
  patterns: [functional-options, compile-time-interface-check, centralized-validation]
key_files:
  created:
    - internal/provider/router.go
    - internal/provider/router_test.go
  modified:
    - internal/provider/config.go
    - internal/metric/metric.go
    - internal/metric/metric_test.go
    - internal/step/step.go
    - internal/step/step_test.go
    - go.mod
    - go.sum
decisions:
  - "Router.Name() returns 'router'; dispatch methods log resolved provider name"
  - "Provider names use exact string match, no normalization"
  - "ValidateK6Operator() is syntactic-only, no K8s API calls"
  - "metric field required for all providers in metric plugin (shared validation)"
  - "Unknown providers pass parseConfig; Router rejects at dispatch"
metrics:
  duration: 4m
  completed: 2026-04-15
  tasks_completed: 2
  tasks_total: 2
  files_changed: 9
---

# Phase 07 Plan 01: Provider Router & Config Extensions Summary

Router multiplexer with functional options, PluginConfig extended for k6-operator, and per-provider parseConfig validation with centralized ValidateK6Operator.

## What Was Built

### Task 1: Router, Config Extensions, k8s Deps (399a280)

**PluginConfig extensions (config.go):**
- Added `ConfigMapRef` struct with `Name` and `Key` fields
- Added `Provider`, `ConfigMapRef`, and `Namespace` fields to `PluginConfig` with `omitempty` tags
- Added `IsGrafanaCloud()` helper returning true for empty or "grafana-cloud" provider
- Added `ValidateK6Operator()` method for centralized k6-operator field validation (syntactic-only, no K8s API)

**Router (router.go):**
- `Router` struct implementing `Provider` interface (compile-time checked via `var _ Provider = (*Router)(nil)`)
- `NewRouter(opts ...RouterOption)` constructor with functional options pattern
- `WithProvider(name, p)` option for registering named backends
- `resolve()` dispatches by exact provider name match; empty defaults to "grafana-cloud"
- All dispatch methods (`TriggerRun`, `GetRunResult`, `StopRun`) log resolved provider name at Debug level
- `Name()` returns "router"

**Tests (router_test.go):**
14 tests covering dispatch to cloud, dispatch to operator, default provider, unknown provider, exact match only, GetRunResult dispatch, StopRun dispatch, Router.Name, JSON backward compatibility, IsGrafanaCloud, and 4 ValidateK6Operator cases.

**go.mod:**
Promoted `k8s.io/client-go` and `k8s.io/api` v0.34.1 from indirect to direct dependencies.

### Task 2: Per-Provider parseConfig Refactor (822691f)

**metric.go parseConfig:**
- Gates Grafana Cloud fields (apiToken, stackId, testId) behind `cfg.IsGrafanaCloud()`
- Calls `cfg.ValidateK6Operator()` for k6-operator provider (no inline checks)
- Keeps metric field as shared validation for all providers

**step.go parseConfig:**
- Same pattern: Grafana Cloud fields gated behind `cfg.IsGrafanaCloud()`
- Calls `cfg.ValidateK6Operator()` for k6-operator provider (no inline checks)
- No metric field check (step plugin doesn't require it)

**New tests:**
- `TestConfig_K6OperatorValidConfig` -- verifies k6-operator config passes without apiToken/stackId
- `TestConfig_K6OperatorMissingConfigMapRef` -- verifies error when configMapRef nil
- `TestConfig_K6OperatorMissingMetric` -- verifies metric field still required
- `TestParseConfig_K6OperatorValid` -- step version of valid k6-operator config
- `TestParseConfig_K6OperatorMissingConfigMapRef` -- step version of missing configMapRef

## Review Concerns Resolved

| Concern | Severity | Resolution |
|---------|----------|------------|
| Config validation trap (parseConfig requires Grafana Cloud fields for all providers) | HIGH | parseConfig now uses IsGrafanaCloud() gating; k6-operator configs skip apiToken/stackId/testId |
| Duplicated validation in metric.go/step.go | MEDIUM (pass 2) | Both call cfg.ValidateK6Operator() from config.go -- single source of truth |
| Provider name normalization | MEDIUM | Exact match only, documented in resolve() comments |
| Router.Name() semantics | MEDIUM | Returns "router"; dispatch methods log resolved provider name |
| JSON backward compatibility | MEDIUM | Test verifies configs without new fields deserialize correctly |
| ValidateK6Operator scope ambiguity | MEDIUM (pass 2) | Doc comment states "syntactic validation only", "Does NOT hit the Kubernetes API" |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] gofmt alignment in router_test.go**
- **Found during:** Task 2 (lint check)
- **Issue:** Struct field alignment in `internalMock` didn't match gofmt expectations
- **Fix:** Ran `gofmt -w` on the file
- **Files modified:** internal/provider/router_test.go
- **Commit:** 822691f (included in Task 2 commit)

**2. [Rule 3 - Blocking] Circular dependency prevented using providertest.MockProvider in router_test.go**
- **Found during:** Task 1
- **Issue:** router_test.go is in package `provider`, cannot import `providertest` (which imports `provider`)
- **Fix:** Defined lightweight `internalMock` struct directly in router_test.go
- **Files modified:** internal/provider/router_test.go
- **Commit:** 399a280

## Known Stubs

None -- all functionality is fully wired.

## Self-Check: PASSED

All created files exist, all commits verified in git log, SUMMARY.md present.
