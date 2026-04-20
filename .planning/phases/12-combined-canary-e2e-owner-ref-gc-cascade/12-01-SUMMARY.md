---
phase: 12-combined-canary-e2e-owner-ref-gc-cascade
plan: 01
subsystem: testing
tags: [e2e, k6-operator, owner-references, cascade-gc, argo-rollouts, kind]

# Dependency graph
requires:
  - phase: 08.1-wire-analysisrun-rollout-metadata-through-plugin-layers
    provides: "parentOwnerRef(cfg) emits AR > Rollout > nil OwnerReference on the TestRun CR (D-07 precedence)"
  - phase: 11-success-path-testrun-cleanup
    provides: "Provider.Cleanup + metric GarbageCollect + step terminal-state hook so plugin-layer cleanup does not race kube-apiserver cascade GC during this test"
provides:
  - "TestK6OperatorCombinedCanaryARDeletion e2e proving D-07 (AR > Rollout) owner-ref precedence under real kube-apiserver cascading GC"
  - "getTestRunsOwnedBy helper (filter TestRun CRs by ownerReferences[].kind + optional name)"
  - "getTestRunStage helper (k6-operator v1.3.x status.stage enum; no 'running' literal)"
  - "listAnalysisRuns helper (discover Rollout-spawned AR names without prediction)"
  - "getTestRunLabel helper (kubectl jsonpath with dot/slash-escaped label keys)"
  - "Long-running k6 script fixture (duration: 120s) enabling overlap assertions that the short iterations-based fixture cannot support"
affects: [phase-13-opportunistic-polish]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Owner-ref cascade assertion: capture AR name BEFORE delete; filter poll by captured name so an Argo-recreated background AR's new TestRun does not contaminate the assertion"
    - "Wait for stage=started (NOT 'running') on both TestRuns before issuing the delete; fatal-on-timeout avoids inconclusive assertions"
    - "Long-running k6 script (duration: 120s) for overlap assertions -- short iteration-capped scripts finish in ~1s and race the polling logic"

key-files:
  created:
    - "e2e/testdata/rollout-combined-canary-k6op.yaml"
    - "e2e/testdata/configmap-script-k6op-long.yaml"
    - ".planning/phases/12-combined-canary-e2e-owner-ref-gc-cascade/12-01-SUMMARY.md"
  modified:
    - "e2e/k6_operator_test.go (add one Test + four helpers; +285 lines; no existing code touched)"

key-decisions:
  - "Use canary.analysis background block with startingStep: 1 (NOT a dedicated analysis step). Dedicated analysis step would block the step plugin's completion for the full 120s script duration before starting the AR -- breaking the 'both TestRuns concurrent' premise. Background analysis starts the AR AT the step-plugin step, giving both CRs simultaneous existence."
  - "Wait for stage=started (k6-operator v1.3.x enum), not 'running'. The earlier draft's 'running' guard would have timed out silently -- there is no 'running' literal in k6-operator's stage enum (initialization, initialized, created, started, stopped, finished, error)."
  - "Capture AR name before delete; filter cascade poll by captured name. Argo Rollouts' reconcileBackgroundAnalysisRun recreates deleted background ARs, spawning a new AR-owned TestRun. Filtering by captured name ensures only the deleted AR's TestRun is counted."
  - "Parse ownerReferences via JSON unmarshal in Go rather than jsonpath. jsonpath cannot conditionally filter on ownerReferences[].kind cleanly; struct-based parsing is simpler and more robust."
  - "Survivor-check includes 5s post-cascade settle window. Catches hypothetical delayed cascade ripple that would wrongly remove the Rollout-owned TestRun."
  - "Reuse dumpK6OperatorDiagnostics unchanged. POLISH-02 (declarative helper refactor) is Phase 13 scope; keeping it unchanged here preserves a clean Phase-12-only diff."

patterns-established:
  - "e2e ownership-filter helper pattern: (namespace, ownerKind, ownerName) -> []names, decoded via Go struct instead of kubectl jsonpath. Generalizable to future tests that need to distinguish CR subsets by parent."
  - "Fail-fast pre-delete stage gate: wait for a known non-terminal stage on BOTH CRs before issuing the kubectl delete, so subsequent disappearance is unambiguously cascade-GC and not k6-operator's own reconciliation."

