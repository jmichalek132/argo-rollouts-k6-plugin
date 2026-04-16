---
phase: 10
reviewers: [codex]
reviewed_at: 2026-04-16T20:15:00Z
plans_reviewed: [10-01-PLAN.md, 10-02-PLAN.md]
---

# Cross-AI Plan Review — Phase 10

## Codex Review

### Plan 10-01: k6-operator Example YAML Directory

**Summary**
This plan is close to the documentation goal and fits the repo's existing examples model, but it underspecifies a few behaviors that matter for "works out of the box." The biggest gap is operational clarity: Argo Rollouts canary steps do not execute on the initial rollout, so step and analysis-gated Rollout examples need explicit update instructions or users will think the plugin is broken. The RBAC scope also needs to match the provider's real API calls in operator.go and summary.go, otherwise the least-privilege story will be weak.

**Strengths**
- Mirrors the existing `examples/` structure, so it is easy to discover and consistent with D-01.
- Separates step and metric examples, which matches the product surface and D-03.
- Includes a copy-paste-ready ConfigMap script with `handleSummary`, which is necessary for the k6-operator metric path.
- README scope is reasonable and avoids bloating the main README.

**Concerns**
- `HIGH`: The plan does not say the README will explain that Rollout canary steps are skipped on the initial deploy. Without that, both `rollout-step` and `rollout-metric` can appear non-functional until a second revision is pushed.
- `MEDIUM`: The RBAC example is broader than the provider appears to require. Current code uses `create/get/delete` on `testruns` or `privateloadzones`, `get` on `configmaps`, `list` on `pods`, and `get` on `pods/log`; `watch` and pod `get` are not obviously required by the plugin implementation.
- `MEDIUM`: The plan does not mention a `ClusterRoleBinding` or namespaced `RoleBinding` example. A correct `ClusterRole` alone is not enough for users to succeed.
- `MEDIUM`: The metric example description says "metric: thresholds," but the phase goal and D-06 emphasize validating `handleSummary` extraction. A thresholds-only example does not demonstrate the richer metric path.
- `LOW`: Keeping docs YAML and e2e YAML separate risks drift unless one is explicitly treated as canonical.

**Suggestions**
- Add explicit README instructions for "initial deploy" versus "triggering the first canary update."
- Include binding guidance for the `argo-rollouts` service account, not just the role object.
- Tighten RBAC to the actual calls, or explicitly justify any broader verbs as forward-compatible.
- Make the metric example use at least one extracted metric such as `http_req_duration` or `http_req_failed`, not only `thresholds`.
- Reuse the same script shape between docs and e2e fixtures to reduce divergence.

**Risk Assessment:** MEDIUM

---

### Plan 10-02: e2e Test Suite

**Summary**
This plan covers the right area and sensibly builds on the existing e2e harness in e2e/main_test.go, but it does not yet prove the full success criteria. As written, it mostly validates "controller reaches Healthy/Successful" rather than the full k6-operator path from CR creation through runner pod logs and parsed metric values. It also misses the RBAC plumbing the in-cluster plugin needs to create CRs and read ConfigMaps/pod logs.

**Strengths**
- Reuses the current `e2e-framework` setup and feature pattern instead of inventing a second harness.
- Pins the k6-operator bundle version, which is good for reproducibility.
- Adds a deterministic `/health` target and iterations-based script, which is appropriate for stable e2e.
- Includes teardown and timeout thinking, which matches current test style.

**Concerns**
- `HIGH`: The plan does not mention applying the new RBAC and binding it to the Argo Rollouts controller service account. Without that, the plugin's in-cluster client will hit `forbidden` on TestRun creation, ConfigMap reads, or pod-log reads.
- `HIGH`: The tests described do not actually validate D-06 end to end. Waiting for Rollout `Healthy` or AnalysisRun `Successful` does not prove `handleSummary` parsing or metric extraction.
- `HIGH`: Preloading `grafana/k6:latest` conflicts with the otherwise pinned strategy. `latest` will make the suite non-reproducible and can break independently of the plugin.
- `MEDIUM`: `installK6Operator()` should wait for CRDs to be established as well as the controller deployment to be ready; "controller is up" alone is not always enough for immediate CR creation.
- `MEDIUM`: The fixture list omits the RBAC manifests and any metric fixture that asserts a numeric extracted value, so the plan is narrower than TEST-01's stated validation path.
- `LOW`: Pulling the bundle from a remote URL during e2e increases flake risk in CI.

**Suggestions**
- Add an explicit setup step that applies the k6-operator RBAC example plus binding before any test runs.
- Replace `grafana/k6:latest` with a pinned tag or digest known to work with k6-operator `v1.3.2`.
- For the metric path, assert the measured value in `AnalysisRun.status.metricResults[*].measurements[*].value`, not just terminal phase.
- Add assertions that a `TestRun` was created and at least one runner pod completed, and dump `TestRun`, pod status, and pod logs on timeout.
- Wait for the `testruns.k6.io` CRD to be established before creating fixtures.
- Consider using one threshold/pass test and one extracted-metric test so both exit-code and `handleSummary` paths are covered.

**Risk Assessment:** HIGH

---

## Consensus Summary

### Agreed Strengths
- Plans correctly build on existing project infrastructure (e2e-framework, example directory pattern)
- k6-operator bundle version is pinned (v1.3.2), matching go.mod dependency
- Separation of step and metric plugin examples is clear and well-scoped
- handleSummary + iterations-based k6 script is the right approach for deterministic e2e

### Agreed Concerns
- **RBAC binding missing from both plans** — ClusterRole is defined but no ClusterRoleBinding is applied in e2e setup or documented in examples. The plugin will hit 403 Forbidden without it.
- **e2e tests validate terminal phase, not full path** — Waiting for "Healthy"/"Successful" proves the controller reached a terminal state but doesn't prove handleSummary JSON was parsed or metric values were extracted. D-06 asks for full path validation.
- **`grafana/k6:latest` is non-reproducible** — Conflicts with the otherwise pinned versioning strategy. Could break independently of the plugin.
- **No "initial deploy" documentation** — Argo Rollouts canary steps skip on first deploy. Users applying examples will see no k6 activity until they push a second revision.

### Divergent Views
None — single reviewer session.

---

*Reviewed: 2026-04-16 by Codex CLI*
*To incorporate feedback: `/gsd-plan-phase 10 --reviews`*
