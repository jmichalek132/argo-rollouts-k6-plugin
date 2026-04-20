---
phase: 11-success-path-testrun-cleanup
plan: 02
subsystem: cleanup
tags: [argo-rollouts, step-plugin, garbage-collect, cleanup, terminal-state, slog, testify]

# Dependency graph
requires:
  - phase: 11-01
    provides: "Provider.Cleanup(ctx, cfg, runID) error interface method + K6OperatorProvider.Cleanup (idempotent CR delete) + GrafanaCloudProvider.Cleanup (no-op) + Router.Cleanup dispatch + providertest.MockProvider.CleanupFn"
  - phase: 08.3-remove-spec-cleanup-post-testrun-gc-d-before-plugin-can-read
    provides: "removal of spec.Cleanup=post on TestRun CRs -- the race that forced success-path cleanup into the plugin"
  - phase: 08.1-wire-analysisrun-rollout-metadata-through-plugin-layers
    provides: "populateFromRollout plumbing -- step plugin uses cfg.RolloutName/Namespace in Cleanup slog.Warn log fields"
provides:
  - "stepState.CleanupDone bool field (json:\"cleanupDone,omitempty\") persisted across Run() via ctx.Status"
  - "K6StepPlugin.Run success-path terminal-state cleanup hook: fires provider.Cleanup once for Passed/Failed/Errored"
  - "CleanupDone guard semantics: once-per-terminal-transition, survives reconciliation-race replay"
affects:
  - "Phase 12 combined-canary e2e (GC-02 exercised end-to-end alongside GC-01 from 11-01)"
  - "future Job provider (step plugin wiring is provider-agnostic -- Cleanup flows through Router)"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "CleanupDone boolean guard on persisted stepState for once-per-terminal-transition semantics"
    - "best-effort cleanup with state.CleanupDone=true set regardless of outcome (GC-03, no retry)"
    - "structured slog.Warn with runId/rollout/namespace/state/error fields; no RpcError surface"
    - "inline context.WithTimeout + cancel() (not defer) matches the existing StopRun-on-timeout idiom in Run()"

key-files:
  created: []
  modified:
    - "internal/step/step.go -- stepState.CleanupDone field + post-switch cleanup hook in Run()"
    - "internal/step/step_test.go -- 7 new tests + local makeStateWithCleanup helper"

key-decisions:
  - "Separate CleanupDone field vs reusing FinalStatus -- FinalStatus is set in the SAME Run() call BEFORE the cleanup hook fires, so a check on FinalStatus != \"\" would skip cleanup on the very first terminal observation. Distinct CleanupDone flag fires cleanup on the first observation and suppresses it only on later reconciliation replays."
  - "Hook location inside Run() post-switch, not in a separate method -- Run() is the natural observation point where Argo Rollouts sees state transitions; first terminal observation is the natural trigger."
  - "Aborted excluded -- Terminate/Abort RPCs already fired StopRun via stopActiveRun (D-07). On k6-operator backend StopRun and Cleanup share deleteCR; firing Cleanup here would be a redundant NotFound-as-success roundtrip."
  - "TimedOut excluded -- the timeout branch (step.go:134-151) already calls StopRun and returns early; the new hook is never reached on that path."
  - "CleanupDone=true set regardless of Cleanup outcome -- best-effort per GC-03; REQUIREMENTS.md \"Retry loop on cleanup failure\" is explicitly Out of Scope. Single attempt, log warning, move on."
  - "Fresh context.Background() + context.WithTimeout per Cleanup call (not an ambient ctx) -- mirrors TriggerRun/GetRunResult/StopRun pattern in the existing Run() code. types.RpcStepContext is not a context.Context."
  - "INFO-1 (plan-check) optional TimedOut-no-fire defensive test was NOT added -- already structurally guaranteed by the early-return in the timeout branch; adding the 3-line guard would have duplicated the structural invariant without increasing coverage."
  - "INFO-7 (plan-check) backward-compat is structural -- mockProvider{} with no CleanupFn set returns nil from Cleanup (providertest/mock.go L45-48), so the 4 existing TestRun_Terminal_* tests continue to PASS without modification."

