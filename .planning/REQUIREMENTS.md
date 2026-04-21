# Requirements: argo-rollouts-k6-plugin

**Defined:** 2026-04-20
**Milestone:** v0.4.0 Cleanup
**Core Value:** Rollouts automatically pass or roll back based on real load test results — no manual gates, no guesswork.

## Milestone Goal

Close out tech debt that v0.3.0 took on — success-path TestRun cleanup, extended e2e coverage for owner-ref GC cascade, and opportunistic polish on code-review findings. No new user-visible features; make v0.3.0 features production-clean.

## v0.4.0 Requirements

Three requirement groups, each mapped to roadmap phases.

### Cleanup (Success-path Garbage Collection)

- [x] **GC-01**: Metric plugin implements `GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError` (the real argo-rollouts v1.9.0 signature; `metricproviders/plugin/rpc/rpc.go:121`) — when called by Argo Rollouts on measurement-retention overflow (`analysis/analysis.go:775-800`, `len(measurements) > limit`), walks `ar.Status.MetricResults` for the entry matching `metric.Name`, iterates its `Measurements`, and deletes every k6-operator TestRun CR the plugin created for that AnalysisRun (CR namespace/resource/name are recoverable from `measurement.Metadata["runId"]`). Cascade deletes runner pods via Kubernetes owner references. No-op when `provider: grafana-cloud` (the Cloud API retains runs server-side — nothing to clean up client-side). — **Complete 2026-04-20 (11-01)**
- [x] **GC-02**: Step plugin invokes the same cleanup path on terminal Run state (`Passed`, `Failed`, `Errored`) — not on `Running` (still in progress) and not on `Aborted` (already handled by `StopRun` via `Terminate`/`Abort`). Success-path cleanup symmetry with metric plugin. — **Complete 2026-04-20 (11-02)**
- [x] **GC-03**: Cleanup errors are logged at `slog.Warn` level but do NOT cause the analysis or step to fail — resource cleanup is best-effort, plugin output must stay unchanged. — **Complete 2026-04-20 (11-01 metric-side + 11-02 step-side)**
- [x] **GC-04**: Unit tests cover: (a) `GarbageCollect` deletes the correct TestRun CR ✅ (11-01); (b) `GarbageCollect` is a no-op for `grafana-cloud` provider ✅ (11-01); (c) step plugin terminal-state hook fires once per terminal transition, not on every poll ✅ (11-02); (d) cleanup errors don't surface to controller ✅ (11-01 metric-side + 11-02 step-side). — **Complete 2026-04-20 (11-01 + 11-02)**

### Testing (E2E coverage for owner-ref GC)

- [x] **TEST-02**: New e2e test `TestK6OperatorCombinedCanaryARDeletion` in `e2e/k6_operator_test.go` — creates a Rollout that runs BOTH a step plugin AND a metric plugin (via AnalysisTemplate reference) simultaneously. During the run, deletes the AnalysisRun (via `kubectl delete analysisrun ...`). Asserts:
  - Metric-plugin-created TestRun CR is GC'd by kube-apiserver (AR owner ref) — `kubectl get testruns` shows it gone within reconcile window
  - Step-plugin-created TestRun CR survives (Rollout owner ref)
  - Proves D-07 precedence (AR > Rollout) under real Kubernetes cascading GC, not just unit-level OwnerReference construction — **Complete 2026-04-20 (12-01)**

### Polish (Opportunistic cleanup)

- [x] **POLISH-01**: `buildTestRun` Godoc consolidation — merge the three accumulated doc blocks (original + 08.2 parallelism + 08.3 cleanup rationale) into a single coherent block; move detailed rationale into a separate `// Design notes` section below the signature or into a sibling `doc.go`. **Complete 2026-04-20 (13-01) — colocated // Design notes block chosen over sibling doc.go.**
- [x] **POLISH-02**: Extract `dumpK6OperatorDiagnostics` in `e2e/k6_operator_test.go` (~35 lines of repeated `exec.Command("kubectl", ...).Output()` calls) into a declarative helper taking a list of `{resource, namespace, format}` tuples. Single append point for future dumps. **Complete 2026-04-20 (13-01) — `k6DiagDump` struct + `emitK6Dump` helper drive 5 get-shaped dumps; controller-logs stay explicit; output byte-identical.**
- [x] **POLISH-03**: Resolve 3 INFO items from 08.1-REVIEW.md: **Complete 2026-04-20 (13-01) — all 3 items closed.**
  - [x] IN-01: Ambiguous warning message at `metric.go:246-249` when AR has non-controller Rollout owner ref — rephrase for operator clarity. **Split into zero-refs vs N-refs-no-controller branches; `ownerRefCount` slog field added.**
  - [x] IN-02: `populateFromRollout` at `step.go:261-274` does not defend against empty `rollout.Name` when `rollout.UID` is set — add a defensive check with a slog.Warn + skip (kube-apiserver would reject anyway, but fail-fast is cheaper). **Defensive branch added; Namespace fall-through preserved.**
  - [x] IN-03: `TestTriggerRun_WithRolloutUID` at `operator_test.go:347-379` does not lock `Controller`/`BlockOwnerDeletion` nil state — add explicit assertions. **`assert.Nil` added across all 3 owner-ref tests (AR, Rollout, AR-wins-over-Rollout).**

