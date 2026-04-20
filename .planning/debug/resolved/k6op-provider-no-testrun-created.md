---
slug: k6op-provider-no-testrun-created
status: resolved
trigger: Phase 08.2 e2e re-run — TestK6OperatorStepPass and TestK6OperatorMetricPass both FAIL because the plugin never creates a TestRun CR in the namespace
created: 2026-04-19T14:28:34Z
updated: 2026-04-20T09:58:00Z
phase: "08.2"
---

# Debug Session: k6-operator provider TestRun CR GC'd before Resume can read status

## Symptoms

### Expected behavior
After Phase 08.2 (parallelism=1 default in `buildTestRun`), the plugin's k6-operator provider should submit a TestRun CR to the in-cluster API when invoked by an AnalysisRun (metric plugin) or a Rollout step (step plugin). The TestRun CR should then be reconciled by k6-operator, spawning runner pods that execute the k6 script, and the plugin should be able to poll the TestRun's terminal stage + read pod logs for handleSummary.

### Actual behavior
Initial run: diagnostic teardown dump shows empty TestRuns list (misleading — see root cause). `TestK6OperatorStepPass` times out at 326.84s with Rollout Degraded. `TestK6OperatorMetricPass` reaches `phase=Error` in 92.84s.

After enhancing the diagnostic dump to include argo-rollouts controller logs + AR yaml, the full picture emerged:

- The plugin DOES create TestRun CRs successfully — multiple per AR (once per retry).
- Every Resume 10s later fails with `testruns.k6.io "k6-unknown-XXX" not found`.
- k6-operator's logs show the TestRun running end-to-end: `stage=created -> stage=started -> stage=finished` in ~2s (1 VU, 10 iterations against an in-cluster mock), then the CR vanishes.
- The AnalysisRun retries via consecutiveErrorLimit (default 4), each retry creates a new TestRun; after 5 errors the AR is marked Error and the test fails.

### Reproduction
```
go test -tags=e2e -count=1 -timeout=10m -run TestK6OperatorMetricPass ./e2e/...
```
Fails deterministically (before fix). Passes cleanly (after fix).

### Scope
- k6-operator provider path only (cloud provider path was unaffected).
- Both step and metric plugin entry points hit the same buildTestRun, so both failed.
- Plugin infrastructure, RBAC, handshake, metadata wiring (08.1), parallelism default (08.2) were all correct.

## Current Focus

- hypothesis: RESOLVED — spec.Cleanup=post tells k6-operator to delete the TestRun CR (which cascades to its runner pods) as soon as stage transitions to finished/error. The plugin design assumes the TestRun remains observable after completion so GetRunResult can read the terminal stage and parse handleSummary from pod logs. These assumptions are incompatible with spec.Cleanup=post.
- test: passes now — `go test -tags=e2e -run TestK6OperatorMetricPass ./e2e/...` (27.81s) and `go test -tags=e2e -run TestK6OperatorStepPass ./e2e/...` (21.65s).
- expecting: AR phase=Successful, metric value "1", TestRun.spec.parallelism readable as "1" via jsonpath (proves the CR persists past the Resume that reads its terminal state).
- next_action: (completed) — run full e2e suite optional, committing fix as per Phase 08.2.

## Evidence

