---
phase: 07-foundation-kubernetes-client
plan: 02
subsystem: infra
tags: [kubernetes, client-go, configmap, k6-operator, provider, sync-once]

# Dependency graph
requires:
  - phase: 07-foundation-kubernetes-client (plan 01)
    provides: Router multiplexer, PluginConfig with Provider/ConfigMapRef/Namespace fields, ValidateK6Operator()
provides:
  - K6OperatorProvider with lazy k8s client init via sync.Once
  - ConfigMap script reading with namespace fallback chain
  - Phase 7 stub methods (TriggerRun/GetRunResult/StopRun)
  - Syntactic-only Validate() delegating to ValidateK6Operator()
  - Router wiring in both metric and step plugin binaries
affects: [08-k6-operator-execution, 10-documentation]

# Tech tracking
tech-stack:
  added: [k8s.io/client-go (promoted from indirect to used), k8s.io/client-go/kubernetes/fake]
  patterns: [lazy-client-init-sync-once, permanent-failure-caching, configmap-script-loading, functional-options-for-test-injection]

key-files:
  created:
    - internal/provider/operator/operator.go
    - internal/provider/operator/operator_test.go
  modified:
    - cmd/metric-plugin/main.go
    - cmd/step-plugin/main.go

key-decisions:
  - "sync.Once permanent failure caching is intentional -- InClusterConfig failures are pod misconfig, not transient"
  - "Namespace fallback chain: cfg.Namespace -> 'default'; Phase 8 injects rollout namespace"
  - "Validate() is syntactic-only -- no K8s API calls; ConfigMap existence checked in readScript"

patterns-established:
  - "WithClient(fake) option: inject fake k8s client for testing, bypasses InClusterConfig"
  - "Phase stub pattern: validate + load data + return 'not yet implemented' error"
  - "Router wiring: both binaries register all providers at startup via NewRouter(WithProvider(...))"

requirements-completed: [FOUND-01, FOUND-02, FOUND-03]

# Metrics
duration: 4min
completed: 2026-04-15
---

# Phase 7 Plan 2: K6OperatorProvider & Binary Wiring Summary

**K6OperatorProvider with lazy k8s client (sync.Once), ConfigMap script reading, and Router wired into both plugin binaries**

## Performance

- **Duration:** 4 min
- **Started:** 2026-04-15T16:52:58Z
- **Completed:** 2026-04-15T16:56:54Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- K6OperatorProvider implements full Provider interface with lazy client init, ConfigMap reading, and documented stub behavior
- Permanent failure caching via sync.Once is intentional design (InClusterConfig failures require pod restart)
- Both plugin binaries (metric + step) now wire Router with grafana-cloud and k6-operator providers
- Backward compatible -- empty provider field defaults to grafana-cloud

## TDD Gate Compliance

- RED gate: `4c3f810` (test commit -- 12 tests, all failing at compile time)
- GREEN gate: `0022bec` (feat commit -- implementation makes all 12 tests pass)
- REFACTOR gate: not needed (code clean on first pass)

## Task Commits

Each task was committed atomically:

1. **Task 1: K6OperatorProvider with lazy client and ConfigMap reading**
   - `4c3f810` (test) -- RED: 12 failing tests for operator package
   - `0022bec` (feat) -- GREEN: K6OperatorProvider implementation, all tests pass
2. **Task 2: Wire Router into both plugin binaries** - `ede0eda` (feat)

## Files Created/Modified
- `internal/provider/operator/operator.go` - K6OperatorProvider: lazy k8s client, ConfigMap reading, Validate, TriggerRun stub
- `internal/provider/operator/operator_test.go` - 12 tests covering all behaviors
- `cmd/metric-plugin/main.go` - Router wiring with both providers
- `cmd/step-plugin/main.go` - Router wiring with both providers

## Decisions Made
- sync.Once permanent failure caching is intentional -- InClusterConfig reads files not network, failure = pod misconfig
- Namespace fallback is cfg.Namespace -> "default"; Phase 8 will inject rollout namespace from ObjectMeta
- Validate() delegates to cfg.ValidateK6Operator() only -- no K8s API interaction during validation
- TriggerRun validates config and loads script to prove pipeline, then returns stub error

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Known Stubs

| Stub | File | Line | Reason |
|------|------|------|--------|
| TriggerRun returns error | internal/provider/operator/operator.go | ~172 | Phase 8 will implement TestRun CR creation |
| GetRunResult returns error | internal/provider/operator/operator.go | ~177 | Phase 8 will implement TestRun status polling |
| StopRun returns error | internal/provider/operator/operator.go | ~182 | Phase 8 will implement TestRun deletion |

These stubs are intentional and documented -- they are the Phase 7 contract boundary. Routing and script loading work end-to-end; actual k6-operator execution is Phase 8 scope.

## Next Phase Readiness
- Router dispatches to k6-operator provider -- Phase 8 fills in TriggerRun/GetRunResult/StopRun
- ConfigMap script reading proven working -- Phase 8 uses script content for TestRun CR
- Namespace fallback documented -- Phase 8 adds rollout namespace injection
- All tests, lint, lint-stdout pass

## Self-Check: PASSED

All 4 files verified present. All 3 commit hashes verified in git log.

---
*Phase: 07-foundation-kubernetes-client*
*Completed: 2026-04-15*
