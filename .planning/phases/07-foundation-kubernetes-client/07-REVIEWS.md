---
phase: 7
reviewers: [codex]
reviewed_at: 2026-04-15T23:45:00Z
review_pass: 2
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md]
previous_review: 2026-04-15 (pass 1 — concerns led to plan revision)
---

# Cross-AI Plan Review — Phase 7

> **Review Pass 2** — Plans were revised after pass 1 to address HIGH concerns (config validation trap, InitPlugin vs lazy init, sync.Once permanent failure, script propagation, namespace ambiguity). This pass reviews the revised plans.

## Codex Review

### Plan 07-01 Review

#### Summary
Plan 07-01 is directionally correct and matches the chosen provider-pattern architecture: a thin router, backward-compatible defaulting to Grafana Cloud, and config extension in a single shared struct fit the current codebase well. The main weakness is that the validation design is underspecified relative to the current implementation: today validation lives in `metric.parseConfig()` and `step.parseConfig()`, while the current `provider.Provider` interface has no `Validate` method. Unless that interface change and its test fallout are handled explicitly, this plan risks introducing duplicated or inconsistent validation paths.

#### Strengths
- Keeps routing thin and per-call, which fits the existing stateless provider model.
- Preserves backward compatibility by defaulting empty `provider` to Grafana Cloud.
- Uses a single `PluginConfig`, which avoids splitting config parsing logic across binaries.
- Exact-match provider resolution avoids ambiguous normalization behavior.
- Router-level rejection of unknown providers is a good control point for backend selection.
- Test coverage emphasis on dispatch/default behavior is appropriate for this layer.

#### Concerns
- **HIGH**: The plan relies on per-provider validation, but the current `provider.Provider` interface does not expose `Validate`. The plan does not list changes to `internal/provider/provider.go`, `internal/provider/providertest/mock.go`, or any other callers/mocks that would need to absorb that interface change.
  - *Note: Revised plan addresses this by keeping Validate() outside the Provider interface on K6OperatorProvider and using IsGrafanaCloud() in parseConfig. The concern is partially resolved but validation remains split across metric.go and step.go.*
- **MEDIUM**: Validation remains split across `internal/metric/metric.go` and `internal/step/step.go`. If both files gate validation differently, drift is likely over time.
- **MEDIUM**: "Unknown providers pass parseConfig" means invalid config is accepted until runtime dispatch. That is survivable, but it weakens early feedback and may produce inconsistent UX between metric and step entrypoints.
- **LOW**: Adding `k8s.io/client-go` and `k8s.io/api` as direct dependencies in this plan is slightly out of order; this plan itself does not use them yet.
- **LOW**: `Router.Name() == "router"` is fine mechanically, but if any logging/error handling uses `Name()` without also logging the resolved backend, diagnostics get less clear.

#### Suggestions
- Add the validation contract explicitly to the design:
  - Either extend `provider.Provider` with `Validate(*PluginConfig) error`,
  - Or define a narrow secondary interface such as `type ValidatingProvider interface { Validate(*PluginConfig) error }`.
- Centralize provider selection + validation in one place. `metric` and `step` should ideally parse JSON once, then delegate provider-specific validation through the router instead of hard-coding provider branches in both packages.
- Reject unknown providers during config parsing if possible, not only at dispatch time. That gives earlier, clearer failure.
- Move the direct dependency promotion to 07-02 unless there is a tooling reason to land it earlier.
- Add tests that cover `provider: ""`, `provider: "grafana-cloud"`, `provider: "k6-operator"`, and an unknown provider through the actual `parseConfig` entrypoints, not only the router.

#### Risk Assessment
**MEDIUM**. The architecture is sound, but the interface/validation gap is real. If that contract is not nailed down up front, the result will be duplicated validation logic and brittle tests.

---

### Plan 07-02 Review

#### Summary
This plan gets most of the foundation work into the right place: lazy in-cluster client ownership inside the operator provider, fake-client injection for tests, and local `readScript()` logic inside the provider all align with the stated decisions. The biggest problem is a direct mismatch with the phase requirements around namespace defaulting: the plan falls back to `"default"`, while the project decisions and success criteria say the namespace should default to the rollout namespace. That is not a minor detail; it affects correctness for real workloads.

#### Strengths
- Keeps Kubernetes concerns isolated inside `K6OperatorProvider`, which avoids contaminating Grafana Cloud paths.
- Lazy client creation is the right default for backward compatibility and avoids touching K8s in Grafana-only deployments.
- `WithClient(fake)` is a practical test seam.
- `readScript()` with explicit errors for missing ConfigMap, missing key, and empty content is a good Phase 7 boundary.
- Wiring the router in both binaries at this stage is appropriate so backend selection is exercised end-to-end.
- Test plan covers the important first-order cases around client init and ConfigMap reads.

#### Concerns
- **HIGH**: Namespace fallback to `"default"` conflicts with D-09 and success criterion 3, which require defaulting to the rollout namespace. This plan does not actually achieve that requirement.
  - *Note: Revised plan explicitly documents the fallback chain as cfg.Namespace -> "default" and defers rollout namespace injection to Phase 8. The concern is valid but may be an acceptable scope decision if the success criterion is updated.*
- **HIGH**: The plan does not explain how rollout namespace reaches `K6OperatorProvider`. The current provider interface only accepts `context.Context` and `*PluginConfig`; no namespace-bearing runtime object is passed through. This is a dependency/design gap, not just an implementation detail.
  - *Note: Revised plan acknowledges this and defers to Phase 8. Metric/step layers will inject namespace from AnalysisRun/Rollout ObjectMeta into cfg.Namespace before calling provider methods.*
