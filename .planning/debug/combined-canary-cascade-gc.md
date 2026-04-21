---
slug: combined-canary-cascade-gc
status: resolved
trigger: "make test-e2e failure (2026-04-21) — TestK6OperatorCombinedCanaryARDeletion: AR-owned TestRun k6-k6-combined-canary-k6op-e2e-9b597ad4 still present 90s after AR delete; kube cascade GC did not fire"
created: 2026-04-21T09:20:00Z
updated: 2026-04-21T10:30:00Z
goal: find_and_fix
---

# Debug Session: combined-canary-cascade-gc

## Symptoms

- **Expected behavior**: Deleting the AR-owned background AnalysisRun should trigger kube-apiserver cascading garbage collection of its owned TestRun CR. Rollout-owned TestRun should survive. This proves D-07 owner-ref precedence (AR > Rollout).
- **Actual behavior**: AR-owned TestRun `k6-k6-combined-canary-k6op-e2e-9b597ad4` still present 90s after the AR `k6-combined-canary-k6op-e2e-6d4998b67d-2` was deleted. Cascade GC appeared not to fire within the poll window.
- **Error message**:
  ```
  k6_operator_test.go:455: TestRun(s) owned by deleted AnalysisRun "k6-combined-canary-k6op-e2e-6d4998b67d-2" still present 90s after delete; kube cascade GC did not fire: [k6-k6-combined-canary-k6op-e2e-9b597ad4]
  --- FAIL: TestK6OperatorCombinedCanaryARDeletion (131.37s)
  ```
- **Timeline**: Test was added in Phase 12-01 (commit landed 2026-04-20) and passed on implementation. First known failure: 2026-04-21 09:20:00Z on a clean `make test-e2e` run against kind.
- **Reproduction**: `make test-e2e` — the `TestK6OperatorCombinedCanaryARDeletion/.../AR-owned_TestRun_GC'd_on_AR_delete;_Rollout-owned_TestRun_survives` sub-case.

## Relevant k6-operator log signals from failing run

Both TestRuns stuck at `stage = started` with `0/1 jobs complete, 0 failed` for the entire polling window. Initially attributed to CPU / image-pull contention on the kind node; after rounds 1 and 2 of fixes this attribution was revealed as incomplete — AR recreation by argo-rollouts is the load-bearing factor, not GC-controller latency.

## Current Focus

(resolved — see Resolution below)

## Evidence

- timestamp: 2026-04-21T09:35:00Z
  source: internal/provider/operator/testrun.go:79-97 (parentOwnerRef)
  finding: parentOwnerRef emits an OwnerReference with `APIVersion`, `Kind`, `Name`, and `UID` populated but leaves `Controller` and `BlockOwnerDeletion` as nil pointers. This is intentional and documented by Phase 13 IN-03 (commit e4b5841).

- timestamp: 2026-04-21T09:38:00Z
  source: internal/provider/operator/operator_test.go:320-323, 388-391, 428-431
  finding: Three unit-test assertions lock `Controller == nil` and `BlockOwnerDeletion == nil` on the emitted owner ref (AR branch, Rollout branch, and AR-wins-over-Rollout branch). These passed in the same `make test-e2e` run (unit tests run under `make test`). The emitted OwnerReferences contract is unchanged from Phase 12-01.

- timestamp: 2026-04-21T09:42:00Z
  source: Kubernetes GC semantics (apimachinery/pkg/apis/meta/v1 OwnerReference docs; k8s.io/docs garbage-collection)
  finding: `Controller=true` gates "controllee" adoption semantics (conflict resolution for a single controlling owner) and is NOT required for cascade-delete. Cascade GC walks `ownerReferences[].uid` and deletes dependents whose owner UID no longer exists. `BlockOwnerDeletion=true` is only required to hold the owner's foreground-delete open until the dependent is gone; without it, foreground cascade still works, it just doesn't block the owner. Neither flag being nil prevents cascade GC from firing.

- timestamp: 2026-04-21T09:47:00Z
  source: /Users/jorge/go/pkg/mod/github.com/grafana/k6-operator@v1.3.2/internal/controller/testrun_controller.go + grep "[Ff]inalizer"
  finding: The k6-operator TestRun controller has RBAC for `testruns/finalizers` (kubebuilder scaffolding) but **does not add any finalizer in code**. Only the PrivateLoadZone controller adds `privateloadzones.k6.io/finalizer`. There is no finalizer on TestRun CRs that could block cascade GC from completing.

- timestamp: 2026-04-21T09:50:00Z
  source: /Users/jorge/go/pkg/mod/github.com/argoproj/argo-rollouts@v1.9.0/analysis/ (grep "[Ff]inalizer" found no matches)
  finding: The argo-rollouts AnalysisRun controller does not add any finalizer to AnalysisRun objects. The AR delete completes immediately at the storage layer once the apiserver processes it.

