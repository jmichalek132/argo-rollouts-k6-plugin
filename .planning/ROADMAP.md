# Roadmap: argo-rollouts-k6-plugin

## Overview

Two Argo Rollouts plugin binaries (metric + step) that gate canary and blue-green deployments on Grafana Cloud k6 load test results OR in-cluster k6-operator runs. Extensible provider interface designed for future execution backends (direct binary, Kubernetes Job).

## Milestones

- ✅ **v1.0 MVP** — Phases 1-4 (shipped 2026-04-14)
- ✅ **v0.2.0 Hardening** — Phases 5-6 (shipped 2026-04-15)
- ✅ **v0.3.0 In-Cluster Execution** — Phases 7-10 + decimals 08.1/08.2/08.3 (shipped 2026-04-20)
- 🚧 **v0.4.0 Cleanup** — Phases 11-13 (in progress)

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-4) — SHIPPED 2026-04-14</summary>

- [x] Phase 1: Foundation & Provider (2/2 plans) — completed 2026-04-09
- [x] Phase 2: Metric Plugin (2/2 plans) — completed 2026-04-10
- [x] Phase 3: Step Plugin (2/2 plans) — completed 2026-04-10
- [x] Phase 4: Release & Examples (3/3 plans) — completed 2026-04-10

Full details: [milestones/v1.0-ROADMAP.md](milestones/v1.0-ROADMAP.md)

</details>

<details>
<summary>✅ v0.2.0 Hardening (Phases 5-6) — SHIPPED 2026-04-15</summary>

- [x] Phase 5: CI Pipeline Fix (1/1 plan) — completed 2026-04-15
- [x] Phase 6: Automated Dependency Management (1/1 plan) — completed 2026-04-15

Full details: [milestones/v0.2.0-ROADMAP.md](milestones/v0.2.0-ROADMAP.md)

</details>

<details>
<summary>✅ v0.3.0 In-Cluster Execution (Phases 7-10 + 08.1/08.2/08.3) — SHIPPED 2026-04-20</summary>

- [x] Phase 7: Foundation & Kubernetes Client (2/2 plans) — completed 2026-04-16
- [x] Phase 8: k6-operator Provider (2/2 plans) — completed 2026-04-16
- [x] Phase 08.1: Wire AnalysisRun/Rollout metadata through plugin layers (3/3 plans, inserted) — completed 2026-04-17
- [x] Phase 08.2: Default cfg.Parallelism=1 when unset (1/1 plan, inserted) — completed 2026-04-19
- [x] Phase 08.3: Remove spec.Cleanup=post (1/1 plan, inserted retroactively) — completed 2026-04-20
- [x] Phase 9: Metric Integration (2/2 plans) — completed 2026-04-16
- [x] Phase 10: Documentation & E2E (2/2 plans) — completed 2026-04-16

Full details: [milestones/v0.3.0-ROADMAP.md](milestones/v0.3.0-ROADMAP.md)

</details>

### 🚧 v0.4.0 Cleanup (In Progress)

- [x] **Phase 11: Success-path TestRun cleanup** — metric plugin `GarbageCollect` + symmetric step plugin terminal-state hook delete k6-operator TestRun CRs created during successful analysis/step runs (GC-01, GC-02, GC-03, GC-04) — **completed 2026-04-20**
- [ ] **Phase 12: Combined canary e2e + owner-ref GC cascade** — new e2e `TestK6OperatorCombinedCanaryARDeletion` proves D-07 owner-ref precedence under real kube-apiserver garbage collection (TEST-02)
- [ ] **Phase 13: Opportunistic polish** — `buildTestRun` Godoc consolidation, `dumpK6OperatorDiagnostics` helper extraction, 3 INFO items from 08.1-REVIEW.md (POLISH-01, POLISH-02, POLISH-03)

## Phase Details

### Phase 11: Success-path TestRun cleanup

**Goal**: Metric and step plugins delete the k6-operator TestRun CRs they created once analysis/step reaches a terminal state, closing the success-path leak that 08.3 deferred when `spec.Cleanup=post` was removed.
**Depends on**: v0.3.0 complete (Phase 10 + 08.3)
**Requirements**: GC-01, GC-02, GC-03, GC-04
**Success Criteria** (what must be TRUE):
  1. On terminal AnalysisRun, `K6MetricProvider.GarbageCollect` deletes every k6-operator TestRun CR the metric plugin created for that AR (recovered from `measurement.Metadata["runId"]` + AR namespace); no-op when `provider: grafana-cloud`.
  2. On terminal step state (`Passed`/`Failed`/`Errored`), `K6StepPlugin.Run` fires cleanup for the TestRun CR the step created — exactly once per terminal transition, never on `Running`, never on `Aborted` (still handled by `StopRun` via Terminate/Abort).
  3. Cleanup errors (k8s API unavailable, CR already gone, RBAC denied) are logged at `slog.Warn` and do NOT alter `RpcError`, measurement phase, or step phase returned to the controller.
  4. Unit tests assert: GarbageCollect deletes the right CR for k6-operator, is a no-op for grafana-cloud, step terminal-state hook fires once per transition, and cleanup errors never surface.
