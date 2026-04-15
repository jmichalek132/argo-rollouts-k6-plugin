---
phase: 7
reviewers: [codex]
reviewed_at: 2026-04-15T23:45:00Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md]
---

# Cross-AI Plan Review -- Phase 7

## Codex Review

### Plan 07-01 Review

#### Summary
Plan 07-01 is directionally sound and appropriately scoped for the first wave: extending `PluginConfig`, introducing a thin Router, and promoting Kubernetes dependencies establishes the minimum abstraction needed for multiple backends without disrupting the existing Grafana Cloud path. The main gap is that the plan understates how provider-specific validation will interact with the current duplicated config validation in `internal/metric` and `internal/step`, and it does not fully pin down contract details like router `Name()` semantics and config compatibility behavior.

#### Strengths
- Keeps Wave 1 narrowly focused on routing and config shape instead of mixing backend implementation into the same change.
- Matches the existing codebase well: both binaries currently instantiate the cloud provider directly, so a Router insertion point is clean.
- Preserves backward compatibility by defaulting empty `provider` to Grafana Cloud.
- Uses functional options for registration, which fits the current provider construction style.
- TDD scope is reasonable for the Router and likely enough to catch dispatch regressions early.
- Dependency promotion in this wave is sensible if Wave 2 will immediately consume `client-go`.

#### Concerns
- **HIGH**: The plan says "per-provider validation" but does not account for the current validation already happening in `metric.parseConfig()` and `step.parseConfig()`. As written, those paths still require Grafana Cloud fields like `apiToken` and `stackId`, which would break `k6-operator` configs before routing ever happens.
- **MEDIUM**: `PluginConfig` extension is underspecified around optionality and backward compatibility. Adding provider/operator fields is safe only if JSON tags and zero values do not alter existing behavior or tests.
- **MEDIUM**: Router `Name()` is called out as ambiguous in research, but the plan does not resolve it. Returning a fixed router name vs resolved provider name affects logs and debugging.
- **MEDIUM**: Unknown-provider handling is mentioned, but normalization rules are not. Case sensitivity and whitespace behavior should be explicit to avoid surprising failures.
- **LOW**: Promoting `k8s.io/client-go` and `k8s.io/api` in Wave 1 may slightly increase churn before any code uses them; not a major issue, but it adds dependency noise if Wave 2 slips.
- **LOW**: The threat model for this wave appears more generic than actionable. "Provider field spoofing" and "config tampering" do not materially change risk inside this plugin unless tied to specific trust boundaries.

#### Suggestions
- Refactor config validation ownership in this wave, not Wave 2. At minimum, move shared syntactic validation into a common place and reserve provider-specific required-field checks for the Router/provider layer.
- Define Router `Name()` explicitly. Best option is usually a stable name like `"router"` for the Router itself, while provider-specific logs should use the resolved backend name.
- Specify provider normalization rules: either exact-match only, or `strings.TrimSpace` plus lowercasing. If exact-match is intended, document that clearly.
- Add tests for JSON backward compatibility: existing configs without `provider`, `namespace`, or `configMapRef` should unmarshal identically and keep passing.
- Add a test that `provider: grafana-cloud` still accepts existing cloud-only configs and that `provider: k6-operator` does not require cloud credentials.
- Consider deferring direct dependency promotion until the first actual `client-go` import if the team wants smaller review units.

#### Risk Assessment
**MEDIUM.** The routing abstraction itself is low risk, but the validation boundary is currently a real trap: if not addressed in Wave 1, the code can compile and route correctly in tests while still rejecting valid `k6-operator` configs at runtime.

---

### Plan 07-02 Review

#### Summary
Plan 07-02 covers the missing functional pieces for Phase 7 and is mostly aligned with the roadmap: a stub `K6OperatorProvider`, lazy Kubernetes client creation, ConfigMap script loading, and wiring both binaries through the Router. The largest issue is a requirements mismatch: the roadmap success criterion says the plugin creates a Kubernetes client during `InitPlugin`, while the plan deliberately uses lazy initialization inside the provider on first `k6-operator` call. That may be the right design, but it needs to be reconciled explicitly. There are also a few missing edge cases around namespace resolution, error caching with `sync.Once`, and how loaded script content reaches downstream providers.

#### Strengths
- Keeps the operator backend intentionally narrow: client init plus ConfigMap script loading, without pulling in actual k6-operator execution yet.
- Lazy init with injectable fake client is a good fit for testability and avoids forcing Kubernetes assumptions on Grafana Cloud users.
- ConfigMap reading inside the provider matches the locked decision and avoids premature package extraction.
- Wiring both binaries in the same wave is correct; otherwise the feature would exist in code but remain unreachable.
- The planned tests cover the primary unhappy paths for ConfigMap loading.
- Preserving `K6_BASE_URL` support maintains existing test and local-dev ergonomics.