requirements-completed: [TEST-02]

# Metrics
duration: 6min
completed: 2026-04-20
---

# Phase 12 Plan 01: Combined-canary e2e for D-07 owner-ref GC cascade Summary

**TestK6OperatorCombinedCanaryARDeletion e2e proves AR > Rollout owner-ref precedence under real kube-apiserver cascading GC -- AR-owned TestRun disappears within 90s of AnalysisRun delete; Rollout-owned step TestRun survives with managed-by label intact.**

## Performance

- **Duration:** ~6 min (wall clock; e2e suite ran 324s = 5m 24s for all 7 tests, of which the new test ran 43.28s)
- **Started:** 2026-04-20T16:50:00Z
- **Completed:** 2026-04-20T16:56:00Z
- **Tasks:** 1 (single-task plan; e2e is all-or-nothing)
- **Files modified:** 3 (1 Go test file + 2 new testdata YAMLs)

## Accomplishments

- New `TestK6OperatorCombinedCanaryARDeletion` in `e2e/k6_operator_test.go` deploys a Rollout running the step plugin AND a background AnalysisTemplate (metric plugin) concurrently against k6-operator in a real kind cluster.
- Test waits until both TestRuns reach stage `started`, captures the AR name, issues `kubectl delete analysisrun <captured-name>`, and then asserts (within a 90s bound) that only the AR-owned TestRun disappears via kube-apiserver cascading GC while the Rollout-owned step TestRun survives with its `app.kubernetes.io/managed-by=argo-rollouts-k6-plugin` label intact.
- D-07 precedence (AR > Rollout > none) now has end-to-end coverage: unit tests from Phase 08.1 asserted the OwnerReference struct shape; this e2e closes the loop against the real kube-apiserver GC controller.
- Four new helpers for owner-ref-aware e2e polling: `getTestRunsOwnedBy`, `getTestRunStage`, `listAnalysisRuns`, `getTestRunLabel`.
- Two new testdata fixtures: `rollout-combined-canary-k6op.yaml` (Rollout + AnalysisTemplate with background analysis at `startingStep: 1`) and `configmap-script-k6op-long.yaml` (120s k6 duration so both TestRuns stay in `started` during the AR-delete window).

## Task Commits

Single-task plan committed atomically:

1. **Task 1: Add combined-canary e2e test + testdata fixtures + owner-ref helpers** - `9819bf2` (test)

## Files Created/Modified

- `e2e/k6_operator_test.go` - Added `TestK6OperatorCombinedCanaryARDeletion` (Setup/Assess/Teardown) + 4 helpers (`getTestRunsOwnedBy`, `getTestRunStage`, `listAnalysisRuns`, `getTestRunLabel`) + `strings` import. Zero modifications to existing tests/helpers. (+285 lines)
- `e2e/testdata/rollout-combined-canary-k6op.yaml` - New fixture: AnalysisTemplate (`k6-operator-combined-e2e`) + Rollout (`k6-combined-canary-k6op-e2e`) with `canary.analysis.templates[0]` referencing the AnalysisTemplate and `startingStep: 1` so the background AR launches concurrently with the step-plugin step.
- `e2e/testdata/configmap-script-k6op-long.yaml` - New fixture: ConfigMap `k6-e2e-script-long` with a 120s k6 script (`vus: 1, duration: "120s", sleep(1)` for ~1 req/s). Replaces the short `iterations: 10` fixture pattern for this test only.

## Decisions Made

See `key-decisions` in frontmatter. Key calls:

- **Background analysis (`canary.analysis` + `startingStep: 1`), NOT a dedicated analysis step.** Dedicated step would block the step plugin's completion for the full 120s script duration before starting the AR -- breaking the "both TestRuns coexist" premise the cascade assertion depends on.
- **Wait for stage `started` (NOT `running`).** k6-operator v1.3.x has no `running` literal in the stage enum. The earlier draft's `"running"` guard would have timed out silently.
- **Capture AR name before the delete; filter cascade poll by it.** Argo's `reconcileBackgroundAnalysisRun` recreates deleted background ARs, spawning a new AR-owned TestRun. Filtering by the captured name is the only way to distinguish "the deleted AR's TestRun" from "an entirely new AR's TestRun".
- **Parse `ownerReferences` via Go struct unmarshal, not jsonpath.** jsonpath cannot conditionally filter `ownerReferences[].kind` cleanly; a small struct with `encoding/json` is more robust.