## Future Requirements

Carried forward from v0.3.0-REQUIREMENTS.md. Not in v0.4.0. Tracked for future consideration.

### Kubernetes Job Provider (likely v0.5.0 or later)

- **JOB-01**: Plugin creates a `batch/v1` Job with k6 container and ConfigMap volume mount
- **JOB-02**: Plugin polls Job status and extracts result from container exit code
- **JOB-03**: Plugin cleans up completed Jobs with background propagation policy

### Other (no target milestone yet)

- **SEC-01**: Step plugin secret handling via `secretKeyRef` (upstream Argo Rollouts limitation — see memory `project_step_plugin_secret_limitation.md`)
- **CUST-01**: Custom k6 metric support (user-defined Counter/Gauge/Rate/Trend)
- **SCRIPT-01**: PersistentVolume script sourcing for scripts > 1 MiB
- **SCRIPT-02**: Multi-file k6 module import support
- **DOCS-03**: README in-cluster quick-start section (duplicates `examples/k6-operator/` but improves first-touch experience)
- **DOCS-04**: `CHANGELOG.md` standup — machine-readable changelog for consumers bumping versions

## Out of Scope

Explicitly excluded for v0.4.0 and beyond. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Retroactive cleanup of TestRuns created before v0.4.0 | Users can run `kubectl delete testruns -l app.kubernetes.io/managed-by=argo-rollouts-k6-plugin -n <ns>` manually; no migration logic warranted. |
| Configurable cleanup policy (Always/Never/OnSuccess) | Single sensible default (cleanup after terminal analysis) keeps config surface small. Users who want to keep CRs for debugging can use `kubectl get events` + argo-rollouts controller logs. |
| Cleanup delay / grace period | TestRun CRs don't contain unique data once `GetRunResult` has returned the terminal state. Delaying deletion just extends the leak window. |
| Local binary execution (subprocess) | Anti-feature from v0.3.0 Out of Scope — corrupts go-plugin stdout protocol (grafana/k6#3744). |
| Automatic k6-operator installation | User's responsibility. Plugin fails gracefully with clear error if CRDs missing. |
| Retry loop on cleanup failure | Per GC-03, cleanup is best-effort. Controller retries are the wrong abstraction; kube GC will catch orphans anyway via owner-ref cascade when the AR/Rollout itself is deleted. |

## Traceability

Which phases cover which requirements. Populated by gsd-roadmapper.

| Requirement | Phase | Status |
|-------------|-------|--------|
| GC-01 | Phase 11 | Complete (11-01, 2026-04-20) |
| GC-02 | Phase 11 | Complete (11-02, 2026-04-20) |
| GC-03 | Phase 11 | Complete (11-01 metric-side + 11-02 step-side, 2026-04-20) |
| GC-04 | Phase 11 | Complete (a/b/d via 11-01; c via 11-02, 2026-04-20) |
| TEST-02 | Phase 12 | Complete (12-01, 2026-04-20) |
| POLISH-01 | Phase 13 | Complete (13-01, 2026-04-20) |
| POLISH-02 | Phase 13 | Complete (13-01, 2026-04-20) |
| POLISH-03 | Phase 13 | Complete (13-01, 2026-04-20) |

**Coverage:**
- v0.4.0 requirements: 8 total
- Mapped to phases: 8 (Phase 11: 4, Phase 12: 1, Phase 13: 3)
- Unmapped: 0

---
*Requirements defined: 2026-04-20 at v0.4.0 milestone start*
*Traceability populated: 2026-04-20 at v0.4.0 roadmap creation*
