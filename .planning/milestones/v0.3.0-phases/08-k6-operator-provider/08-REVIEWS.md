---
phase: 8
reviewers: [codex]
reviewed_at: 2026-04-16T00:00:00Z
plans_reviewed: [08-01-PLAN.md, 08-02-PLAN.md]
---

# Cross-AI Plan Review — Phase 8

## Codex Review

### Plan 08-01 Review

#### Summary
Plan 08-01 is a solid foundation slice: it isolates pure object construction and exit-code interpretation into testable units, aligns well with the stated decisions, and directly addresses the hardest semantic gap in the operator integration: `stage=finished` not meaning pass/fail. The main weaknesses are around validation ownership, a few likely edge cases in naming and pod inspection, and a mild risk that `buildPrivateLoadZone` enters scope before Wave 2 proves the provider-routing behavior that actually chooses it.

#### Strengths
- Clear separation between pure construction logic and cluster-observation logic.
- Good use of table-driven tests for deterministic helpers.
- Correctly incorporates the known k6-operator pitfall that `TypeMeta` must be set before unstructured conversion.
- `stageToRunState("finished") -> Running` is the right design given issue #577.
- Owner reference helper makes D-09 explicit and testable instead of burying it in provider code.
- Exit-code precedence `Errored > Failed > Passed` is defensible and matches operational expectations.
- Naming helper centralization reduces drift across provider operations.
- `json:"-"` for rollout-scoped metadata is a clean way to keep internal execution data out of external config surfaces.

#### Concerns
- **HIGH**: `checkRunnerExitCodes()` does not specify behavior for zero runner pods found. That case is likely during reconciliation lag and can easily be misclassified.
- **HIGH**: `checkRunnerExitCodes()` may race with pod lifecycle if some runner pods have not appeared yet, are still pending, or have restarted containers; the plan mentions nil `Terminated` but not expected pod count or retries across calls.
- **MEDIUM**: `testRunName(rolloutName, namespace)` includes `namespace` in the hash input but the documented naming convention says `k6-<rollout>-<hash>`. That is probably fine, but it should be explicitly justified to avoid confusion and accidental incompatibility with cleanup logic.
- **MEDIUM**: Parallelism validation is only referenced in the threat model, not in the actual tasks. If validation lives elsewhere, say so; if not, this foundation plan is incomplete.
- **MEDIUM**: `buildPrivateLoadZone()` in Wave 1 may be premature if PLZ create semantics differ meaningfully from TestRun in Wave 2. That creates a chance of speculative abstraction.
- **LOW**: Label constants only mention `managed-by` and `rollout`; success criteria also imply consistent labels, but traceability may benefit from `analysisrun` or provider-type labels too.
- **LOW**: Owner reference edge-case testing is good, but the plan does not mention whether cross-namespace ownership constraints or GVK correctness are validated.

#### Suggestions
- Define `checkRunnerExitCodes()` behavior for these cases explicitly:
  - no runner pods found
  - some pods found but some containers not terminated
  - multiple containers per pod
  - restarted runner containers with prior exit codes
- Add tests that simulate progressive operator reconciliation across multiple `GetRunResult()` calls rather than only steady-state pod lists.
- State where config validation lives for `parallelism`, resources, image, env, and args. If not already present, add minimal validation in this wave.
- Document the hash input for `testRunName()` and ensure the same function is reused by `TriggerRun` and `StopRun`.
- Consider keeping `buildPrivateLoadZone()` behind a narrower helper or deferring it unless Wave 2 truly needs both CR builders immediately.
- Add test coverage for maximum rollout-name length and truncation behavior against the DNS/subresource constraints actually used by the CR name.

#### Risk Assessment
**MEDIUM**. The plan is well-structured and likely implementable, but correctness depends heavily on subtle pod-state edge cases. If `checkRunnerExitCodes()` semantics are underspecified, the provider can report false success/failure even with otherwise good code.

---

### Plan 08-02 Review

#### Summary
Plan 08-02 is directionally correct and maps well onto the phase goal: create CRs, poll state via single GET, infer final result from runner exit codes, and delete on stop. It is appropriately thin and avoids over-engineering. The biggest risks are around lifecycle identity: `StopRun` and `GetRunResult` need a reliable way to know whether the created object was a TestRun or PLZ, and deletion/lookup semantics are under-specified if only the generated name is retained. There is also some hidden coupling in changing `ensureClient()` to return two clients.

#### Strengths
- Good adherence to D-04: single GET per `GetRunResult()` keeps provider behavior compatible with the plugin's polling model.
- `dynamic.Interface` is the right fit for CR CRUD while preserving typed construction upstream.
- `WithDynClient` is a good testability hook.
- Trigger/Get/Stop responsibilities are cleanly separated and match the provider contract.
- Auto-detection of TestRun vs PLZ is consistent with the user decision and prevents config sprawl.
- Explicit delete on `StopRun` directly satisfies K6OP-08.
- Error propagation is called out rather than hidden, which is important for operator-facing debuggability.

