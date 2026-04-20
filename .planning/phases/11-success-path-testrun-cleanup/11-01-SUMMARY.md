---
phase: 11-success-path-testrun-cleanup
plan: 01
subsystem: cleanup
tags: [argo-rollouts, k6-operator, garbage-collect, cleanup, dynamic-client, slog, testify]

# Dependency graph
requires:
  - phase: 08.3-remove-spec-cleanup-post-testrun-gc-d-before-plugin-can-read
    provides: "removal of spec.Cleanup=post on TestRun CRs -- the race that forced success-path cleanup into the plugin"
  - phase: 08.1-wire-analysisrun-rollout-metadata-through-plugin-layers
    provides: "populateFromAnalysisRun plumbing -- reused by GarbageCollect for log context"
provides:
  - "Provider.Cleanup(ctx, cfg, runID) error interface method"
  - "K6OperatorProvider.Cleanup (idempotent CR delete, source='cleanup')"
  - "GrafanaCloudProvider.Cleanup (no-op, slog.Debug)"
  - "Router.Cleanup dispatch"
  - "providertest.MockProvider.CleanupFn + delegating Cleanup method"
  - "K6MetricProvider.GarbageCollect walking ar.Status.MetricResults[*].Measurements[*].Metadata[runId]"
  - "shared K6OperatorProvider.deleteCR(ctx, runID, source) helper with 'source' structured log field"
affects:
  - "11-02 step plugin terminal-state hook (compile-depends on Provider.Cleanup)"
  - "Phase 12 combined-canary e2e (success-path cleanup must exist for the GC cascade test to be unconfounded)"
  - "future Kubernetes Job provider (JOB-01/02/03) -- can differentiate StopRun vs Cleanup at the delete path"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "deleteCR helper shared between StopRun and Cleanup with source=stop|cleanup log key"
    - "GarbageCollect walking AR.Status.MetricResults for measurement runIds (mirrors argo-rollouts JobProvider precedent)"
    - "per-call context.WithTimeout in loop with cancel() at end-of-iteration (not defer-in-loop)"
    - "error-swallowing at slog.Warn with structured fields for best-effort cleanup (GC-03)"

key-files:
  created: []
  modified:
    - "internal/provider/provider.go -- Provider interface gains Cleanup method"
    - "internal/provider/router.go -- Router.Cleanup dispatch"
    - "internal/provider/router_test.go -- TestRouter_CleanupDispatchesToResolvedProvider + internalMock cleanup fields"
    - "internal/provider/providertest/mock.go -- CleanupFn field + delegating Cleanup method"
    - "internal/provider/cloud/cloud.go -- GrafanaCloudProvider.Cleanup no-op"
    - "internal/provider/cloud/cloud_test.go -- TestCleanup_IsNoOp"
    - "internal/provider/operator/operator.go -- deleteCR helper + Cleanup + refactored StopRun"
    - "internal/provider/operator/operator_test.go -- 4 TestCleanup_* cases"
    - "internal/metric/metric.go -- real GarbageCollect walking ar.Status.MetricResults"
    - "internal/metric/metric_test.go -- 7 TestGarbageCollect_* cases (replacing the no-op stub)"
    - ".planning/REQUIREMENTS.md -- GC-01 signature corrected to the real argo-rollouts v1.9.0 contract"

key-decisions:
  - "Add Cleanup method rather than reusing StopRun -- semantic distinction (cancel vs release-after-terminal) lets future providers differentiate the delete path even though they share it today on the operator backend"
  - "Walk ar.Status.MetricResults for the matching metric.Name rather than label-selector-by-ar.UID -- runIDs are not deterministically derivable from ar.UID (include timestamp hash), and Measurements is authoritative"
  - "Ignore the limit parameter in GarbageCollect's cleanup decision -- we clean every runID in the slice, because by the time Argo trims to limit the earlier measurements are the ones discarded; NotFound safety net makes over-deletion free"
  - "Errors swallowed at slog.Warn per GC-03 -- mirrors the existing pattern in Terminate (metric.go:197-202) which swallows StopRun errors the same way"
  - "Sequential for-loop over measurements; no parallelization -- slice is tiny in practice (Argo default limit=2) and goroutine+errgroup overhead adds no latency benefit for a controller-side async pass"
  - "Per-call context.WithTimeout derived from context.Background() with cancel() at end-of-iteration (not defer-in-loop) to avoid context leaks"
  - "New 'source' structured slog field (values 'stop'|'cleanup') on deleteCR's Info logs -- observability win for grepping controller logs when diagnosing cancel-vs-cleanup delete paths"

patterns-established:
  - "Best-effort cleanup contract: log at Warn, return success to controller (GC-03)"
  - "Idempotent delete: k8serrors.IsNotFound treated as success, mirrors StopRun idempotent abort"
  - "source= structured log field to distinguish call-site semantics (stop vs cleanup today; future callers can add their own label)"

requirements-completed: [GC-01, GC-03, GC-04]

# Metrics
duration: 26min
completed: 2026-04-20
---

