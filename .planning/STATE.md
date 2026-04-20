---
gsd_state_version: 1.0
milestone: v0.4.0
milestone_name: Cleanup
status: executing
stopped_at: Phase 12 complete (1/1 plan) -- combined-canary e2e landed green; D-07 owner-ref precedence proven under real kube cascade GC
last_updated: "2026-04-20T16:56:00Z"
last_activity: 2026-04-20 -- Phase 12 plan 12-01 complete (TestK6OperatorCombinedCanaryARDeletion + 4 helpers + 2 testdata fixtures; 7/7 e2e PASS incl new 43.28s; lint green; zero internal/ changes). Phase 12 now 1/1 plan done. Next: Phase 13 opportunistic polish.
progress:
  total_phases: 3
  completed_phases: 2
  total_plans: 4
  completed_plans: 3
  percent: 75
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-20)

**Core value:** Rollouts automatically pass or roll back based on real load test results -- no manual gates, no guesswork.
**Current focus:** Phase 12 complete. D-07 owner-ref precedence (AR > Rollout) now has both unit-level (Phase 08.1) and e2e-level (12-01) coverage against real kube-apiserver cascading GC. Next: Phase 13 opportunistic polish (POLISH-01/02/03).

## Current Position

Milestone: v0.4.0 Cleanup
Phase: 12 (combined-canary-e2e-owner-ref-gc-cascade) — COMPLETE (1/1 plan)
Plan: All plans done. Next phase: 13 opportunistic polish.
Status: Phase 12 complete. Ready to start Phase 13.
Last activity: 2026-04-20 -- Plan 12-01 complete (commit 9819bf2)

Progress: [████████░░] 75%

## Performance Metrics

**Velocity (from v1.0 + v0.2.0 + v0.3.0 + v0.4.0 so far):**

- Total plans completed: 20
- Average duration: ~4.7 min/plan
- Total execution time: ~80 min

**Recent Trend:**

- Last 5 plans: 1min, 11min, 26min, 3min, 6min (12-01: 1 e2e test + 4 helpers + 2 testdata YAMLs; passed on first execution, no deviations)
- Trend: Stable (12-01 at 6min — dominated by e2e wall-clock; the new test itself was 43s of the 324s suite runtime)

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| 11-01 | 26 min | 3 | 11 |
| 11-02 | 3 min | 2 | 2 |
| 12-01 | 6 min | 1 | 3 |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