## Deviations from Plan

None -- plan executed exactly as written. Every design decision the planner front-loaded (stage=started vs running, background analysis vs dedicated step, captured-AR-name filter, JSON-unmarshal owner-ref helper) landed verbatim in the code. `make test-e2e` passed on the first execution (43.28s for the new test; 324.4s for the full suite).

## Issues Encountered

None.

## Verification Gates

| Gate | Command | Result |
|------|---------|--------|
| Build | `go build ./...` | exit 0 |
| E2E build | `go build -tags=e2e ./e2e/...` | exit 0 |
| Unit tests | `go test ./... -count=1` | all packages ok (internal/metric, internal/provider, internal/provider/cloud, internal/provider/operator, internal/step) |
| E2E tests | `make test-e2e` | 7/7 PASS (TestK6OperatorStepPass 83.58s, TestK6OperatorMetricPass 12.70s, **TestK6OperatorCombinedCanaryARDeletion 43.28s (NEW)**, TestMetricPluginPass 21.72s, TestMetricPluginFail 9.40s, TestStepPluginPass 43.49s, TestStepPluginFail 24.98s); 4 TestLive* SKIP (K6_LIVE_TEST-gated, unchanged) |
| Lint | `make lint` | 0 issues |

### Structural invariants

```
grep -c "func TestK6OperatorCombinedCanaryARDeletion" e2e/k6_operator_test.go   -> 1
grep -c "func getTestRunsOwnedBy"                      e2e/k6_operator_test.go   -> 1
grep -c "func getTestRunStage"                         e2e/k6_operator_test.go   -> 1
grep -c "func listAnalysisRuns"                        e2e/k6_operator_test.go   -> 1
grep -c "func getTestRunLabel"                         e2e/k6_operator_test.go   -> 1
grep -c "getTestRunsOwnedBy("                          e2e/k6_operator_test.go   -> 5 (defn + 4 callsites)
grep -c "kind: AnalysisTemplate" e2e/testdata/rollout-combined-canary-k6op.yaml  -> 1
grep -c "kind: Rollout"          e2e/testdata/rollout-combined-canary-k6op.yaml  -> 1
grep -c "startingStep"           e2e/testdata/rollout-combined-canary-k6op.yaml  -> 1
grep -c "duration:"              e2e/testdata/configmap-script-k6op-long.yaml    -> 2 (http_req_duration + run duration)
```

## User Setup Required

None -- no external service configuration required; the e2e test runs entirely against a local kind cluster with the mock-k6 server.

## Next Phase Readiness

- TEST-02 closed. D-07 owner-ref precedence now has both unit-level (Phase 08.1) and e2e-level (this phase) coverage against real kube-apiserver cascading GC.
- Phase 13 (Opportunistic polish, POLISH-01/02/03) is unblocked. POLISH-02 will refactor `dumpK6OperatorDiagnostics` into a declarative tuple-driven helper; the new test calls it from the same call sites, so no test edits needed at that time.
- v0.4.0 Cleanup milestone progress: 3/4 plans complete (75%) after this plan. Remaining: Phase 13 (1 plan).

## Self-Check: PASSED

- `e2e/k6_operator_test.go` contains `TestK6OperatorCombinedCanaryARDeletion` (verified via grep -> 1 match).
- `e2e/testdata/rollout-combined-canary-k6op.yaml` exists (ls -l confirms).
- `e2e/testdata/configmap-script-k6op-long.yaml` exists (ls -l confirms).
- Commit `9819bf2` exists in git log (verified).
- `make test-e2e` exit 0 with 7/7 PASS including the new test (verified in output file).
- `make lint` exit 0 (verified).

---
*Phase: 12-combined-canary-e2e-owner-ref-gc-cascade*
*Completed: 2026-04-20*