patterns-established:
  - "Once-per-terminal-transition guard via boolean flag in persisted ctx.Status (generalizable to any post-terminal hook the step plugin may want to add in the future)"
  - "Best-effort cleanup contract mirrored across metric plugin (GarbageCollect, 11-01) and step plugin (Run terminal hook, 11-02): log at Warn, always return success"

requirements-completed: [GC-02, GC-03, GC-04]

# Metrics
duration: 3min
completed: 2026-04-20
---

# Phase 11 Plan 02: Success-path TestRun Cleanup (Step Plugin) Summary

**Step plugin `Run()` now fires `provider.Cleanup` exactly once on the first observation of a Passed/Failed/Errored terminal state, closing the step-plugin half of the success-path TestRun CR leak that Phase 08.3 deferred.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-20T12:55:53Z
- **Completed:** 2026-04-20T12:58:55Z
- **Tasks:** 2 (both complete)
- **Files modified:** 2 (`internal/step/step.go`, `internal/step/step_test.go`)

## Accomplishments

- **`stepState.CleanupDone bool` field** added with JSON tag `cleanupDone,omitempty`. Persisted across Run() invocations via `ctx.Status`. Guards the cleanup hook from re-firing on controller reconciliation-race replays.
- **Success-path terminal-state cleanup hook in `K6StepPlugin.Run()`** — a 15-line block inserted between the terminal switch and the `json.Marshal(state)` return. Fires `k.provider.Cleanup(ctx, cfg, state.RunID)` exactly once when `result.State ∈ {Passed, Failed, Errored}` AND `!state.CleanupDone` AND `state.RunID != ""`.
- **GC-03 contract honored** — cleanup errors are logged at `slog.Warn` with structured fields (`runId`, `rollout`, `namespace`, `state`, `error`) and NEVER alter `result.Phase`, `result.Message`, or the returned `RpcError`. `state.CleanupDone` flips to true regardless of outcome (no retry loop per REQUIREMENTS.md Out of Scope).
- **7 new unit tests** in `internal/step/step_test.go` (+7 net, bringing total from 249 to **256 PASS**). Coverage: positive cases (Passed/Failed/Errored fire Cleanup once), negative cases (Aborted/Running do NOT fire), once-per-transition guard (CleanupDone=true suppresses replay fire), GC-03 error-swallow invariant.
- **Zero impact on existing paths** — `stopActiveRun` (Terminate/Abort helper) and the timeout branch are untouched; both already delete the CR idempotently via StopRun/deleteCR. The 4 prior `TestRun_Terminal_*` tests continue to PASS unchanged (backward-compat is structural — `mockProvider{}` with no `CleanupFn` returns nil).

## Task Commits

Each task was committed atomically in a TDD (RED → GREEN) pair:

1. **Task 1 RED: failing terminal-state cleanup hook tests** — `1ba2595` (`test`)
2. **Task 1 GREEN: step plugin fires provider.Cleanup on success-path terminal state** — `f4bd93d` (`feat`)

Task 2 (whole-package gate) ran no source edits and therefore produced no commit of its own — it only validated `go build ./...`, `go build -tags=e2e ./e2e/...`, `go test ./... -count=1`, and `make lint` (all green).

**Plan metadata commit** (SUMMARY + STATE + ROADMAP): appended below by the executor.

## Files Created/Modified

**Step plugin layer (two targeted edits, no refactor):**

- `internal/step/step.go`
  - `stepState` struct extended with `CleanupDone bool` field (json tag `cleanupDone,omitempty`). Godoc spells out the GC-03 best-effort semantics and the backward-compat invariant (absent field deserializes as `false`).
  - `Run()` post-switch hook: new `isSuccessPathTerminal := result.State == provider.Passed || ... Errored` block; `if isSuccessPathTerminal && !state.CleanupDone && state.RunID != ""` fires `k.provider.Cleanup` once with a fresh `context.WithTimeout`; errors log at `slog.Warn`; `state.CleanupDone = true` flips regardless of outcome.