- [v0.3.0 roadmap]: Use dynamic client (unstructured) for TestRun CRD -- avoid importing controller-runtime
- [v0.3.0 roadmap]: client-go v0.34.1 already in go.sum as indirect dep, promote to direct
- [v0.3.0 roadmap]: Pass/fail from runner pod exit codes (k6-operator issue #577 workaround)
- [v0.3.0 roadmap]: handleSummary JSON from pod logs for metric extraction
- [Phase 08.2]: PluginConfig field defaults live at the consumer site (builder function), not in parseConfig or the validation layer. Preserves "0 means unset" at the boundary and keeps builders pure (no slog, no cfg mutation).
- [Phase 08.2]: e2e regression guards for builder defaulting read back the emitted CR via kubectl jsonpath rather than inferring from downstream phase. Direct field verification is higher signal than Rollout-Healthy/AnalysisRun-Successful inference.
- [Phase 08.3]: Do NOT set spec.Cleanup on TestRun CRs. k6-operator v1.3.x deletes CRs with Cleanup=post as soon as stage reaches finished/error, which races the plugin's status-read loop. Success-path cleanup belongs in GarbageCollect (metric) and a terminal-state hook (step) -- a future phase.
- [Phase 08.3]: When debugging plugin failures against a live cluster, argo-rollouts controller logs (`kubectl logs -n argo-rollouts deploy/argo-rollouts`) are the authoritative source of plugin stderr -- go-plugin pipes plugin binary stderr into the controller's log stream. Any e2e diagnostic dump must capture those logs or the root cause stays hidden.
- [Phase 11-01]: Add `Cleanup(ctx, cfg, runID) error` method to Provider interface rather than reusing `StopRun`. Semantic split: StopRun means "cancel in-flight run"; Cleanup means "release resources after terminal state". Share the delete code path on k6-operator backend today via deleteCR helper, but keep future Job provider free to differentiate (cancel with grace-period=0, reap with ttlSecondsAfterFinished).
- [Phase 11-01]: GarbageCollect walks `ar.Status.MetricResults` for the matching `metric.Name` rather than label-selector-by-`ar.UID`. runIDs contain a timestamp hash and are not derivable from ar.UID; Measurements carry the authoritative per-measurement runID. Walk-and-dispatch is simpler than list-and-filter, and matches argo-rollouts' own JobProvider.GarbageCollect precedent (which only uses label selector because Jobs carry a deterministic AnalysisRunUIDLabelKey).
- [Phase 11-01]: GC-03 cleanup errors are swallowed at slog.Warn and never surface as RpcError. Mirrors the existing pattern in metric.go Terminate which swallows StopRun errors the same way. Retry loops are explicitly Out of Scope per REQUIREMENTS.md.
- [Phase 11-02]: stepState gains a separate CleanupDone bool field rather than reusing FinalStatus. FinalStatus is set in the SAME Run() call BEFORE the cleanup hook fires, so a FinalStatus!="" check would skip cleanup on the very first terminal observation. A distinct boolean flag fires cleanup on the first terminal observation and suppresses it only on subsequent reconciliation replays.
- [Phase 11-02]: Step plugin terminal-state hook lives inside Run() post-switch, not in a separate method. Run() is Argo Rollouts' state-transition observation point (D-04: invoked repeatedly until terminal); the first terminal observation is the natural trigger. Aborted is excluded (Terminate/Abort RPCs already fired StopRun via D-07); TimedOut is excluded (timeout branch already calls StopRun and returns early).
- [Phase 11-02]: CleanupDone=true set regardless of Cleanup outcome -- best-effort per GC-03. REQUIREMENTS.md explicitly lists "Retry loop on cleanup failure" as Out of Scope. Single attempt, log warning, move on.
- [Phase 12-01]: Use canary.analysis background block with `startingStep: 1`, NOT a dedicated analysis step. Dedicated step would sequence the step plugin and AR -- step plugin's 120s script would fully complete before the AR even starts, breaking the "both TestRuns concurrent" premise the cascade assertion depends on. Background analysis launches the AR at the same canary step index, giving simultaneous CR existence.
- [Phase 12-01]: Wait for stage `started` (k6-operator v1.3.x enum), not `running`. There is no `running` literal in k6-operator's stage enum (initialization, initialized, created, started, stopped, finished, error). An earlier draft's `"running"` guard would have silently timed out. Fatal-on-timeout on the pre-delete stage gate prevents proceeding with an inconclusive cascade assertion.
- [Phase 12-01]: Capture the AR name BEFORE the delete; filter the cascade poll by the captured name. Argo Rollouts' `reconcileBackgroundAnalysisRun` recreates deleted background ARs, spawning a new AR-owned TestRun. Without filtering, the new TestRun would be counted as "old TestRun still present" and the assertion would false-fail. Filtering by captured name scopes the assertion to exactly the cascade under test.
- [Phase 12-01]: Parse `ownerReferences` via Go struct unmarshal (`encoding/json`) rather than kubectl jsonpath. jsonpath cannot conditionally filter `ownerReferences[].kind` cleanly; struct-based parsing is simpler, case-sensitive, and mirrors the getAnalysisRunMetricValue precedent in the same file.

### Pending Todos

None.

### Blockers/Concerns

None.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260420-hal | Bump golangci-lint CI pin v2.1.6 → v2.11.4 (Go 1.25 build) | 2026-04-20 | d6a7945 | [260420-hal-bump-golangci-lint-action-version-in-ci-](./quick/260420-hal-bump-golangci-lint-action-version-in-ci-/) |

### Roadmap Evolution

- Phase 08.1 inserted after Phase 8: Wire AnalysisRun/Rollout metadata through plugin layers (URGENT — exposed by Phase 10 e2e tests; plugin layers discard parent ObjectMeta so `cfg.Namespace`, `cfg.RolloutName`, `cfg.AnalysisRunUID` are never populated, causing k6-operator provider to fall back to `default` namespace and fail)
- Phase 08.2 inserted after Phase 08.1: Default `cfg.Parallelism=1` when unset (URGENT — exposed by re-running e2e after 08.1 fix; `testrun.go:162` passes `cfg.Parallelism` through as `int32(0)` which causes k6-operator to create TestRun with `parallelism: 0` and `paused: true`, so no runner pods ever spawn and the AnalysisRun hangs in Running state forever)
- Phase 08.3 inserted after Phase 08.2 (RETROACTIVE, `/gsd-debug`-discovered): Remove `spec.Cleanup=post` from `buildTestRun` — k6-operator v1.3.x deletes TestRun CRs (cascading to runner pods) on stage=finished/error when Cleanup=post, which race-deletes the CR before the metric plugin's 10s Resume poll can read terminal state and parse handleSummary from pod logs. Phase 08.2 exposed this by letting TestRuns actually finish instead of staying paused.

## Session Continuity

Last session: 2026-04-20T16:56:00Z
Stopped at: Phase 12 complete (1/1 plan) -- combined-canary e2e landed; D-07 owner-ref cascade proven under real kube GC
Resume file: .planning/phases/12-combined-canary-e2e-owner-ref-gc-cascade/12-01-SUMMARY.md