- timestamp: 2026-04-20T07:47:43Z | source: controller logs (from enhanced diagnostic dump) | observation: Plugin logs `k6 TestRun created name=k6-unknown-3d8edbf3`, then 10s later AR controller emits `failed to resume via plugin: get testruns k6-e2e-*/k6-unknown-3d8edbf3: testruns.k6.io "k6-unknown-3d8edbf3" not found`. A NEW TestRun is created every 20s due to AR retry.
- timestamp: 2026-04-20T07:47:35Z | source: k6-operator controller logs | observation: TestRun `k6-unknown-503003d1` reconciles through created -> started -> finished stages within ~2s, then disappears.
- timestamp: 2026-04-20T09:50:00Z | source: `/Users/jorge/go/pkg/mod/github.com/grafana/k6-operator@v1.3.2/internal/controller/testrun_controller.go:361-366` | observation: k6-operator v1.3.2 source: `case "error", "finished": if k6.GetSpec().Cleanup == "post" { _ = r.Delete(ctx, k6) }` — the operator literally calls `r.Delete` on the TestRun CR when stage reaches a terminal value AND spec.Cleanup=post. Because we set Cleanup=post in buildTestRun, every TestRun self-deletes within seconds of completion (before the metric plugin's next Resume tick).
- timestamp: 2026-04-20T09:55:00Z | source: e2e verification run | observation: With `Cleanup: "post"` removed from `buildTestRun`, TestK6OperatorMetricPass passes in 27.81s and TestK6OperatorStepPass passes in 40.49s. Argo Rollouts controller logs show `metric plugin Resume ... state=finished phase=Successful value=1` — the plugin reads the TestRun status AND pod logs successfully.

## Eliminated Hypotheses

- "plugin's TriggerRun errors out before creating the CR" — plugin logs show successful Creates; the bug is observed post-creation.
- "RBAC denies CR create" — plugin logs show successful Creates.
- "Namespace resolution wrong" — CR is created in the AR's namespace; Resume looks up in the same namespace; the 404 is because the CR was garbage-collected, not because the namespace is wrong.
- "parallelism=0 hang" — with Phase 08.2's default=1, the TestRun actually runs to completion; that's what exposes the cleanup bug.

## Resolution

- root_cause: `buildTestRun` set `spec.Cleanup = "post"` unconditionally. k6-operator v1.3.x interprets `Cleanup=post` as "delete the TestRun CR (and its runner pods via cascade) when stage transitions to finished or error." This races the metric plugin's Resume (10s interval) and the step plugin's poll (15s interval): by the time the plugin tries to read the terminal stage and pod logs, the CR is already gone and the API returns 404. The intent of `Cleanup=post` in the research notes was "cleanup pods post-run" but the operator's actual semantics cleanup the whole CR, which defeats the plugin's observation loop.
- fix: Remove `Cleanup: "post"` from `buildTestRun` (leave the field unset, which maps to k6-operator's default behavior of "don't touch the TestRun"). The plugin already has `StopRun` (called by Terminate/Abort) for explicit cleanup on cancellation. A follow-up phase should implement post-terminal deletion (via `GarbageCollect` on the metric plugin and an equivalent hook on the step plugin) so success-path TestRuns don't leak. The `TestBuildTestRun_Cleanup` unit test was inverted into `TestBuildTestRun_CleanupUnset` with a regression comment that pins the new invariant.
- verification: (1) unit tests pass (`go test ./internal/...` — 237+ tests green). (2) `make lint` clean. (3) `go test -tags=e2e -count=1 -timeout=10m -run TestK6OperatorMetricPass ./e2e/...` PASS in 27.81s. (4) `go test -tags=e2e -count=1 -timeout=10m -run TestK6OperatorStepPass ./e2e/...` PASS in 40.49s.
- files_changed:
  - `internal/provider/operator/testrun.go` — remove `Cleanup: "post"`, add explanatory doc comment on buildTestRun
  - `internal/provider/operator/testrun_test.go` — rename `TestBuildTestRun_Cleanup` → `TestBuildTestRun_CleanupUnset`, assert `spec.Cleanup == ""` with regression commentary
  - `e2e/k6_operator_test.go` — extend `dumpK6OperatorDiagnostics` to also dump AnalysisRun/Rollout yaml + argo-rollouts and k6-operator controller logs (made the root cause visible and is worth keeping as a diagnostic aid for future failures)

## Key References

- e2e output (pre-fix, enhanced diagnostic): `/tmp/k6op-metric-repro4.log` (in-memory — full controller logs captured)
- k6-operator delete code: `github.com/grafana/k6-operator@v1.3.2/internal/controller/testrun_controller.go:361-366`
- Plugin TriggerRun / GetRunResult: `internal/provider/operator/operator.go`
- Plugin buildTestRun: `internal/provider/operator/testrun.go`
- Phase 10 e2e test: `e2e/k6_operator_test.go`