**Plans**: 2/2 complete
  - [x] 11-01-PLAN.md -- Metric plugin GarbageCollect: wire `provider.Cleanup(ctx, cfg, runID)` through K6OperatorProvider (reuse `StopRun` delete path with idempotent NotFound handling), handle grafana-cloud no-op, unit tests for GC-01/GC-03/GC-04(a,b,d) — **completed 2026-04-20** ([11-01-SUMMARY.md](./phases/11-success-path-testrun-cleanup/11-01-SUMMARY.md))
  - [x] 11-02-PLAN.md -- Step plugin terminal-state cleanup hook: post-terminal cleanup in `Run` for `Passed`/`Failed`/`Errored` (new `stepState.CleanupDone` guard fires exactly once per terminal transition), unit tests for GC-02/GC-03/GC-04(c,d) — **completed 2026-04-20** ([11-02-SUMMARY.md](./phases/11-success-path-testrun-cleanup/11-02-SUMMARY.md))

### Phase 12: Combined canary e2e + owner-ref GC cascade

**Goal**: Prove D-07 owner-reference precedence (AR > Rollout > none) under real Kubernetes garbage collection — not just unit-level OwnerReference struct assertion.
**Depends on**: Phase 11 (metric cleanup must not race with kube-apiserver GC during the test; without Phase 11 the assertion "metric-created TestRun GC'd via AR owner ref" is confounded by success-path leaks)
**Requirements**: TEST-02
**Success Criteria** (what must be TRUE):
  1. New e2e test `TestK6OperatorCombinedCanaryARDeletion` in `e2e/k6_operator_test.go` deploys a Rollout that runs a step plugin AND references an AnalysisTemplate (metric plugin) simultaneously against the same kind cluster.
  2. While both TestRun CRs are in `Running` stage, the test issues `kubectl delete analysisrun <name>` and the AR-owned TestRun disappears from `kubectl get testruns` within the reconcile window (kube-apiserver cascading GC via AnalysisRun OwnerReference).
  3. The Rollout-owned step TestRun survives the AR deletion and is still observable with `managed-by=argo-rollouts-k6-plugin` label.
  4. Test runs green in CI (`make test-e2e` green, 7/7 tests PASS including the new case) with diagnostic dumps on failure (builds on `dumpK6OperatorDiagnostics`).
**Plans**: TBD
  - [ ] 12-01-PLAN.md -- Combined-canary e2e test + testdata (Rollout with step+AnalysisTemplate, ConfigMap script, mock target service); kubectl jsonpath regression guards on both TestRuns' presence/absence post-AR-deletion; diagnostic dumps on failure

### Phase 13: Opportunistic polish

**Goal**: Pay down three opportunistic-polish items: consolidate accumulated `buildTestRun` Godoc, extract the repeated kubectl dump block into a declarative helper, and resolve 3 INFO items from 08.1-REVIEW.md.
**Depends on**: Nothing (can run in parallel with Phase 11 — targets Godoc/comments/warnings/test assertions, not behavior shared with Phase 11's cleanup path)
**Requirements**: POLISH-01, POLISH-02, POLISH-03
**Success Criteria** (what must be TRUE):
  1. `internal/provider/operator/testrun.go` `buildTestRun` has a single coherent Godoc block; detailed rationale (08.2 parallelism + 08.3 cleanup) moves to a `// Design notes` block below the signature or into a sibling `doc.go`.
  2. `e2e/k6_operator_test.go` `dumpK6OperatorDiagnostics` is driven by a declarative list of `{resource, namespace, format}` tuples with a single append point — repeated `exec.Command("kubectl", ...).Output()` blocks collapsed.
  3. IN-01 (`metric.go:246-249`) warning distinguishes "no refs" vs "refs present without controller Rollout" and includes `ownerRefCount`.
  4. IN-02 (`step.go:261-274`) `populateFromRollout` warns + skips `RolloutUID` when `rollout.Name == ""`.
  5. IN-03 (`operator_test.go:347-379`) test locks `Controller` and `BlockOwnerDeletion` nil state on the emitted Rollout ref (and parallel assertions on AR-ref and AR-wins-over-Rollout tests).
**Plans**: TBD
  - [ ] 13-01-PLAN.md -- Consolidate buildTestRun Godoc, extract declarative `dumpK6OperatorDiagnostics` helper, resolve IN-01/IN-02/IN-03 (all small edits in 3 files: `testrun.go`, `e2e/k6_operator_test.go`, `metric.go`/`step.go`/`operator_test.go`)

## Next Milestone

After v0.4.0 Cleanup ships, likely candidates for v0.5.0: Kubernetes Job provider (`JOB-01`/`JOB-02`/`JOB-03`), README in-cluster quick-start + `CHANGELOG.md` standup (`DOCS-03`/`DOCS-04`), or step plugin `secretKeyRef` support pending upstream Argo Rollouts.

Run `/gsd-new-milestone` to start planning the next milestone.

## Progress

| Milestone | Phases | Plans | Status | Shipped |
|-----------|--------|-------|--------|---------|
| v1.0 MVP | 1-4 | 9/9 | Complete | 2026-04-14 |
| v0.2.0 Hardening | 5-6 | 2/2 | Complete | 2026-04-15 |
| v0.3.0 In-Cluster Execution | 7-10 + 08.1/08.2/08.3 | 13/13 | Complete | 2026-04-20 |
| v0.4.0 Cleanup | 11-13 | 2/4 | In progress | - |

### v0.4.0 Phase Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 11. Success-path TestRun cleanup | 2/2 | Complete | 2026-04-20 |
| 12. Combined canary e2e + owner-ref GC cascade | 0/1 | Not started | - |
| 13. Opportunistic polish | 0/1 | Not started | - |
