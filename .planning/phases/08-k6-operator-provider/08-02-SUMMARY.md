---
phase: 08-k6-operator-provider
plan: 02
subsystem: k6-operator-provider
tags: [k6-operator, dynamic-client, lifecycle-identity, crd-crud, idempotent-delete]
dependency_graph:
  requires:
    - "testrun.go: buildTestRun, buildPrivateLoadZone, testRunName, encodeRunID, decodeRunID, gvrForResource, isCloudConnected, analysisRunOwnerRef"
    - "exitcode.go: stageToRunState, checkRunnerExitCodes"
    - "config.go: PluginConfig with RolloutName, AnalysisRunUID, Parallelism, Resources, RunnerImage, Env, Arguments"
  provides:
    - "operator.go: TriggerRun creates TestRun/PrivateLoadZone CRs via dynamic client"
    - "operator.go: GetRunResult polls CR status and inspects runner pod exit codes"
    - "operator.go: StopRun deletes CRs with idempotent NotFound handling"
    - "operator.go: WithDynClient option for dynamic fake client injection in tests"
    - "operator.go: ensureClient returns (kubernetes.Interface, dynamic.Interface, error)"
  affects:
    - internal/metric/metric.go (future: inject cfg.RolloutName, cfg.AnalysisRunUID, cfg.Namespace)
    - internal/step/step.go (future: inject cfg.RolloutName, cfg.AnalysisRunUID, cfg.Namespace)
tech_stack:
  added:
    - "k8s.io/client-go/dynamic (promoted from indirect to direct usage)"
    - "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    - "k8s.io/apimachinery/pkg/runtime (DefaultUnstructuredConverter)"
    - "k8s.io/apimachinery/pkg/api/errors (IsNotFound)"
    - "k8s.io/client-go/dynamic/fake (test only)"
  patterns:
    - "Lifecycle identity encoding: runID = namespace/resource/name (no GVR re-derivation)"
    - "Dynamic client for CRD CRUD (typed->unstructured conversion via DefaultUnstructuredConverter)"
    - "Idempotent delete: NotFound treated as success in StopRun"
    - "Validation-before-IO: ValidateK6Operator runs before readScript"
    - "Exit code inspection: checkRunnerExitCodes called only for finished stage"
key_files:
  created: []
  modified:
    - internal/provider/operator/operator.go
    - internal/provider/operator/operator_test.go
    - internal/provider/operator/exitcode_test.go
decisions:
  - "buildPrivateLoadZone called with (cfg, ns, crName) not (cfg, cmName, cmKey, ns, crName) since PLZ spec has no script fields (adapted from plan based on Plan 01 actual signature)"
  - "Extracted goconst test constants (testCRName, testPodRunner0, testPodRunner1) to satisfy linter"
metrics:
  duration: 7m09s
  completed: "2026-04-16T14:49:27Z"
  tasks_completed: 1
  tasks_total: 1
  test_count: 85
---

# Phase 8 Plan 02: K6OperatorProvider Core Implementation Summary

Dynamic client CRD CRUD with lifecycle identity encoding -- TriggerRun creates TestRun/PrivateLoadZone, GetRunResult decodes runID for stage+exit code polling, StopRun deletes with idempotent NotFound handling.

## Completed Tasks

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 (RED) | Add failing tests for TriggerRun, GetRunResult, StopRun | acd392e | operator.go (struct+signature changes), operator_test.go (16 new tests) |
| 1 (GREEN) | Implement TriggerRun, GetRunResult, StopRun with dynamic client | eb5d702 | operator.go (full implementation), operator_test.go, exitcode_test.go (goconst fix) |

## What Was Built

### TriggerRun (operator.go)
- Validates config BEFORE readScript (no I/O wasted on invalid config)
- Reads script from ConfigMap to verify it exists
- Auto-detects PrivateLoadZone vs TestRun via `isCloudConnected(cfg)` (D-02)
- Converts typed k6-operator structs to unstructured via `runtime.DefaultUnstructuredConverter` (T-08-06 mitigation)
- Creates CR via dynamic client with correct GVR
- Sets OwnerReferences when `cfg.AnalysisRunUID` is non-empty (D-09)
- Falls back to "unknown" rollout name when `cfg.RolloutName` is empty
- Returns encoded runID: `namespace/resource/name`