# Phase 11 Plan 01: Success-path TestRun Cleanup (Metric Plugin) Summary

**Metric plugin `GarbageCollect` now walks `ar.Status.MetricResults` and dispatches `Provider.Cleanup` per measurement `runId`, closing the success-path TestRun CR leak deferred by Phase 08.3.**

## Performance

- **Duration:** 26 min
- **Started:** 2026-04-20T12:24:40Z
- **Completed:** 2026-04-20T12:50:16Z
- **Tasks:** 3 (all complete)
- **Files modified:** 11 (10 production + 1 requirements hygiene)

## Accomplishments

- **New `Provider.Cleanup(ctx, cfg, runID) error` interface method** with three concrete implementations (operator = idempotent delete, cloud = no-op, router = dispatch) plus `MockProvider.CleanupFn` for tests.
- **Real `K6MetricProvider.GarbageCollect` implementation** that walks `ar.Status.MetricResults` for the matching `metric.Name`, iterates Measurements, extracts `Metadata["runId"]`, and dispatches `provider.Cleanup` per non-empty runID.
- **Shared `deleteCR(ctx, runID, source string)` helper** on `K6OperatorProvider` -- factored from `StopRun` and reused by `Cleanup`. Adds a new `source` slog field (`"stop"` vs `"cleanup"`) on Info-level delete logs so operators can grep controller logs for cancel-vs-cleanup deletes. This is a searchable observability win called out by plan-check INFO-5.
- **GC-03 contract honored**: cleanup errors are logged at `slog.Warn` with structured fields (`runId`, `metric`, `analysisRun`, `namespace`, `error`) and NEVER surface as `types.RpcError`. Mirrors how `Terminate` already swallows `StopRun` errors.
- **11 new unit tests** added across four packages (4 operator + 1 cloud + 1 router + 7 metric - 1 deleted stub = +11 net). Total test count: **249 PASS** (up from 238 baseline; plan predicted >=248).

## Task Commits

Each task was committed atomically with a TDD (RED -> GREEN) pair per task:

1. **Task 1 RED: failing Cleanup tests across operator, cloud, router** — `e361d7f` (`test`)
2. **Task 1 GREEN: add Provider.Cleanup for success-path TestRun CR deletion** — `7dc6e14` (`feat`)
3. **Task 2 RED: replace GarbageCollect no-op tests with real measurement-walking cases** — `20dc2f2` (`test`)
4. **Task 2 GREEN: implement K6MetricProvider.GarbageCollect walking ar.Status.MetricResults** — `20d25ab` (`feat`)
5. **Task 3 hygiene: correct GC-01 signature in REQUIREMENTS.md** — `af1f2b6` (`docs`)

Task 3 (whole-package gate) ran no source edits and therefore produced no commit of its own -- it only validated `go build ./...`, `go build -tags=e2e ./e2e/...`, `go test ./... -count=1`, and `make lint` (all green).

**Plan metadata:** appended below by the executor once STATE.md / ROADMAP.md are updated.

## Files Created/Modified

**Provider layer (interface + three impls + mock + router):**

- `internal/provider/provider.go` — added `Cleanup(ctx, cfg, runID) error` to Provider interface with full Godoc distinguishing it from StopRun
- `internal/provider/router.go` — added `Router.Cleanup` dispatch mirroring the existing StopRun dispatch pattern
- `internal/provider/router_test.go` — added `cleanupCalled`/`cleanupRunID` fields to `internalMock` + `TestRouter_CleanupDispatchesToResolvedProvider`
- `internal/provider/providertest/mock.go` — added `CleanupFn` field and delegating `Cleanup` method
- `internal/provider/cloud/cloud.go` — added `GrafanaCloudProvider.Cleanup` no-op (returns nil, logs at slog.Debug)
- `internal/provider/cloud/cloud_test.go` — added `TestCleanup_IsNoOp`
- `internal/provider/operator/operator.go` — refactored `StopRun` to delegate to new private `deleteCR(ctx, runID, source)` helper; added `Cleanup` delegating to the same helper with `source="cleanup"`. Added `source` structured log field on both Info calls inside deleteCR.
- `internal/provider/operator/operator_test.go` — added `TestCleanup_DeletesTestRun`, `TestCleanup_DeletesPrivateLoadZone`, `TestCleanup_NotFound_ReturnsSuccess`, `TestCleanup_InvalidRunID`

**Metric plugin layer:**

- `internal/metric/metric.go` — replaced 3-line `GarbageCollect` no-op with a real walker: parseConfig, populateFromAnalysisRun, walk `ar.Status.MetricResults` for matching `metric.Name`, iterate Measurements, extract `Metadata["runId"]`, dispatch `k.provider.Cleanup` per non-empty runID, swallow errors at Warn, return empty RpcError. Per-call `context.WithTimeout` with `cancel()` at end-of-iteration (NOT defer-in-loop).
- `internal/metric/metric_test.go` — deleted `TestGarbageCollect_ReturnsNilRpcError` stub; added 7 new `TestGarbageCollect_*` cases: CallsCleanupForEachMeasurementRunId, CloudProviderSeesCleanupCalls, LogsWarnOnCleanupErrorAndReturnsNilRpcError, NilAnalysisRun, EmptyMetricResults, MeasurementWithoutRunId, SkipsMeasurementsForOtherMetricNames

