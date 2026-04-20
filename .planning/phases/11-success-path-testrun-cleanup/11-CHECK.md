# Phase 11 Plan Check — Goal-Backward Verification

**Phase:** 11 — Success-path TestRun cleanup
**Plans checked:** 2 (`11-01-PLAN.md`, `11-02-PLAN.md`)
**Round:** 1 of 2
**Verdict:**
- `11-01-PLAN.md` — **PASS with 2 minor suggestions (INFO)**
- `11-02-PLAN.md` — **PASS with 3 minor suggestions (INFO)**
- **Overall:** proceed to commit and execution. No blockers; no revision required.

---

## Summary scorecard

| Dimension | 11-01 | 11-02 |
|-----------|-------|-------|
| 1. Goal alignment | OK | OK |
| 2. Must-haves completeness | OK | OK |
| 3. Scope boundaries | OK | OK |
| 4. File coverage | OK | OK |
| 5. Test coverage | OK | OK |
| 6. Concurrency/race hazards | OK | OK |
| 7. Interface contract | OK | OK |
| 8. Threat model | OK | OK |
| 9. Verification gates | OK | OK |
| 10. Plan ordering | OK (CONCERN: informational, see below) | OK |
| Context compliance (CLAUDE.md) | OK | OK |
| Scope reduction detection | OK (not triggered) | OK (not triggered) |

---

## Dimension-by-dimension findings

### 1. Goal alignment

**Phase goal** (ROADMAP.md): metric & step plugins delete k6-operator TestRun CRs they created once analysis/step reaches terminal state.

- **11-01** covers GC-01, GC-03, GC-04(a), GC-04(b), GC-04(d) — the metric plugin side. Executing this plan:
  - Extends `Provider` with `Cleanup(ctx, cfg, runID) error`.
  - Rewrites `K6MetricProvider.GarbageCollect` to walk `ar.Status.MetricResults[*].Measurements[*].Metadata["runId"]` and dispatch Cleanup per runID matching `metric.Name`.
  - Grafana Cloud no-op, k6-operator idempotent Delete.
  - Errors swallowed at Warn, empty `RpcError` returned.
  - This delivers success criterion #1 and #3 for the metric side.

- **11-02** covers GC-02, GC-04(c), GC-04(d) — the step plugin side. Executing this plan:
  - Adds `stepState.CleanupDone bool` persisted via `ctx.Status`.
  - Adds a post-switch hook in `Run()` firing once per terminal transition for Passed/Failed/Errored.
  - Aborted, Running, TimedOut correctly excluded.
  - Errors swallowed, `Phase`/`RpcError` unchanged.
  - This delivers success criterion #2 and #3 for the step side.

