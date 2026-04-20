# Phase 12 Plan Check — `12-01-PLAN.md`

**Verdict:** NEEDS_REVISION

**Date:** 2026-04-20
**Plan:** `.planning/phases/12-combined-canary-e2e-owner-ref-gc-cascade/12-01-PLAN.md`
**Requirement:** TEST-02 (prove D-07 owner-ref precedence under real kube-apiserver cascade GC)

The plan is largely sound in structure and intent, but contains **two blocker-level issues** (one factual k6-operator stage enum error that invalidates core wait logic; one architectural race with Argo's background-AR recreation that can flake the cascade assertion) and several warnings.

---

## Blockers

### B-1 — `stage == "running"` will never match (k6-operator Stage enum is factually wrong in the plan)

The plan waits for both TestRuns to reach `stage == "running"` before issuing `kubectl delete analysisrun`:

```go
// (2) Wait for both to reach stage "running".
runningDeadline := time.Now().Add(3 * time.Minute)
for time.Now().Before(runningDeadline) {
    arStage, _ := getTestRunStage(cfg, arTRs[0], cfg.Namespace())
    roStage, _ := getTestRunStage(cfg, roTRs[0], cfg.Namespace())
    if arStage == "running" && roStage == "running" {
        break
    }
    time.Sleep(3 * time.Second)
}
```

And the helper Godoc and plan's `truths` / `key-decisions` repeatedly state `running` is a valid k6-operator stage. **It isn't.** The k6-operator CRD v1.3.2 enumerates:

```
initialization;initialized;created;started;stopped;finished;error
```

(source: `$GOPATH/pkg/mod/github.com/grafana/k6-operator@v1.3.2/api/v1alpha1/testrun_types.go:172`, `// +kubebuilder:validation:Enum=initialization;initialized;created;started;stopped;finished;error`)

Our own `internal/provider/operator/exitcode.go:47-62 stageToRunState` confirms: stages `initialization|initialized|created|started` all map to `provider.Running`, and `finished` requires an exit-code probe to distinguish pass/fail. **There is no stage literal named `"running"` in k6-operator.**

Consequences if the plan ships as written:
- The loop never hits the break condition and always runs the full 3-minute deadline.
- Because there is no `t.Fatalf` on timeout, the test proceeds silently to step (3) regardless of actual stage. The precondition "both TestRuns are in-flight before the AR delete" is not enforced — the AR may be deleted while runs are still at `initialized` (runner pods not yet scheduled) or `finished` (already done), either of which muddies the "cascade under real load" narrative that TEST-02 is meant to prove.
- Worse: if the 120s k6 duration is shorter than the 3-minute silent wait on a slow kind cluster, both TestRuns could reach `finished` before the delete fires — cascade GC would still work (kube doesn't care about stage), but the test headline "while both TestRuns are in stage running" is false.

**Fix:**
1. Wait for stage `"started"` (runner pods have launched and k6 is executing).
2. Add an explicit `t.Fatalf` (with `dumpK6OperatorDiagnostics`) when the deadline is reached without both TestRuns reaching `started` — do not fall through silently.
3. Update all plan truths, Godoc, and the `getTestRunStage` comment to reflect the real enum values.
4. Update the `truths` entries that mention `running` and the `objective` paragraph ("while both TestRun CRs are in `running` stage" → "while both TestRun CRs are in flight (stage `started`)").

### B-2 — Argo Rollouts will recreate the background AR after the test deletes it (flake risk / assertion race)

Plan uses `spec.strategy.canary.analysis.templates[]` with `startingStep: 1`. Once the test deletes the background AR mid-run, Argo's next reconcile of the Rollout hits:

```go
// rollout/analysis.go:347-372
func (c *rolloutContext) reconcileBackgroundAnalysisRun() (*v1alpha1.AnalysisRun, error) {
    currentAr := c.currentArs.CanaryBackground   // nil after delete
    ...
    if needsNewAnalysisRun(currentAr, c.rollout) { // returns true when currentAr==nil
        ...
        currentAr, err := c.createAnalysisRun(...)  // NEW background AR
        ...
    }
```

Argo will re-create the background AR within one reconcile cycle (informer-driven, typically seconds). The new AR's metric plugin will then create a new AR-owned TestRun. The test's loop:

```go
// (4) Assert AR-owned TestRun disappears within 90s
for time.Now().Before(cascadeDeadline) {
    arRemaining, err = getTestRunsOwnedBy(cfg, cfg.Namespace(), "AnalysisRun", "")
    if err == nil && len(arRemaining) == 0 { break }
    time.Sleep(3 * time.Second)
}
```

- May transiently see `len==0` between the kube-cascade-GC completion and the second TestRun being created (pass), OR
- May see `len==1` permanently if reconcile + TestRun creation outpaces kube GC (fail).
- Either way, the assertion becomes non-deterministic.

The plan's `<interfaces>` section explicitly rejected the dedicated-analysis-step alternative in favor of background analysis to make the two TestRuns overlap in time — a valid motivation — but did not then address the delete-and-respawn consequence that comes with background analysis.

**Fix options (pick one):**

(a) **Filter by the specific AR name, not just Kind.** Change the assertion from "no AR-owned TestRuns" to "no TestRun owned by THIS AR (the one we deleted)". The helper already supports `ownerName`; the test just needs to capture the name before delete and pass it in:
```go
// capture AR name before delete
arName := ars[0]
// ... delete ...
// assertion loop filters by ownerName too:
arRemaining, _ = getTestRunsOwnedBy(cfg, ns, "AnalysisRun", arName)
```
By-name filtering makes the assertion robust to re-created-AR noise.

(b) **Remove `canary.analysis` from the Rollout spec immediately before deleting the AR** via `kubectl patch`, so Argo does not re-create it. Coarser; couples the test to kubectl-patch semantics.

(c) **Delete the Rollout before asserting.** Simplest but inverts the test intent — the surviving Rollout-owned TestRun assertion then measures something different (surviving the Rollout delete vs surviving the AR delete).

Option (a) is the minimal, most honest fix. Plan must be updated to pass the captured AR name into the poll.

---

## Warnings

### W-1 — Rollout-owned survivor assertion has the same filter problem as B-2 in reverse

Step (6) reads the managed-by label off `roSurvivors[0]`. If a second AR is re-created (per B-2) and then its TestRun completes and Phase 11's cleanup path fires while the test is still running, the `roSurvivors[0]` from the first list may point to a TR that has since been deleted. The label read then fails. Low probability but non-zero.

**Fix:** After `getTestRunsOwnedBy(cfg, ns, "Rollout", "k6-combined-canary-k6op-e2e")` returns, loop over all survivors and verify at least one is present with the label; or re-fetch within the label check to tolerate the moving-target case.

### W-2 — `parentOwnerRef` sets neither `Controller` nor `BlockOwnerDeletion`

Reading `internal/provider/operator/testrun.go:79-97` shows `parentOwnerRef` returns OwnerReferences with only `APIVersion`, `Kind`, `Name`, `UID` populated. Kubernetes cascade GC operates on `metadata.ownerReferences` regardless of `Controller`/`BlockOwnerDeletion` (default Background policy deletes children after a short delay). The 90s cascade budget is comfortable for kind — default-mode Background cascade latency on kind is typically 5-30s. Not a blocker, but document the rationale in the plan (why the 90s figure is conservative enough) to pre-empt revisiting if CI shows 60-90s latencies.

### W-3 — Shared ConfigMap means both TestRuns mount the same script

Both TestRuns reference `configMapRef.name: k6-e2e-script-long`. That is fine for "both TestRuns produce load concurrently" but risks identical `test/run` annotation collisions if Phase 12 is ever re-run back-to-back in the same namespace within the same test session (testenv reuses the namespace per run). The Setup block does `kubectl delete testruns --all` and `analysisruns --all` before the patch, but does NOT delete `analysistemplate/k6-operator-combined-e2e` between test iterations within a single `make test-e2e` run. Not an issue on first run; could be noise if the suite is re-run within a single `go test` invocation via `-count=N`. Low priority.

**Fix:** Add `kubectl delete analysistemplate k6-operator-combined-e2e --ignore-not-found` to Setup's prelude (or rely on Teardown always running — but Teardown only fires on test completion, not on test iteration).

### W-4 — Diagnostic dump on the stage-polling-timeout path is missing

If B-1 is fixed by adding a `t.Fatalf` on "stage never reached `started`", the fix must include `dumpK6OperatorDiagnostics(cfg, cfg.Namespace())` before the fatal. The plan's other assertion points all do this; the stage wait would be an outlier.

### W-5 — Wall-clock budget

New test budget is ~5 min (120s k6 + 90s cascade + up to 4 min both-exist + 3 min both-started polling windows + teardown). Current suite is 6/6 in 261s. Worst-case the suite grows to ~560s — still comfortably under Go's default 10-minute test timeout and under typical CI per-job limits. Not a blocker but worth noting in the SUMMARY template.

---

## Per-Dimension Verdict

| Dimension | Verdict | Note |
|-----------|---------|------|
| 1. Goal alignment (D-07 + real cascade) | CONCERN | Intent aligned with TEST-02; B-1 and B-2 undermine the actual assertion chain. |
| 2. Timing design (120s vs polling) | FAIL | B-1: wait loop condition is factually wrong; silent timeout. |
| 3. Background analysis semantics (`startingStep: 1`) | PASS | `startingStep: 1` with steps `[setWeight(20), plugin, setWeight(100)]` correctly starts the AR when the plugin step (index 1) begins. Confirmed via `argo-rollouts@v1.9.0/utils/replicaset/canary_test.go:1097-1124` behavior table. |
| 4. AR discovery (`listAnalysisRuns[0]`) | PASS | Setup deletes `analysisruns --all` before patch; namespace-scoped; one background AR per revision. Deterministic for the happy path. |
| 5. Owner-ref selector mechanics (`getTestRunsOwnedBy`) | PASS | JSON unmarshal filters by `ownerReferences[].kind`+optional `name`. Robust to extra ref fields — future additions don't break (match-any loop). |
| 6. Cascade timeout (90s) | PASS | Comfortable on kind; kube Background GC typical latency 5-30s even under load. |
| 7. Teardown correctness | PASS | Teardown deletes all resources with `--ignore-not-found`. Order is correct (Rollout → AT → AR → Service → CM → TestRuns). |
| 8. No scope creep | PASS | Plan's `files_modified` lists exactly 3 files (test file + 2 testdata). Scope guards explicitly forbid `internal/**`, RBAC edits, or existing-test modifications. |
| 9. RBAC | PASS | Test kubectl runs under kind cluster-admin kubeconfig, not the plugin SA; RBAC restrictions do not apply. Plugin SA already has `delete` on `testruns` (unneeded here but confirmed). |
| 10. Interaction with Phase 11 | PASS | Phase 11's `Provider.Cleanup` is independent of kube cascade; deleting the AR does not trigger the plugin's cleanup path (plugin stops being called once AR is gone). Kube cascade proceeds regardless. The assertions hold. |
| 11. Flake risk + wall-clock | CONCERN | B-2 introduces a real race flake risk. W-5 bumps CI budget ~300s. |
| 12. Requirement coverage (TEST-02) | PASS | Single plan addresses the single requirement; `requirements` frontmatter lists `TEST-02`; must_haves map coherently to ROADMAP truths 1-4. |
| 13. Task completeness | PASS | Task 1 has files, action, verify (automated command), acceptance_criteria, done. |
| 14. Dependency correctness | PASS | Wave 1, depends_on: [], requires Phase 11 (satisfied, 11-01/11-02 both shipped 2026-04-20). |
| 15. CLAUDE.md compliance | PASS | Go/k8s stack; slog (D-12); e2e tag; testify implicit via test harness; no prohibited tools. |
| 16. Context compliance | N/A | No CONTEXT.md for this phase. |

---

## Recommendation

**NEEDS_REVISION.** The plan is close to landing but requires two concrete edits before execution:

1. **(B-1)** Replace `"running"` with `"started"` throughout the plan (plan body, `truths`, Godoc in `getTestRunStage`, all wait-loop references). Add an explicit `t.Fatalf` + `dumpK6OperatorDiagnostics` when the 3-minute stage-wait deadline elapses without both TestRuns reaching `started`.
2. **(B-2)** Capture the AR name before deleting, pass it into the cascade assertion loop as the `ownerName` filter, so the assertion is robust to Argo's background-AR recreation behavior.

Warnings W-1..W-5 can be folded into the same revision or left as follow-ups — W-1 is the most worth fixing alongside the blockers since it touches the same survivor-check code.

**After revision**, re-run plan-check. No other dimensions require attention; structural scaffolding (helpers, fixture shapes, teardown, scope guards, dependency graph, RBAC analysis) is solid.

---

## Round 2

**Date:** 2026-04-20
**Verdict:** NEEDS_REVISION (minor — 2 prose inconsistencies; code-path blockers resolved)

### B-1 — `"running"` → `"started"` stage literal

**Status:** PARTIAL.

What was fixed (code + most prose):
- Code block at lines 563-581 now waits for `arStage == "started" && roStage == "started"` (lines 570-577).
- Fatal-on-timeout branch added at lines 578-581 with `dumpK6OperatorDiagnostics(cfg, cfg.Namespace())` before `t.Fatalf`. Closes W-4.
- `getTestRunStage` Godoc (lines 710-714) correctly documents the real enum and notes no `"running"` literal exists.
- Truth #5 (line 22) now correctly states: "while both TestRuns are in stage `started` ... there is no `running` literal".
- Design-decisions bullet at line 421 correctly reframes the guard.
- Success criterion ROADMAP truth 2 (line 869) correctly says "stage `started`".
- SUMMARY design-call bullet (line 886) correctly documents "Wait for stage `started`".

What was missed (two direct carryovers from B-1's fix list "Update all plan truths ... and the objective paragraph"):

1. **Line 27 (truth entry for the long-script ConfigMap):**
   ```
   "... so both TestRuns stay in `running` stage long enough for the AR-delete window ..."
   ```
   This backticked `\`running\`` stage-literal contradicts truth #5 (line 22) in the same block which correctly says "there is no `running` literal". Internally inconsistent must_haves.

2. **Line 73 (`<objective>` paragraph):**
   ```
   "... then — while both TestRun CRs are in `running` stage — deletes the AnalysisRun ..."
   ```
   Round-1 B-1 fix explicitly called out: "Update the `truths` entries that mention `running` and the `objective` paragraph". The objective paragraph was not updated.

The runtime behavior is correct (the code uses `"started"`); these two lines are a documentation/consistency defect only. But they are the exact strings flagged as "update all truths and the objective paragraph" in Round-1, so the revision is incomplete.

Acceptable neighbor references (not issues):
- "long-running k6 script" / "long-running" throughout (prose adjective — not a stage literal).
- "poll-for-running" at line 419 (shorthand for the polling phase — not an enum claim).
- Lines 339, 565, 712 explicitly say `there is no "running" literal` — these are correct disclaimers.

### B-2 — Cascade poll filters by captured AR name

**Status:** RESOLVED.

- Line 594: `arName := ars[0]` captures the AR name BEFORE `runKubectl("delete", "analysisrun", arName, ...)` at lines 595-599.
- Line 607: `getTestRunsOwnedBy(cfg, cfg.Namespace(), "AnalysisRun", arName)` — cascade poll filters by the captured name (not `""`).
- Line 613-616: fatal message references the captured `arName` so a failure is attributed to the specific AR that was deleted.
- Step (3) rationale comment (lines 583-588) correctly documents: "Capture the name BEFORE the delete; the cascade poll in (4) filters by this captured name so a potential Argo-recreated background AR (the AnalysisRun controller reconciles the missing background AR via reconcileBackgroundAnalysisRun) cannot contaminate the assertion with a new AR-owned TestRun."
- Step (4) comment (lines 601-603) reiterates: "Filter cascade poll by the CAPTURED arName so a recreated background AR's TestRun is not counted as 'still present'."
- Truth #5 on line 22 explicitly documents the design decision: "The cascade-poll filter uses the captured AR name so a potential Argo-recreated background AR's TestRun is not conflated with the deleted one."
- ROADMAP truth 2 (line 869) and SUMMARY design call (line 887) both document the rationale.

Design decision is captured in BOTH the code comment AND the must_haves truths. Closes B-2.

### Regressions & new issues

None found. Spot-checks:
- All four helpers (`getTestRunsOwnedBy`, `getTestRunStage`, `listAnalysisRuns`, `getTestRunLabel`) still present with correct signatures.
- `canary.analysis.templates[0].templateName` + `startingStep: 1` fixture shape unchanged.
- Teardown still deletes Rollout, AnalysisTemplate, AnalysisRuns, Service, ConfigMap, TestRuns with `--ignore-not-found`.
- Task has files + action + verify (automated) + acceptance_criteria + done.
- Scope guards and `files_modified` (3 files) unchanged.
- Threat model and verification-gates blocks unchanged.
- Requirement frontmatter `TEST-02` unchanged; ROADMAP alignment preserved.
- W-1..W-5 from Round 1 remain as noted (W-4 is now addressed by the fatal-on-timeout branch; W-1/W-2/W-3/W-5 stand as previously triaged low-priority).

### Final verdict

**NEEDS_REVISION (minor).** Two surgical text edits close everything:

1. Line 27: replace `` `running` stage `` with `` `started` stage `` (or reword: "stay in-flight during the AR-delete window").
2. Line 73: replace `` `running` stage `` with `` `started` stage `` in the `<objective>`.

B-1 code path is correct; B-2 is fully resolved. If those two one-word edits land, plan is ready to execute. No need for a Round-3 deep review afterward — a glance at those two lines suffices.

**Recommendation:** Revise those two lines, then proceed to commit. Do NOT loop through the full plan-check again — the behavioral blockers are closed; only internal-consistency text fixes remain.