#### Concerns
- **HIGH**: The plan conflicts with the stated success criterion "Plugin creates a working Kubernetes client ... during InitPlugin." The current plugin layers' `InitPlugin()` methods are no-ops, and the proposed design initializes lazily in provider calls instead. If unchanged, either the criterion or the implementation plan is wrong.
- **HIGH**: `sync.Once` plus initialization failure needs a precise retry policy. If `ensureClient()` stores a transient failure forever, the plugin may be permanently wedged until process restart. Your research already flags this, but the plan does not state the intended behavior.
- **MEDIUM**: The plan mentions ConfigMap reading but not how script content is represented in `PluginConfig` or otherwise passed downstream. FOUND-03 requires script content to be available to downstream providers, not merely read internally.
- **MEDIUM**: Namespace defaulting is not fully nailed down. The roadmap says default to the rollout namespace, but the plan's tests mention "default-ns" and research mentions empty string -> `"default"`. Those are different behaviors and can lead to incorrect cross-namespace reads.
- **MEDIUM**: `WithClient(fake)` covers test injection, but there is no mention of how `rest.InClusterConfig()` itself is abstracted for unit testing of init failure/success.
- **MEDIUM**: ConfigMap validation scope omits script size and encoding assumptions. A 1 MiB limit may be acceptable to ignore operationally, but empty/non-empty is not the only realistic failure mode.
- **LOW**: `nil-configmapref` behavior is listed as a test, but the plan does not say whether that means "valid when provider is grafana-cloud" or "error when provider is k6-operator." That distinction matters.
- **LOW**: `klog` stdout contamination is called out, but the plan does not specify the mitigation. With `go-plugin`, this deserves an explicit implementation note, not just a lint target.

#### Suggestions
- Reconcile the `InitPlugin` requirement before implementation. Either:
  1. Change the phase success criterion to "client can be created from in-cluster credentials and is initialized lazily on first `k6-operator` use," or
  2. Extend the provider/plugin interface so `InitPlugin()` can trigger provider initialization intentionally.
- Avoid plain `sync.Once` if retry-after-failure is desired. A mutex plus cached client/error state is often clearer when transient cluster/API startup failures should not be permanent.
- Define exactly where the loaded script body lives after `readScript()`: for example `cfg.Script` populated before dispatch, or an operator-provider-specific internal request object. That contract should appear in the plan.
- Clarify namespace resolution in tests and implementation:
  - If `config.namespace` is set, use it.
  - Else use rollout namespace.
  - Only fall back to `"default"` if rollout namespace is unavailable and that fallback is explicitly intended.
- Add tests for in-cluster config failure and clientset creation failure, not just fake-client happy-path reads.
- Add a test for a ConfigMap key present but containing whitespace-only content if "non-empty" is meant semantically rather than byte-length only.
- Specify the `klog` mitigation explicitly, such as redirecting/initializing `klog` to stderr before any client-go code executes.
- Add an integration-style test at the plugin layer that a `k6-operator` config reaches the provider path without requiring Grafana Cloud credentials.

#### Risk Assessment
**MEDIUM-HIGH.** The scope is still reasonable, but this wave carries the real behavior change and has a few architectural mismatches that could cause phase-goal failure even if unit tests pass: especially `InitPlugin` vs lazy init, validation ordering, and ambiguous namespace/script propagation semantics.

---

## Consensus Summary

*Single reviewer -- consensus analysis requires 2+ reviewers.*

### Key Concerns (prioritized)

1. **Validation boundary trap (HIGH)** -- `metric.parseConfig()` and `step.parseConfig()` currently require Grafana Cloud fields (`apiToken`, `stackId`). k6-operator configs will fail validation before reaching the Router unless config validation is refactored to be per-provider.

2. **InitPlugin vs lazy init mismatch (HIGH)** -- Success criterion says "creates a working Kubernetes client during InitPlugin" but the plan uses lazy `sync.Once` init on first k6-operator call. Needs explicit reconciliation.

3. **sync.Once permanent failure (HIGH)** -- If `rest.InClusterConfig()` fails transiently, `ensureClient()` caches the error permanently. Plan should state whether this is intentional (process restart required) or if retry is needed.

4. **Script content propagation (MEDIUM)** -- FOUND-03 requires script content "available to downstream providers." The plan reads it internally in `readScript()` but doesn't specify how it reaches Phase 8's TestRun CR creation.

5. **Namespace resolution ambiguity (MEDIUM)** -- Roadmap says "rollout namespace default," plan says `"default"` fallback. These are different behaviors.

### Strengths Confirmed
- Two-wave split is clean and reviewable
- Router pattern fits the existing codebase well
- Backward compatibility preserved for Grafana Cloud users
- TDD approach with fake client injection is testable and sound
- Scope is appropriately narrow for foundation work

### Actionable Items Before Execution
1. Refactor `parseConfig()` validation to be per-provider aware (or defer Grafana Cloud field checks to the cloud provider)
2. Reconcile InitPlugin success criterion with lazy init design
3. Document sync.Once failure-is-permanent as intentional design choice
4. Define script content contract for downstream consumption
5. Pin down namespace fallback chain: config -> rollout namespace -> "default"