- **MEDIUM**: Permanent failure caching in `sync.Once` is risky. A transient failure on first operator use can wedge the process until restart.
  - *Note: Revised plan explicitly documents this as intentional design (InClusterConfig reads local files, not network — failures are configuration errors, not transient). Includes dedicated test and block comment.*
- **MEDIUM**: If provider `Validate()` performs existence checks via `readScript()`, config validation may now hit the Kubernetes API on every parse/dispatch path. That is acceptable for operator mode, but it should be explicit because it changes the cost and timing of validation.
- **MEDIUM**: A stub `TriggerRun()` that always returns "not yet implemented" means successful routing to `k6-operator` still fails for real executions in this phase. That may be acceptable, but the plan should say so plainly so the phase goal is not overstated.
- **LOW**: No mention of guarding against oversized ConfigMap payloads. A single script is usually fine, but error handling should remain clear if content is unexpectedly large.
- **LOW**: `WithClient()` consuming `sync.Once` with a no-op is workable, but it is a slightly opaque pattern; tests should confirm it behaves predictably under repeated calls.

#### Suggestions
- Resolve namespace propagation now. Options:
  - Add namespace to `PluginConfig` during `metric`/`step` parsing from the rollout/analysis context.
  - Or extend provider method inputs so runtime namespace is available without encoding it into config.
- Do not fall back to `"default"` unless the requirements are changed. If rollout namespace cannot be plumbed in Phase 7, call that out as an unmet criterion instead of silently changing behavior.
- Reconsider permanent error caching. A mutex-guarded lazy init that retries until success is safer than `sync.Once` if first-use failures can be transient.
- Be explicit about `Validate()` scope:
  - Syntactic validation only, or
  - Full existence checks that call Kubernetes.
  Right now the plan implies both.
- Add tests for namespace precedence:
  - explicit `cfg.Namespace`
  - omitted namespace using rollout namespace
  - empty namespace when no rollout namespace is available, if that state can exist
- Document user-visible semantics for this phase: routing and script loading are implemented; operator execution is still stubbed until Phase 8.

#### Risk Assessment
**MEDIUM-HIGH**. The provider ownership and lazy-init structure are solid, but the namespace-default mismatch means the current plan does not fully meet the phase goal as written. That is the main issue to fix before implementation.

---

## Consensus Summary

*Single reviewer (Codex) — consensus analysis requires 2+ reviewers. Findings below are from Codex only.*

### Key Concerns (prioritized)

1. **Validation interface contract (HIGH — partially resolved)** — Plan 01 keeps Validate() outside the Provider interface (on K6OperatorProvider directly) and uses IsGrafanaCloud() in parseConfig. This avoids interface changes but leaves validation split across metric.go and step.go. Acceptable for Phase 7 scope but creates drift risk.

2. **Namespace fallback chain (HIGH — acknowledged, deferred)** — Plan 02 uses cfg.Namespace -> "default" fallback. D-09 says "rollout namespace." Revised plan explicitly defers rollout namespace injection to Phase 8 and documents the gap. The success criterion should be validated against this deferred scope.

3. **Rollout namespace plumbing (HIGH — deferred to Phase 8)** — No mechanism in Phase 7 to inject rollout/AnalysisRun namespace into cfg.Namespace. Phase 8 will have metric/step layers populate it from ObjectMeta. Acceptable if Phase 7 success criteria don't require rollout namespace behavior.

4. **sync.Once permanent failure (MEDIUM — explicitly addressed)** — Revised plan documents permanent failure as intentional design, adds dedicated test and block comment. InClusterConfig reads local pod filesystem, not network. Acceptable design choice.

5. **Script content propagation (MEDIUM — documented)** — readScript returns string. TriggerRun uses it internally. Phase 8's createTestRun will consume it. Contract documented in TriggerRun block comment.

6. **Stub TriggerRun returns error (MEDIUM)** — k6-operator routing works end-to-end but actual execution returns "not yet implemented." Phase 7 goal is routing + script loading, not execution. Acceptable but should be clear in phase documentation.

### Strengths Confirmed
- Two-plan split is clean and reviewable
- Router pattern fits the existing codebase well
- Backward compatibility preserved for Grafana Cloud users
- TDD approach with fake client injection is testable and sound
- Scope is appropriately narrow for foundation work
- Review concerns from pass 1 were systematically addressed in revised plans

### Resolution Status (vs Pass 1)

| Pass 1 Concern | Severity | Status in Pass 2 |
|----------------|----------|-------------------|
| Config validation trap (parseConfig requires GC fields) | HIGH | **Resolved** — parseConfig refactored with IsGrafanaCloud() gating |
| InitPlugin vs lazy init mismatch | HIGH | **Resolved** — success criterion updated to say "lazily on first k6-operator provider call via sync.Once" |
| sync.Once permanent failure | HIGH | **Resolved** — documented as intentional, test + block comment added |
| Script content propagation | MEDIUM | **Resolved** — contract documented in TriggerRun, readScript returns string for Phase 8 |
| Namespace resolution ambiguity | MEDIUM | **Partially resolved** — explicit fallback chain documented, rollout namespace deferred to Phase 8 |

### Remaining Items Before Execution
1. Confirm Phase 7 success criteria don't require rollout namespace injection (deferred to Phase 8)
2. Consider centralizing per-provider validation to reduce drift risk between metric.go and step.go
3. Ensure unknown providers are rejected as early as possible (parseConfig or Router dispatch)