- `internal/step/step_test.go`
  - 7 new tests appended after the existing `TestRun_Terminal_Aborted`:
    - `TestRun_TerminalPassed_FiresCleanupOnce` (positive + cfg-plumbing assertion)
    - `TestRun_TerminalFailed_FiresCleanupOnce`
    - `TestRun_TerminalErrored_FiresCleanupOnce`
    - `TestRun_TerminalAborted_DoesNotFireCleanup`
    - `TestRun_Running_DoesNotFireCleanup`
    - `TestRun_CleanupDoneGuardPreventsDoubleFire`
    - `TestRun_CleanupError_DoesNotChangePhaseAndSetsCleanupDone`
  - Local helper `makeStateWithCleanup(runID, triggeredAt, cleanupDone)` added for the guard test. Existing `makeState` helper and all prior callsites are untouched (minimal-diff discipline).

## Decisions Made

All decisions were front-loaded in the plan's `<context>` block and executed verbatim. Notable:

- **Separate `CleanupDone` field vs reusing `FinalStatus`.** `FinalStatus` is set inside the terminal switch BEFORE the cleanup hook runs on the same Run() call — so `if FinalStatus != ""` would skip cleanup on its very first opportunity. A distinct `CleanupDone` flag fires cleanup on the first terminal observation and suppresses it only on subsequent reconciliation replays. Plan-check INFO-2 called out this subtlety; the field rename + Godoc make the semantics unambiguous.
- **Hook inside `Run()`, not a separate method.** `Run()` is where Argo Rollouts observes state transitions (D-04: repeatedly until terminal). Adding a separate `cleanup()` method invoked from `Run()` would have doubled the call-site surface without reducing complexity.
- **Aborted excluded from the hook.** Aborted arriving in `Run()` is a post-cancellation replay: `Terminate`/`Abort` RPCs already fired `StopRun` via `stopActiveRun` (D-07). On the k6-operator backend StopRun and Cleanup share the `deleteCR` helper (11-01), so firing Cleanup here would be a redundant NotFound-as-success roundtrip. Cleaner to keep the explicit exclusion than to rely on NotFound-dedup.
- **TimedOut excluded structurally.** The timeout branch (`step.go:134-151`) already calls `StopRun` and returns early with `PhaseFailed`. It sets `FinalStatus="TimedOut"` — intentionally NOT in the `isSuccessPathTerminal` set — and never reaches the new hook. INFO-1 (plan-check) flagged this as an optional defensive test; we chose NOT to add it because the guarantee is structural (early return) and a 3-line test would duplicate the invariant without increasing coverage.
- **`state.CleanupDone = true` regardless of Cleanup result.** Per GC-03, cleanup errors don't fail the step. Per REQUIREMENTS.md Out of Scope `Retry loop on cleanup failure`, we do not retry. Setting the flag after a failed Cleanup means the next replay Run() skips cleanly. Correct semantics: "we tried, it failed, move on."
- **Inline `cleanupCancel()`, not `defer` in loop.** No loop here, but matches the project-wide idiom used for TriggerRun/GetRunResult/StopRun in the same `Run()` body.

## Deviations from Plan

None — plan executed exactly as written. The plan's `<behavior>` and `<action>` blocks specified the two edits with exact code snippets; the executor applied them verbatim. Both INFO items in `11-CHECK.md` relevant to 11-02 (INFO-1 optional TimedOut test, INFO-7 backward-compat note) were considered; INFO-1 was declined with rationale (structural guarantee already in place), INFO-7 was incorporated as a `key-decision` entry above and called out in the test commentary.

## Issues Encountered

None. The TDD cycle ran cleanly:

1. **RED** — the 7 new tests added `stepState{CleanupDone: true}` in the `makeStateWithCleanup` helper, which forced a compile error on the missing field (the correct RED signal for a struct-extension plan). Confirmed with `go test` emitting `unknown field CleanupDone in struct literal of type stepState` x8 (1 helper + 7 assertion sites).
2. **GREEN** — two edits in step.go (field add + hook insert) compiled on first attempt. All 7 new tests PASS + all 4 prior `TestRun_Terminal_*` tests remain PASS.
3. **Gate** — `go build ./...` + `go build -tags=e2e ./e2e/...` + `go test ./... -count=1` (256 PASS) + `make lint` (0 issues) all green on first run.