### GetRunResult (operator.go)
- Decodes runID to recover namespace, GVR, and CR name (HIGH review concern: no re-derivation from config)
- Single GET per poll cycle (D-04)
- Extracts `status.stage` from unstructured object (absent stage = Running)
- Maps stage to RunState via `stageToRunState`
- For "finished" stage: inspects runner pod exit codes via `checkRunnerExitCodes`
- Handles NotFound CR with wrapped error
- Handles pods not yet terminated (Pitfall 2) by returning Running

### StopRun (operator.go)
- Decodes runID to recover namespace, GVR, and CR name
- Deletes CR via dynamic client using decoded GVR (works for both TestRun and PrivateLoadZone)
- Treats NotFound as success (HIGH review concern: idempotent delete for abort paths)

### Infrastructure Changes
- Added `dynClient dynamic.Interface` to K6OperatorProvider struct
- Added `WithDynClient` option for test injection
- Updated `ensureClient` to return `(kubernetes.Interface, dynamic.Interface, error)` tuple
- Updated `readScript` to use new 3-return signature (ignores dynClient)
- All logging follows project slog convention with `"provider", p.Name()`

## Decisions Made

1. **buildPrivateLoadZone signature**: Plan suggested 5-param signature `(cfg, cmName, cmKey, ns, crName)` but actual Plan 01 implementation has 3-param `(cfg, ns, crName)` since PLZ spec has no script fields. Used actual signature.

2. **goconst lint compliance**: Extracted repeated test string literals into constants (`testCRName` in operator_test.go, `testPodRunner0`/`testPodRunner1` in exitcode_test.go) to satisfy goconst linter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] goconst lint failures**
- **Found during:** Task 1, GREEN phase verification
- **Issue:** String literals `"k6-my-app-abc12345"` (7 occurrences), `"k6-myapp-abc-runner-0"` (4 occurrences), `"k6-myapp-abc-runner-1"` (3 occurrences) triggered goconst lint errors
- **Fix:** Extracted into test-file constants: `testCRName`, `testPodRunner0`, `testPodRunner1`
- **Files modified:** operator_test.go, exitcode_test.go
- **Commit:** eb5d702

**2. [Rule 3 - Blocking] Test helper name collision**
- **Found during:** Task 1, RED phase compilation
- **Issue:** `testRunnerPod` and `testRunnerPodNotTerminated` helpers conflicted with identically named functions in exitcode_test.go (same package)
- **Fix:** Removed duplicate helpers from operator_test.go, reused `testRunnerPod` and `testRunningPod` from exitcode_test.go
- **Files modified:** operator_test.go
- **Commit:** acd392e

## Verification

```
go test -race -v -count=1 ./internal/provider/operator/...  -- PASS (85 tests)
go test -race -count=1 ./internal/provider/...              -- PASS (all sub-packages)
go build ./...                                              -- PASS
make lint                                                   -- PASS (0 issues)
make test                                                   -- PASS (all packages)
```

## TDD Gate Compliance

- RED gate: test(08-02) commit acd392e -- 16 new tests fail with "not yet implemented (Phase 8)"
- GREEN gate: feat(08-02) commit eb5d702 -- all 85 operator tests pass
- REFACTOR gate: not needed (no cleanup required after GREEN)

## Threat Surface Scan

No new threat surface beyond what is documented in the plan's threat model. All CRD operations use the dynamic client authenticated via in-cluster service account (T-08-05). Typed-to-unstructured conversion uses `runtime.DefaultUnstructuredConverter` (T-08-06). All operations logged with slog (T-08-08). RunID format validated on decode (T-08-10).

## Self-Check: PASSED

All 4 files verified on disk. Both commit hashes (acd392e, eb5d702) found in git log.