- timestamp: 2026-04-21T09:53:00Z
  source: /Users/jorge/go/pkg/mod/github.com/grafana/k6-operator@v1.3.2/internal/controller/k6_start.go:128-129
  finding: k6-operator sets `status.stage="started"` **immediately after creating the starter Job**, BEFORE confirming runner pods are Ready. The starter Job then hits the runner pods' HTTP endpoints to actually kick off the test. On a resource-starved kind cluster (runner pods stuck on image pull or CPU throttling), the stage=started gate passes long before the test is actually running. This is a tangential observation — cluster load is real, but see evidence below: it is NOT the dominant factor.

- timestamp: 2026-04-21T09:58:00Z
  source: e2e/k6_operator_test.go:434-435 (the failing delete call)
  finding: `kubectl delete analysisrun arName -n NS --ignore-not-found` uses the apiserver's default propagation policy, which for CRs is **Background**. Under Background propagation, the delete call returns as soon as the apiserver tombstones the owner; the out-of-band GC controller (kube-controller-manager) then enumerates dependents asynchronously and issues deletes.

- timestamp: 2026-04-21T10:00:00Z
  source: cross-reference — Phase 12-01 SUMMARY + Phase 13 unit tests passing in the same run
  finding: The TestRun emission contract is identical to the green run on 2026-04-20. Nothing about owner-ref composition changed between the passing and failing runs. The failure is therefore environmental or test-harness-level, not a regression in plugin code.

- timestamp: 2026-04-21T10:10:00Z
  source: post-round-1 local verification (--cascade=foreground fix)
  finding: Added `--cascade=foreground` to the AR delete and declared the session resolved based on compile/vet/lint passing. This was WRONG — no e2e verification was performed.

- timestamp: 2026-04-21T10:15:00Z
  source: orchestrator re-run of `GOPATH=... DOCKER_HOST=... go test -v -tags=e2e -count=1 -timeout=15m -run TestK6OperatorCombinedCanaryARDeletion ./e2e/...` with round-1 fix applied
  finding: SAME FAILURE. Same 90s timeout, same "still present" message. The `--cascade=foreground` fix had no effect because the AR->TestRun OwnerReference carries `BlockOwnerDeletion=nil`. Foreground propagation only synchronously blocks on dependents whose `blockOwnerDeletion=true`; with none present the `foregroundDeletion` finalizer is removed immediately and dependents fall through to the async kube-controller-manager GC loop — identical to `--cascade=background`. Evidence: the Kubernetes GC spec itself ("Once the controller deletes all the dependents whose ownerReference.blockOwnerDeletion=true, it deletes the owner object. If no such dependents, the owner is deleted immediately and dependents are cleaned up by background GC.").

- timestamp: 2026-04-21T10:20:00Z
  source: post-round-2 local verification (--cascade flag reverted + cascade deadline extended 90s -> 5m; poll interval 3s -> 5s)
  finding: SAME FAILURE. Full 5-minute deadline expired with the TestRun still reporting AR ownership. This rules out "async GC delay" as the sole cause — 5 minutes is far beyond what any GC-controller latency should consume on kind, even under load. Something is actively replenishing AR-owned TestRuns.

- timestamp: 2026-04-21T10:25:00Z
  source: /Users/jorge/go/pkg/mod/github.com/argoproj/argo-rollouts@v1.9.0/rollout/analysis.go (reconcileBackgroundAnalysisRun @ line 347; needsNewAnalysisRun @ line 170) + utils/analysis/helpers.go (CreateWithCollisionCounter @ line 257)
  finding: **ROOT CAUSE.** After the test deletes the background AR, argo-rollouts' rollout reconciler observes `c.currentArs.CanaryBackground == nil`. `needsNewAnalysisRun(nil, rollout)` unconditionally returns `true` (line 171-173: `if currentAr == nil { return true }`). The reconciler then calls `createAnalysisRun(...)` which invokes `CreateWithCollisionCounter`. The delete has already propagated, so there is NO `IsAlreadyExists` collision — the new AR is created under the **SAME name** `k6-combined-canary-k6op-e2e-6d4998b67d-2` but with a **NEW UID**. The metric plugin's `Run(ar)` on the new AR then calls `provider.TriggerRun` which emits a new TestRun with owner-ref `AnalysisRun/<same-name>/<new-UID>`. The test's cascade-poll helper `getTestRunsOwnedBy` filters by `Kind` and `Name` only (not UID), so the new TestRun matches the stale `arName` filter and is falsely counted as "still present".