**Traceability hygiene:**

- `.planning/REQUIREMENTS.md` — corrected GC-01 to quote the real argo-rollouts v1.9.0 signature (`GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError`) and the real trigger condition (measurement-retention overflow per `analysis/analysis.go:775-800`), replacing the fictional `GarbageCollect(ar, []*Measurement) error` prose. Plan-check INFO-4 follow-up.

## Decisions Made

All decisions were front-loaded in the plan's `<context>` block and executed verbatim. Notable:

- **Add `Cleanup` method rather than reusing `StopRun`.** `StopRun` means "cancel an in-flight run"; `Cleanup` means "release resources after a terminal state." They share the delete path on the operator backend today, but the semantic split keeps a future Kubernetes Job provider free to differentiate (e.g., `kubectl delete --grace-period=0` for StopRun vs `ttlSecondsAfterFinished` for Cleanup).
- **Walk `ar.Status.MetricResults` rather than label-selector-by-`ar.UID`.** Our runIDs include a timestamp hash (`testRunName` in `operator/testrun.go:53`) and are not derivable from `ar.UID`. Measurements carry the authoritative runID per-measurement. Walk-and-dispatch is simpler than list-and-filter, and matches argo-rollouts' own `JobProvider.GarbageCollect` precedent (which walks by label selector only because Jobs carry a deterministic `AnalysisRunUIDLabelKey`).
- **Ignore `limit` parameter in cleanup decision.** Argo Rollouts only calls `GarbageCollect` when `len(measurements) > limit` and trims the slice *after* our return. We clean every runID in the slice, because by the time Argo trims, the earlier measurements are the ones being discarded. `IsNotFound` safety net makes over-deletion free.
- **Sequential loop, no parallelization.** `measurementsLimit` defaults to 2 per AR; slice is tiny in practice. Goroutine/errgroup machinery adds complexity with no latency benefit for a controller-side async GC pass.
- **`cancel()` at end-of-iteration (not defer-in-loop).** `defer cancel()` inside a loop leaks contexts until the enclosing function returns. Inline cancel keeps contexts ephemeral and was called out explicitly in the plan.

## Deviations from Plan

None — plan executed exactly as written. The plan's `<behavior>` and `<action>` blocks specified file-by-file edits with exact code snippets; executor followed them verbatim. The only additive change beyond the plan body was the **INFO-4 follow-up** (REQUIREMENTS.md GC-01 signature patch), which the plan-check had flagged as an optional hygiene item and the executor's scope-guards explicitly instructed to apply.

## Issues Encountered

None. The TDD cycle (RED → GREEN) worked cleanly on both Task 1 and Task 2 without any debugging detours. Build + test + lint gates were green on first attempt at each stage.

## User Setup Required

None — no external service configuration required. RBAC is unchanged from Phase 10 (the `delete` verb on `testruns.k6.io` and `privateloadzones.k6.io` was already granted in `examples/k6-operator/clusterrole.yaml`; verified in plan-check dimension 8).

## Observability Notes

The new `source` structured log field (`"stop"` | `"cleanup"`) on `K6OperatorProvider.deleteCR`'s `slog.Info` calls is a searchable observability improvement. Operators grepping controller logs for:

```
kubectl logs -n argo-rollouts deploy/argo-rollouts | grep '"source":"cleanup"'
```

will see exactly which TestRun CRs were reaped by success-path GC vs which by Terminate/Abort. This was called out in plan-check INFO-5 as worth documenting here for post-v0.4.0 operators.

## Verification Gates (all green)

| Gate | Command | Result |
|------|---------|--------|
| Build | `go build ./...` | PASS (exit 0) |
| E2E build | `go build -tags=e2e ./e2e/...` | PASS (exit 0) |
| Unit tests | `go test ./... -count=1` | PASS (249 tests, 5/5 packages OK) |
| Lint | `make lint` | PASS (0 issues, stdout-protocol check green) |

Structural invariants from the plan's `<verification>` block: all 13 grep assertions PASS.

## Next Phase Readiness

- **Plan 11-02 unblocked.** The new `provider.Cleanup` interface method is in place; the step plugin terminal-state hook (Task 11-02) will call `k.provider.Cleanup(ctx, cfg, runID)` via the Router exactly as the metric plugin now does.
- **Phase 12 combined-canary e2e unblocked.** With success-path cleanup in place, Phase 12's GC-cascade assertion ("AR-owned TestRun disappears after AR deletion, Rollout-owned TestRun survives") is no longer confounded by leaks from the metric plugin path.
- **No blockers. No concerns.**

## Self-Check: PASSED

All 12 files created/modified verified present on disk. All 5 task commits verified present in `git log`.

---
*Phase: 11-success-path-testrun-cleanup*
*Completed: 2026-04-20*