Together the plans deliver all four ROADMAP success criteria. REQ coverage is complete (GC-01/02/03/04 all appear in at least one plan's `requirements` frontmatter field).

### 2. Must-haves completeness

Both plans' `must_haves` blocks are well-structured:
- **Truths** are user-observable or falsifiable behavioral statements (e.g., "Cleanup called exactly twice with runIDs X, Y in order" for 11-01; "Aborted state does NOT trigger Cleanup in Run()" for 11-02).
- **Artifacts** all point at real source paths verified present in the repo.
- **Key_links** trace real flow: metric.go → router → operator/cloud → dynClient.Delete.

### 3. Scope boundaries

- **11-01**: 3 tasks, 10 files modified + 1 SUMMARY. Narrow by design. The `deleteCR` helper extraction is a scope-preserving refactor — it does not introduce new abstraction, it consolidates an existing branch. The plan explicitly calls out no changes to `cmd/*-plugin/main.go`, no RBAC edits.
- **11-02**: 2 tasks, 2 files modified + 1 SUMMARY. Even narrower.

Neither plan exhibits scope creep. The tasks-per-plan and files-per-plan fall inside the 2-3 / 5-8 sweet spot.

### 4. File coverage

Cross-checked `files_modified` against task `<files>` directives and `<action>` edits:

**11-01** — 10 files listed, 10 touched (4 source + 4 test + 2 dispatch/mock):
- `internal/provider/provider.go` (interface extension) ✓
- `internal/provider/router.go` + `router_test.go` ✓
- `internal/provider/providertest/mock.go` ✓ (only one mock in the repo — confirmed via Grep; no other `MockProvider|mockProvider` test doubles exist)
- `internal/provider/cloud/cloud.go` + `cloud_test.go` ✓
- `internal/provider/operator/operator.go` + `operator_test.go` ✓
- `internal/metric/metric.go` + `metric_test.go` ✓

**11-02** — 2 files listed:
- `internal/step/step.go` + `step_test.go` ✓

All assertions about the current codebase shape verified correct against source:
- `internal/provider/provider.go` lines 40-56 show the Provider interface with exactly TriggerRun/GetRunResult/StopRun/Name as the plan claims.
- `internal/provider/router.go` lines 87-94 match the StopRun dispatch pattern the plan will mirror.
- `internal/provider/operator/operator.go` lines 378-419 match the StopRun delete path the plan factors into `deleteCR`.
- `internal/provider/cloud/cloud.go` lines 192-219 match the StopRun shape.
- `internal/metric/metric.go` lines 57-61 show the no-op GarbageCollect stub to replace.
- `internal/step/step.go` lines 38-43 (stepState) and 163-189 (terminal switch) match the plan's insertion points.
- `examples/k6-operator/clusterrole.yaml` line 21 grants `delete` on `testruns`/`privateloadzones` — RBAC is sufficient, no edits needed as plan claims.

No missed files. No phantom files.

### 5. Test coverage

**11-01** — test cases enumerated (behavior coverage):
- Operator delete path: DeletesTestRun, DeletesPrivateLoadZone, NotFound_ReturnsSuccess, InvalidRunID (4 cases mirroring StopRun).
- Cloud no-op: IsNoOp (1 case).
- Router dispatch: CleanupDispatchesToResolvedProvider (1 case).
- Metric walking: CallsCleanupForEachMeasurementRunId, CloudProviderSeesCleanupCalls, LogsWarnOnCleanupErrorAndReturnsNilRpcError, NilAnalysisRun, EmptyMetricResults, MeasurementWithoutRunId, SkipsMeasurementsForOtherMetricNames (7 cases).

This covers every must_haves truth and directly maps to GC-01/03/04(a,b,d). The SkipsMeasurementsForOtherMetricNames case is especially valuable — it guards against a subtle regression where a change to metric.Name matching could silently delete CRs belonging to another metric.

**11-02** — test cases enumerated:
- TerminalPassed/Failed/Errored_FiresCleanupOnce (3 positive)
- TerminalAborted_DoesNotFireCleanup + Running_DoesNotFireCleanup (2 negative)
- CleanupDoneGuardPreventsDoubleFire (guard invariant)
- CleanupError_DoesNotChangePhaseAndSetsCleanupDone (GC-03)

All seven map to GC-02/04(c,d). Failure path coverage (cleanup error → no phase change) is explicit.

**INFO-1 (11-02):** no test is specified for the TimedOut path interacting with the new hook. The plan asserts in <interfaces> that timeout returns early before reaching the new block, which is structurally correct given the current code (timeout branch at step.go:134-151 returns unconditionally). A defensive `TestRun_TimedOut_DoesNotFireCleanupInNewBlock` test would lock in that invariant, but it's redundant with the structural guarantee — listing here as INFO only.

### 6. Concurrency/race hazards

- **11-01 GarbageCollect loop**: `cancel()` placement is correctly called out as end-of-iteration (not `defer` in loop). Sequential per-measurement execution is explicitly justified by the small slice size.
- **11-02 CleanupDone guard**: "Cleanup invoked at most once per terminal transition" is correct — the guard reads `state.CleanupDone` from the unmarshaled ctx.Status, and the field is serialized back out before Run() returns. A replay Run() after terminal will see CleanupDone=true and skip. The order (fire, then set, then marshal) is explicit in the hook block.

One subtle point the plan handles correctly: `CleanupDone = true` is set **after** the slog.Warn block, outside the `if cleanupErr != nil` branch — so the flag flips regardless of error. This matches the "best-effort, no retry" contract from REQUIREMENTS.md `Retry loop on cleanup failure` Out of Scope.

**INFO-2 (11-02):** `stepState.CleanupDone = true` is assigned *after* `cleanupCancel()`. This is fine semantically, but the block as written in `<interfaces>` has `cleanupCancel()` outside the error check. If `cleanupCancel()` were ever wrapped in `defer`, the flag-assignment-after-defer-cancel pattern would silently work — but the current inline `cleanupCancel()` is the correct pattern. No action required, just confirming the executor reads the block as-written.

### 7. Interface contract

`Cleanup` is being added to `Provider`. All implementations must satisfy it. Repo audit:
- `GrafanaCloudProvider` → plan adds method ✓
- `K6OperatorProvider` → plan adds method ✓
- `Router` → plan adds method ✓
- `providertest.MockProvider` → plan adds method + `CleanupFn` field ✓

Verified via Grep that no other `provider.Provider` implementer exists outside these four. No `var _ provider.Provider` in e2e/ or cmd/. The interface extension is clean.

Compile-time checks already present (`var _ provider.Provider = (*GrafanaCloudProvider)(nil)` and `var _ provider.Provider = (*K6OperatorProvider)(nil)` and `var _ Provider = (*Router)(nil)`) will catch any missed implementation at `go build` time — the plan doesn't need to enumerate these but benefits from them.

### 8. Threat model

Both plans ship STRIDE registers. Key points:
- Cleanup uses the SAME RBAC verb on the SAME resources as StopRun — no privilege escalation (T-11-07 / T-11-11).
- `decodeRunID` validates format — tampered runIDs cannot widen the blast radius (T-11-02 / T-11-12).
- Tampered `CleanupDone` flag can only suppress a delete or cause a redundant NotFound-safe delete (T-11-08). Not an escalation.
- No new trust boundary, no new credentials exposed.

Threat registers are disposition-complete; dispositions are well-justified.

### 9. Verification gates

**11-01** gates:
- `go build ./...` + `go build -tags=e2e ./e2e/...` + `go test ./... -count=1` + `make lint`.
- Structural grep assertions are extensive (15+ greps covering function presence, test presence, old-test removal).

**11-02** gates: same four commands + 12 grep assertions.

Both plans include the `-tags=e2e` build step — this is crucial because the e2e package imports `provider.Provider`, and a missed implementation there would blow up at e2e-build time. Good catch.

`make lint` will catch unused-parameter issues if any; the plan has a fallback instruction for blank-identifier binding. OK.

### 10. Plan ordering & dependency

Both plans declare `depends_on: []` and `wave: 1`. Strictly speaking, 11-02 **compile-depends on** 11-01 (the `k.provider.Cleanup` call in step.go won't resolve until the interface method is added). The plan acknowledges this in its `<file_location_note>`: "11-02 must merge AFTER 11-01."

**CONCERN (informational, not a blocker):** `depends_on: []` in 11-02's frontmatter does NOT encode this ordering. If an orchestrator reads only `depends_on` to parallelize, it could try to execute 11-02 before 11-01 and the 11-02 RED tests (which reference `CleanupFn`) would fail to compile for the wrong reason. The plan body does document the order clearly, so a human executor will do the right thing. Two options:
- **(preferred)** Update 11-02 frontmatter to `depends_on: ["01"]` and `wave: 2`. This makes the ordering machine-readable.
- **(acceptable)** Leave as-is and rely on the `<file_location_note>` prose.

Given the GSD workflow runs these sequentially in a single executor (not a parallel orchestrator), either is fine for this phase. Flagging as INFO-3 for surface awareness.

### Context compliance (CLAUDE.md)

CLAUDE.md project directives the plans must respect:
- **Go 1.23+** → plans use stdlib `log/slog` (Go 1.21+) and `context.WithTimeout` (stdlib). OK.
- **D-12 slog over logrus** → plans log via `slog.Warn` / `slog.Debug` / `slog.Info`. OK.
- **Plugin interface versioned, no breaking changes** → plans do NOT modify the Argo Rollouts RPC contract. The `Cleanup` method is on the internal `provider.Provider`, not the upstream `MetricProviderPlugin`. OK — `<file_location_note>` in 11-01 explicitly calls this out.
- **k6-cloud-openapi-client-go** for Grafana Cloud → no API interaction needed (Cleanup is a no-op). OK.
- **testify for tests** → plan uses `assert.*` / `require.*`. OK.
- **k8serrors.IsNotFound idempotent pattern** → plan preserves it. OK.

No CLAUDE.md violation.

### Scope reduction detection

No language like "v1", "simplified", "static for now", "placeholder", "future enhancement" appears in either plan's `<action>` / `<behavior>` blocks. Plans deliver GC-01/02/03/04 fully, not a reduced shadow.

---

## Open-question verifications from planner memory

The verification prompt flagged two items the planner should have surfaced IN the plans themselves:

### Item A: GarbageCollect timing semantics (called on measurement-retention overflow, not "after terminal analysis")

The upstream REQUIREMENTS.md GC-01 says "when called by Argo Rollouts after terminal analysis." The real trigger is `len(measurements) > measurementsLimit` (per `argo-rollouts analysis/analysis.go:775-800`).

**Where the plan documents this:**
- 11-01 `<objective>` lines 93-94: *"When Argo Rollouts invokes `GarbageCollect(ar, metric, limit)` — which it does to enforce the measurement retention limit per metric — the plugin walks..."* ✓ visible at top level.
- 11-01 `<interfaces>` block quotes `analysis/analysis.go:775-800` with the real invocation path. ✓ visible.
- 11-01 `<context>` design-decisions block reiterates: *"Argo Rollouts only calls `GarbageCollect` when `len(measurements) > limit`"* ✓ visible.

**Verdict: captured clearly at the objective level and in context. Not hidden in a comment.** PASS.

### Item B: REQUIREMENTS.md GC-01 signature is fictional

REQUIREMENTS.md GC-01 quotes `GarbageCollect(ar *v1alpha1.AnalysisRun, metrics []*v1alpha1.Measurement) error`. This signature does not exist; the real one is `GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError`.

**Where the plan documents the mismatch:**
- 11-01 `<context>` design-decisions explicitly says: *"REQUIREMENTS.md states a fictional signature. GC-01 quotes [fictional signature]. That signature does not exist in Argo Rollouts v1.9.0 — the real interface is [real signature]. We honor the REAL signature because the plugin gRPC contract is versioned and a hard constraint."* ✓ visible.

**Missing:** the plan does not include a task or acceptance criterion to UPDATE `REQUIREMENTS.md` inline during execution. The mismatch is acknowledged but left in the requirements document for future readers to re-discover.

**INFO-4 (11-01):** consider adding a Task 4 (or extending Task 3) that patches REQUIREMENTS.md GC-01 to reflect the real signature once the GREEN phase lands. Or add it to the SUMMARY follow-up. This is hygiene, not a blocker — execution will succeed either way because the plan follows the real interface.

---

## Top 3 findings per plan

### 11-01 (all INFO — no blockers)

1. **INFO-4:** REQUIREMENTS.md GC-01 contains a fictional signature. The plan acknowledges it but doesn't schedule the correction. Consider adding a doc-patch step or a follow-up note to the SUMMARY.
2. **INFO-5:** The `source` string field added to `deleteCR`'s slog.Info calls (values `"stop"` / `"cleanup"`) is a nice observability improvement. Consider documenting it in the summary as a searchable log key for post-v0.4.0 operators.
3. **INFO-6 (forward compat):** The plan's design-decisions section correctly anticipates that a future Job provider may differentiate StopRun vs Cleanup. The `Cleanup` method's Godoc (per plan) includes this rationale — good.

### 11-02 (all INFO — no blockers)

1. **INFO-1:** No test for `TimedOut → no new hook fire`. Structurally implied by the early-return in the timeout branch, but a 3-line defensive test would lock it in.
2. **INFO-3:** `depends_on: []` doesn't encode the compile-dependency on 11-01. Non-blocking for a sequential executor, potentially confusing for a parallel orchestrator. Consider `depends_on: ["01"]`.
3. **INFO-7 (backward compat):** The plan notes existing `TestRun_Terminal_Passed/Failed/Errored` tests continue to PASS because `mockProvider{}` without a `CleanupFn` returns nil from Cleanup. This is correct, and the SUMMARY should call it out as a proof of the additive-only contract.

---

## Recommendation

**Proceed to commit and execute.** Both plans are production-ready. The INFO items are refinements, not defects. Round 1 closes cleanly; round 2 is not needed.

Suggested executor order:
1. Execute 11-01 (interface extension + metric plugin). Commit at RED, GREEN, and after whole-plan gate.
2. Execute 11-02 (step plugin hook). Commit at RED, GREEN, and after whole-plan gate.
3. (Optional, during 11-01 execution or afterward) patch REQUIREMENTS.md GC-01 signature inline — INFO-4.

**Re-verification after execution** (future `gsd-verifier` invocation) should check:
- REQUIREMENTS.md GC-01 is now accurate (if INFO-4 action taken).
- Unit-test count delta: +11 (11-01) + 7 (11-02) = +18 over v0.3.0 baseline of 237 → expect ≥ 255 tests.
- No regression on existing TestStopRun_*, TestRun_Terminal_*, TestTerminate_*, TestAbort_* suites.

---

*Checked: 2026-04-20, Round 1 of 2. No blockers. PASS with 6 INFO suggestions across both plans.*