- timestamp: 2026-04-21T10:28:00Z
  source: log inspection — both TestRuns (ca7e2144 and 48c55c6b) observed in k6-operator controller output across the full failure window; `ca7e2144` receives "Request deleted. Nothing to reconcile." at test teardown while `48c55c6b` persists through 5-minute cascade poll, consistent with `ca7e2144` being the original AR's TestRun (cascade-deleted on schedule) and `48c55c6b` being the recreated AR's TestRun (never AR-deleted — it has a valid live owner).
  finding: Cascade GC is working correctly on the original AR's TestRun. The test assertion is mis-scoped: it asks "are any TestRuns owned by an AR named X?" when it should ask "are any TestRuns owned by AR instance with UID=U?".

- timestamp: 2026-04-21T10:33:00Z
  source: post-round-3 local verification — UID-based filter fix
  finding: `GOPATH=... DOCKER_HOST=... go test -v -tags=e2e -count=1 -timeout=15m -run TestK6OperatorCombinedCanaryARDeletion ./e2e/...` — **PASS (33.89s total; cascade sub-case 11.75s)**. The UID-filtered cascade poll observes the original AR's TestRun disappearing promptly once the AR UID no longer exists; the recreated AR's TestRun (different UID) is correctly ignored. Hygiene: `make lint` reports `0 issues`, `go test ./internal/provider/operator/...` PASS, Rollout-owned TestRun survival assertion still holds (D-07 precedence proven).

## Eliminated

1. **Hypothesis (initial): nil `Controller`/`BlockOwnerDeletion` on AR owner-ref blocks cascade GC.**
   Eliminated because (a) cascade GC uses UID-linkage only; `Controller=true` is for adoption conflict resolution, not cascade triggering; (b) `BlockOwnerDeletion` only matters for foreground propagation's blocking semantics; (c) this same owner-ref shape was green on 2026-04-20 and is locked by three IN-03 unit tests. Phase 13's nil lock is correct per Kubernetes GC semantics.

2. **Hypothesis: k6-operator TestRun finalizer blocks deletion.**
   Eliminated. The TestRun controller does not add any finalizer (verified via grep of k6-operator@v1.3.2 sources). Only PrivateLoadZone has a finalizer.

3. **Hypothesis: AnalysisRun finalizer delays the AR delete itself.**
   Eliminated. argo-rollouts@v1.9.0 does not add any finalizer to AnalysisRun (verified via grep of analysis/ subtree). The AR is removed from storage immediately.

4. **Hypothesis: Plugin regression in `parentOwnerRef` emitting a malformed ref.**
   Eliminated. `parentOwnerRef` unit tests pass in the same `make test-e2e` run. The emitted ref shape is identical to the passing Phase 12-01 baseline.

5. **(Round-1 hypothesis) `--cascade=foreground` will force synchronous cascade and avoid GC-controller latency.**
   Eliminated by empirical re-run: same failure. Mechanism: with `BlockOwnerDeletion=nil` on all dependents, foreground propagation's `foregroundDeletion` finalizer is removed immediately and the path degrades to background GC. The fix was a no-op against this owner-ref shape.

6. **(Round-2 hypothesis) The sole problem is async GC-controller latency on loaded kind; extending the deadline from 90s to 5m will cover it.**
   Eliminated by empirical re-run: same failure at the 5-minute deadline. Latency alone cannot explain a TestRun persisting for 300+ seconds on a functioning kind cluster. Argo-rollouts was actively producing new TestRuns under the same AR name (different UIDs) via its background-AR recreation path.

## Resolution

**Root cause:** E2E-test scoping bug. `TestK6OperatorCombinedCanaryARDeletion` deletes a background AnalysisRun and polls for "TestRuns owned by an AR named X". After the delete, argo-rollouts' rollout reconciler (`reconcileBackgroundAnalysisRun` -> `needsNewAnalysisRun(nil)==true` -> `CreateWithCollisionCounter`) **recreates the background AR under the same name but a new UID**. The metric plugin's `Run` on the recreated AR emits a new TestRun owned by the new AR. The cascade-poll helper matched only on `Kind` + `Name`, so the recreated AR's TestRun was conflated with the deleted AR's TestRun, producing a spurious "still present" assertion. The plugin's D-07 owner-ref precedence and the Kubernetes cascade-GC path are both working correctly; the test was asking the wrong question.

Cluster load was a real environmental factor in the initial 90s-deadline failure window (k6 runner pods progress slowly on oversubscribed kind), but it was NOT the dominant cause — the 5-minute round-2 deadline was far in excess of any reasonable GC-controller latency and the test still failed.