No debugging detours. No scope adjustments.

## User Setup Required

None. RBAC is unchanged from Phase 10/11-01; no new apiserver permissions; no new secrets; no CRD changes.

## Verification Gates (all green)

| Gate | Command | Result |
|------|---------|--------|
| Build | `go build ./...` | PASS (exit 0) |
| E2E build | `go build -tags=e2e ./e2e/...` | PASS (exit 0) |
| Unit tests | `go test ./... -count=1` | PASS (256 tests, 5/5 packages OK -- baseline 249 + 7 new) |
| Lint | `make lint` | PASS (0 issues, stdout-protocol check green) |

### Structural invariants (all PASS)

```
CleanupDone bool in step.go             -- 1 ✓  (struct field)
cleanupDone,omitempty in step.go        -- 1 ✓  (json tag)
isSuccessPathTerminal in step.go        -- 2 ✓  (declaration + usage on the one insertion)
k.provider.Cleanup in step.go           -- 1 ✓  (single fire point)
state.CleanupDone in step.go            -- 4 ✓  (guard check + assignment + 2 ancillary; >= 2)
k.provider.StopRun in step.go           -- 2 ✓  (timeout branch + stopActiveRun, unchanged)
func stopActiveRun in step.go           -- 1 ✓  (helper unchanged)
TestRun_TerminalPassed_FiresCleanupOnce -- 1 ✓
TestRun_TerminalAborted_DoesNotFireCleanup -- 1 ✓
TestRun_CleanupDoneGuardPreventsDoubleFire -- 1 ✓
TestRun_CleanupError_DoesNotChangePhaseAndSetsCleanupDone -- 1 ✓
TestRun_Terminal_Passed (old, unchanged) -- 1 ✓
func TestRun_Terminal_Aborted (old)      -- 1 ✓
```

## Phase 11 Readiness

Phase 11 is now **2/2 plans complete**:

- [x] **11-01** — metric plugin `GarbageCollect` walks measurements + `Provider.Cleanup` interface landed (249 tests, 26 min)
- [x] **11-02** — step plugin `Run()` terminal-state cleanup hook (256 tests, 3 min)

Phase 11 success criteria (ROADMAP):
1. [x] Terminal AR → `K6MetricProvider.GarbageCollect` deletes per-measurement TestRuns (11-01)
2. [x] Terminal step → `K6StepPlugin.Run` fires Cleanup once per transition (11-02)
3. [x] Cleanup errors logged at Warn, never alter RpcError/phase (11-01 + 11-02)
4. [x] Unit tests assert all of the above (18 new tests across both plans)

## Next Phase Readiness

- **Phase 12 unblocked.** With both success-path leaks closed, Phase 12's combined-canary e2e (`TestK6OperatorCombinedCanaryARDeletion`) can cleanly distinguish owner-ref cascading GC (kube-apiserver) from plugin-triggered Cleanup (application layer) without confounding leaks.
- **Future Job provider unblocked.** The `Provider.Cleanup` interface method (added by 11-01) and the step plugin's Router-routed call path (exercised by 11-02) are provider-agnostic; a Kubernetes Job provider can plug in with no step-plugin changes.
- **No blockers. No concerns.**

## Self-Check: PASSED

All 2 files modified verified present on disk. Both task commits verified present in `git log`.

```
internal/step/step.go       -- 21 lines added (field + hook)
internal/step/step_test.go  -- 238 lines added (7 tests + helper)

1ba2595 test(11-02): add failing terminal-state cleanup hook tests for step plugin
f4bd93d feat(11-02): fire provider.Cleanup on step plugin success-path terminal state
```

---
*Phase: 11-success-path-testrun-cleanup*
*Completed: 2026-04-20*
