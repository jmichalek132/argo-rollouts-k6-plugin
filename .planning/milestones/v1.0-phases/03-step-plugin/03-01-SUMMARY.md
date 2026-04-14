---
phase: 03-step-plugin
plan: 01
subsystem: plugin
tags: [go, argo-rollouts, step-plugin, rpc, k6, tdd]

# Dependency graph
requires:
  - phase: 01-foundation-provider
    provides: "Provider interface (TriggerRun, GetRunResult, StopRun), PluginConfig struct, RunState/RunResult types"
provides:
  - "K6StepPlugin implementing rpc.StepPlugin (InitPlugin + Run + Terminate + Abort + Type)"
  - "Fire-and-wait lifecycle with state persistence via json.RawMessage"
  - "Timeout management (default 5m, max 2h, StopRun on exceed)"
  - "Phase mapping: Passed->Successful, Failed/Errored/Aborted/timeout->Failed"
affects: [03-02-step-binary-wiring, 04-e2e-testing]

# Tech tracking
tech-stack:
  added: []
  patterns: [step-plugin-lifecycle, json-rawmessage-state-persistence, stopActiveRun-shared-helper]

key-files:
  created:
    - internal/step/step.go
    - internal/step/step_test.go
  modified: []

key-decisions:
  - "Validation errors return PhaseFailed (not RpcError) -- RpcError reserved for infrastructure failures"
  - "Terminate and Abort share stopActiveRun helper -- identical behavior per D-07"
  - "stepState struct with json tags for round-trip persistence in RpcStepContext.Status"

patterns-established:
  - "stepState struct persisted via json.RawMessage in RpcStepResult.Status"
  - "parseConfig on K6StepPlugin method (not package-level) to access provider for logging"
  - "stopActiveRun shared helper for Terminate/Abort with swallowed errors"

requirements-completed: [PLUG-02, STEP-01, STEP-02, STEP-03, STEP-04, STEP-05]

# Metrics
duration: 4min
completed: 2026-04-10
---

# Phase 03 Plan 01: Step Plugin Core Logic Summary

**K6StepPlugin with fire-and-wait lifecycle: trigger/poll via Provider interface, timeout management, json.RawMessage state persistence, 89.1% test coverage**

## Performance

- **Duration:** 4 min
- **Started:** 2026-04-10T09:23:41Z
- **Completed:** 2026-04-10T09:28:07Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- K6StepPlugin implements rpc.StepPlugin with compile-time interface check
- Full Run() lifecycle: first call triggers (or attaches via testRunId), subsequent calls poll, terminal state returns appropriate phase
- Timeout management with StopRun on exceed, default 5m, max 2h, invalid config returns PhaseFailed
- Terminate/Abort both call StopRun and swallow errors per D-07/D-08
- 25 tests passing with -race flag, 89.1% coverage

## Task Commits

Each task was committed atomically:

1. **Task 1: RED -- Write failing tests for K6StepPlugin lifecycle** - `3298e43` (test)
2. **Task 2: GREEN -- Implement K6StepPlugin to pass all tests** - `e3adff4` (feat)

## Files Created/Modified
- `internal/step/step.go` - K6StepPlugin struct with full lifecycle (Run, Terminate, Abort, InitPlugin, Type)
- `internal/step/step_test.go` - 25 TDD tests covering all requirements with mock provider

## Decisions Made
- Validation errors (missing config fields, invalid timeout) return PhaseFailed with descriptive message, not RpcError -- RpcError is reserved for infrastructure failures that the controller can retry
- Terminate and Abort are identical in behavior (both call stopActiveRun helper) per D-07
- stepState uses json tags matching context documentation (runId, triggeredAt, testRunURL, finalStatus)
- parseConfig is a method on K6StepPlugin (not package-level like metric plugin) for consistency with step context access pattern

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
- Unused variable in test helper (line 117 `cfg := makeConfig(nil)`) caught by compiler -- removed during RED phase

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- internal/step/step.go ready for binary wiring in cmd/step-plugin/main.go (Plan 03-02)
- K6StepPlugin.New() accepts provider.Provider, ready for GrafanaCloudProvider injection
- All 25 tests pass, no regressions in full test suite

## Self-Check: PASSED

- internal/step/step.go: FOUND
- internal/step/step_test.go: FOUND
- 03-01-SUMMARY.md: FOUND
- Commit 3298e43: FOUND
- Commit e3adff4: FOUND

---
*Phase: 03-step-plugin*
*Completed: 2026-04-10*