**Fix applied:** UID-pinned cascade filter. `TestK6OperatorCombinedCanaryARDeletion` now captures the AR's `metadata.uid` BEFORE issuing `kubectl delete analysisrun`, and the cascade poll calls `getTestRunsOwnedByUID(..., arName, arUID)` which matches only TestRuns whose ownerRef has both the captured name AND the captured UID. A recreated AR has a new UID, so its TestRun is correctly excluded. Deadline kept at 5 minutes as a generous safety net for real cascade latency; the actual cascade window on a healthy kind host is ~1 second.

**Files changed:**
- `e2e/k6_operator_test.go`:
  - New helper `getTestRunsOwnedByUID(cfg, ns, ownerKind, ownerName, ownerUID)` — variant of `getTestRunsOwnedBy` that adds a UID equality check. The original helper is preserved as a thin wrapper (`ownerUID=""`) to avoid churning unrelated call sites.
  - New helper `getAnalysisRunUID(cfg, name, ns)` — reads `metadata.uid` of a named AR via `kubectl get -o jsonpath`.
  - Test step (3) now captures `arUID` alongside `arName`; fails the test if UID is empty.
  - Test step (4) now polls via `getTestRunsOwnedByUID(..., arName, arUID)` with a 5-minute deadline and a 5s interval; reverted the round-1 `--cascade=foreground` flag (provably ineffective against the nil-BlockOwnerDeletion owner-ref shape).
  - Comment block on step (3) rewritten to document the AR-recreation mechanism and point at this debug file; mentions both round-1 (foreground) and round-2 (deadline-only) false-positive fixes as cautionary notes.
  - Test budget comment updated from "~5 min" to "~10 min" to reflect the conservative worst-case path.

**Diff summary (e2e/k6_operator_test.go):**
```
+ getAnalysisRunUID helper (kubectl jsonpath={.metadata.uid})
+ getTestRunsOwnedByUID(..., ownerUID string) -- adds UID predicate
  getTestRunsOwnedBy now wraps getTestRunsOwnedByUID with ownerUID=""
  test step (3): arUID captured + fatal-on-empty
  test step (4): getTestRunsOwnedByUID(..., arName, arUID); 5m deadline; 5s interval
  --cascade=foreground flag removed (round-1 false-positive fix)
```

**Verification (the exact command the orchestrator specified):**
```
GOPATH="$HOME/go" DOCKER_HOST="unix://$HOME/.colima/default/docker.sock" \
  go test -v -tags=e2e -count=1 -timeout=15m \
  -run TestK6OperatorCombinedCanaryARDeletion ./e2e/...
```
Result: **PASS** (33.89s total; cascade sub-case 11.75s).
- `--- PASS: TestK6OperatorCombinedCanaryARDeletion (33.89s)`
- `--- PASS: .../AR-owned_TestRun_GC'd_on_AR_delete;_Rollout-owned_TestRun_survives (11.75s)`
- `make lint` — `0 issues.`
- `go test ./internal/provider/operator/...` — PASS
- `go build -tags e2e ./e2e/...` / `go vet -tags e2e ./e2e/...` — OK

**Why this is safe:**
- No plugin code changes. The owner-ref emission contract (`Controller=nil`, `BlockOwnerDeletion=nil`, UID populated, Phase 13 IN-03) is unchanged and its three unit tests still lock the shape.
- UID filtering is strictly MORE precise than name filtering; it cannot spuriously mark a cascade as complete — only more precisely identify which TestRuns belong to the deleted AR instance.
- The Rollout-owned TestRun survival assertion (step 5) is unchanged: it uses the Rollout's name as filter, and the Rollout is not recreated by the test, so name-only filtering is correct for that branch.
- The D-07 precedence evidence (AR > Rollout) is preserved end to end: the deleted AR's TestRun disappears within ~1 second of the delete via real kube-apiserver cascade GC; the Rollout-owned TestRun (a different owner chain) is untouched.

**Postmortem lessons (for future debug sessions):**
1. "Compile + vet + lint" is NOT verification for an e2e failure. The round-1 fix was declared resolved without running the e2e test — it rebound identically on first real execution. Fix this session's workflow to mandate the originating test command as the pass criterion.
2. When cascade GC "does not fire" for minutes, suspect either a finalizer (eliminated here) OR external replenishment of dependents (the real cause here). Kubernetes async GC latency measured in minutes is effectively always a symptom of something else.
3. Owner-ref comparisons in e2e tests should pin UID, not just name, any time the owner can be recreated under the same name. Argo-rollouts' background-AR reconciliation is one such recreator; there are likely others (step ARs on retry; pre/post-promotion ARs on re-promotion).

**Specialist hint:** go
