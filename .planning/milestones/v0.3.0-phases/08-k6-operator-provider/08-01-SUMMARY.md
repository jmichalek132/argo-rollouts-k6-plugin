---
phase: 08-k6-operator-provider
plan: 01
subsystem: k6-operator-provider
tags: [k6-operator, crd-construction, exit-code, config, run-identity]
dependency_graph:
  requires: []
  provides:
    - "testrun.go: buildTestRun, buildPrivateLoadZone, testRunName, encodeRunID, decodeRunID, gvrForResource, isCloudConnected, analysisRunOwnerRef"
    - "exitcode.go: exitCodeToRunState, stageToRunState, checkRunnerExitCodes"
    - "config.go: PluginConfig extended with Parallelism, Resources, RunnerImage, Env, Arguments, RolloutName, AnalysisRunUID"
  affects:
    - internal/provider/operator/operator.go (Plan 02 will wire helpers into TriggerRun/GetRunResult/StopRun)
tech_stack:
  added:
    - "github.com/grafana/k6-operator v1.3.2"
  patterns:
    - "Pure function CR construction (no I/O)"
    - "Run ID encoding for lifecycle identity across TriggerRun/GetRunResult/StopRun"
    - "Exit code precedence: Errored > Failed > Passed"
key_files:
  created:
    - internal/provider/operator/testrun.go
    - internal/provider/operator/testrun_test.go
    - internal/provider/operator/exitcode.go
    - internal/provider/operator/exitcode_test.go
  modified:
    - go.mod
    - go.sum
    - internal/provider/config.go
decisions:
  - "PrivateLoadZone uses its actual struct (Token, Resources, Image) not TestRun mirroring -- adapted from plan because PLZ spec differs from TestRun spec"
  - "buildPrivateLoadZone takes fewer params than buildTestRun because PLZ has no Script/Parallelism/Arguments fields"
  - "Run ID format is namespace/resource/name with resource validation on decode"
metrics:
  duration: 6m23s
  completed: "2026-04-16T14:38:56Z"
  tasks_completed: 2
  tasks_total: 2
  test_count: 52
---

# Phase 8 Plan 01: k6-operator Foundation Types Summary

k6-operator v1.3.2 imported with typed TestRun/PrivateLoadZone CRD construction, exit code mapping for issue #577 workaround, and run ID encoding for cross-method lifecycle identity.

## Completed Tasks

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Add k6-operator dep, extend PluginConfig, create testrun.go + testrun_test.go | a44e30b | go.mod, config.go, testrun.go, testrun_test.go |
| 2 | Create exitcode.go + exitcode_test.go with stage mapping and runner pod inspection | c294d34 | exitcode.go, exitcode_test.go |

## What Was Built

### PluginConfig Extension (config.go)
- Added 5 user-facing fields: `Parallelism`, `Resources`, `RunnerImage`, `Env`, `Arguments`
- Added 2 injected fields: `RolloutName` (json:"-"), `AnalysisRunUID` (json:"-")
- Extended `ValidateK6Operator()` with parallelism bounds checking (>= 0)

### TestRun CR Construction (testrun.go)
- `buildTestRun`: constructs k6v1alpha1.TestRun with TypeMeta, labels, owner refs, script, runner config, cleanup
- `buildPrivateLoadZone`: constructs k6v1alpha1.PrivateLoadZone for Grafana Cloud-connected execution
- `testRunName`: generates `k6-<rollout>-<8hex>` names with namespace-aware hashing, 253-char limit
- `analysisRunOwnerRef`: creates OwnerReference for AnalysisRun GC (D-09), nil when UID absent
- `encodeRunID`/`decodeRunID`/`gvrForResource`: run identity encoding for lifecycle tracking
- `isCloudConnected`: detects PrivateLoadZone vs TestRun selection
- GVR constants: `testRunGVR` (testruns), `plzGVR` (privateloadzones)
- Label constants: `managed-by`, `rollout` per D-10

### Exit Code Mapping (exitcode.go)
- `exitCodeToRunState`: 0->Passed, 99->Failed, other->Errored
- `stageToRunState`: maps all k6-operator stages including empty string and "finished" (returns Running for exit code check)
- `checkRunnerExitCodes`: lists runner pods by label selector, inspects exit codes with edge case handling:
  - Nil Terminated state -> Running (Pitfall 2)
  - RestartCount > 0 -> Errored (k6-operator backoff limit 0)
  - Multi-container precedence: Errored > Failed > Passed
  - Zero pods -> error

## Decisions Made

1. **PrivateLoadZone struct adaptation**: PLZ spec differs significantly from TestRun spec (no Script, Parallelism, Arguments, Cleanup fields). Adapted `buildPrivateLoadZone` to use the actual `PrivateLoadZoneSpec` fields (Token, Resources, Image) rather than mirroring TestRun as the plan suggested.

2. **Run ID format**: Chose `namespace/resource/name` format with resource validation against known values ("testruns", "privateloadzones") on decode. This eliminates GVR re-derivation in GetRunResult/StopRun.

3. **go directive bump**: k6-operator v1.3.2 required Go 1.25.0 minimum, bumping from 1.24.9. Also upgraded controller-runtime from v0.20.0 to v0.22.4 (indirect).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed Quantity.String() pointer receiver on map lookup**
- **Found during:** Task 1, test compilation
- **Issue:** `resource.Quantity` from map lookup is a value copy; `.String()` requires a pointer receiver
- **Fix:** Used `(&qty).String()` pattern in test assertions
- **Files modified:** testrun_test.go
- **Commit:** a44e30b

**2. [Rule 3 - Blocking] Missing go.sum entries for k6-operator transitive deps**
- **Found during:** Task 1, first test run
- **Issue:** `go get` added k6-operator but `go mod tidy` was needed to resolve transitive deps (logrus, k6 cloudapi, etc.)
- **Fix:** Ran `go mod tidy` after `go get`
- **Files modified:** go.sum
- **Commit:** a44e30b

**3. [Rule 2 - Adaptation] buildPrivateLoadZone signature differs from plan**
- **Found during:** Task 1, implementation
- **Issue:** Plan assumed PLZ spec mirrors TestRun spec, but actual PLZ struct has different fields (no Script, Parallelism, Arguments)
- **Fix:** Adapted buildPrivateLoadZone to accept only (cfg, namespace, crName) and set PLZ-specific fields (Token, Resources, Image)
- **Files modified:** testrun.go, testrun_test.go
- **Commit:** a44e30b

## Verification

```
go test -race -v -count=1 ./internal/provider/operator/...  -- PASS (52 tests)
go test -race -count=1 ./internal/provider/...              -- PASS (all sub-packages)
go build ./...                                              -- PASS
```

## TDD Gate Compliance

- RED gate: Test compilation errors confirmed before implementation (both tasks)
- GREEN gate: All tests pass after implementation (both tasks)
- Combined in single commits per task (tests + implementation) since tests were written first

## Self-Check: PASSED

All 6 files verified on disk. Both commit hashes (a44e30b, c294d34) found in git log.