#### Concerns
- **HIGH**: The plan does not say how `GetRunResult` and `StopRun` know which GVR to use after `TriggerRun` chose TestRun vs PLZ. If that choice is not persisted in `RunID` or equivalent state, later operations may query/delete the wrong resource kind.
- **HIGH**: The plan says `StopRun: dynamic Delete on TestRun/PLZ CR` but does not define idempotent behavior for `NotFound`. Abort/terminate paths should usually treat missing resources as success.
- **HIGH**: `GetRunResult` only mentions `NestedString(stage)` and exit-code fallback. It does not specify how missing CRs, malformed objects, or absent `stage` fields are handled.
- **MEDIUM**: Updating `ensureClient()` to return `(kubernetes.Interface, dynamic.Interface, error)` may ripple through existing Phase 7 code and tests more than the plan suggests.
- **MEDIUM**: Trigger flow mentions validation, but does not state whether failure happens before or after reading the script. Validation should happen as early as possible to avoid unnecessary I/O.
- **MEDIUM**: Owner references are mentioned in tests, but the runtime behavior if owner refs cannot be set correctly is not specified. If `AnalysisRunUID` is absent, cleanup relies entirely on explicit delete.
- **MEDIUM**: The plan does not mention what identifier is returned from `TriggerRun`. If it is only the CR name, later operations may lack namespace/kind/provider context unless those are encoded elsewhere.
- **LOW**: 18 tests is a good target, but the coverage statement is count-based rather than behavior-based. Important scenarios could still be missed.
- **LOW**: Logging is only referenced in the threat model. Since this provider will create and delete CRs, structured fields for namespace, kind, CR name, and rollout should be explicitly planned.

#### Suggestions
- Make the run identifier explicit. It should encode at least:
  - namespace
  - CR name
  - resource kind or GVR
  This removes ambiguity for `GetRunResult` and `StopRun`.
- Specify idempotent delete semantics: `NotFound` should return success on `StopRun`.
- Define exact `GetRunResult` behavior for:
  - CR not found before terminal state
  - missing `stage`
  - unknown `stage`
  - `finished` with no runner pods yet
  - `finished` with some pods incomplete
- Perform config validation before `readScript()` and before any client calls.
- Add tests for the full lifecycle contract:
  - `TriggerRun` returns a resolvable run handle
  - `GetRunResult` can use that handle without recomputing kind/name heuristically
  - `StopRun` succeeds after create, after delete, and when object is already gone
- If both TestRun and PLZ are supported, add a narrow abstraction for "selected CR kind + GVR" rather than duplicating branching logic across all methods.
- Call out RBAC failure modes explicitly in tests or error handling, since forbidden errors on CR create/delete are realistic.

#### Risk Assessment
**MEDIUM-HIGH**. The overall design is reasonable, but lifecycle identity is underspecified. If the provider cannot deterministically recover the created resource kind and namespace across `TriggerRun`, `GetRunResult`, and `StopRun`, the implementation will be fragile even if individual CRUD calls are correct.

---

## Consensus Summary

### Agreed Strengths
- Clean wave split: pure functions (Wave 1) vs integration (Wave 2) is well-designed
- TypeMeta pitfall handling is correct and explicitly tested
- Owner reference (D-09) treatment is explicit and testable
- Exit code precedence logic is defensible
- Single GET per poll (D-04) is correct for the plugin model
- Auto-detection of TestRun vs PLZ avoids config sprawl

### Agreed Concerns
- **HIGH**: Lifecycle identity gap — `GetRunResult` and `StopRun` re-derive GVR from config rather than persisting the choice made in `TriggerRun`. If config state changes between calls (unlikely but possible), operations target the wrong resource.
- **HIGH**: `checkRunnerExitCodes()` edge cases — zero pods found during reconciliation lag, pods not yet appeared, restarted containers with prior exit codes need explicit handling.
- **HIGH**: `StopRun` idempotency — `NotFound` on delete should be treated as success for abort paths.
- **MEDIUM**: Run identifier semantics — `TriggerRun` returns only the CR name. Namespace and GVR are re-derived, creating implicit coupling.
- **MEDIUM**: `GetRunResult` error handling for missing CRs, absent stage field, and malformed objects is underspecified.
- **MEDIUM**: Validation ordering — should happen before I/O (readScript).

### Divergent Views
N/A — single reviewer (Codex). Concerns listed above represent a single perspective.

---

*Reviewed: 2026-04-16 by Codex CLI*
