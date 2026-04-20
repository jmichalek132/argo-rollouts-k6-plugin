---
phase: 10-documentation-e2e
plan: 02
subsystem: e2e-test-suite
tags: [e2e, testing, k6-operator, kind, rbac]
requires:
  - e2e/main_test.go harness with kind + envfuncs
  - existing mock-k6 Deployment + Service in argo-rollouts namespace
  - internal/provider/operator (GetRunResult, parseSummaryFromPods)
  - k6-operator v1.3.2 bundle (remote URL)
provides:
  - TestK6OperatorStepPass and TestK6OperatorMetricPass e2e tests
  - installK6Operator() env.Func with CRD establishment wait
  - preloadK6Image() pre-loading pinned grafana/k6:0.56.0 into kind
  - applyK6PluginRBAC() ClusterRole + ClusterRoleBinding for plugin permissions
  - /health endpoint on mock server for k6 runner pods
  - Full-path metric validation from AnalysisRun.status.metricResults
affects:
  - e2e/main_test.go (new setup steps in testenv.Setup chain)
  - e2e/mock/main.go (new /health and / handlers)
tech-stack:
  added: []
  patterns:
    - features.New().Setup().Assess().Teardown().Feature() (reused)
    - diagnostic dump on failure (TestRuns, pods) mirroring waitForAnalysisRun pattern
    - inline YAML via fmt.Sprintf with cfg.Namespace() (reused)
    - kubectl apply --server-side for large CRD bundles
key-files:
  created:
    - e2e/k6_operator_test.go
    - e2e/testdata/k6-script-configmap.yaml
    - e2e/testdata/analysistemplate-k6op.yaml
    - e2e/testdata/rollout-step-k6op.yaml
  modified:
    - e2e/main_test.go (add 3 new setup functions + chain wiring)
    - e2e/mock/main.go (add /health + / GET handler)
decisions:
  - "Pin k6 runner image to grafana/k6:0.56.0 (not latest) -- reproducibility"
  - "Wait for k6-operator CRD Established condition before any CR creation"
  - "Validate extracted metric value from AnalysisRun.status.metricResults[0].measurements[0].value, not just terminal phase"
  - "Verify step plugin created a TestRun CR by counting testruns in the namespace"
  - "RBAC applied inline in setup (no separate testdata file) for setup-order clarity"
  - "No K6_LIVE_TEST skip guard -- k6-operator tests use real k6-operator, not mock Cloud API"
  - "Use --server-side apply for k6-operator bundle (large CRDs)"
metrics:
  duration: "~8 minutes"
  completed: "2026-04-16"
  tasks_completed: 2
  files_created: 4
  files_modified: 2
---

# Phase 10 Plan 02: k6-operator E2E Test Suite Summary

## One-Liner

Capstone e2e validation of the k6-operator pipeline: installs real k6-operator in kind, applies RBAC, runs real k6 runner pods against a cluster-internal mock `/health` endpoint, and asserts both Rollout terminal phase AND extracted metric values from AnalysisRun metricResults.

## What Was Built

### Setup infrastructure (e2e/main_test.go)
- `installK6Operator()` -- applies the k6-operator v1.3.2 bundle with `--server-side`, waits for `testruns.k6.io` and `privateloadzones.k6.io` CRDs to reach `Established`, then waits for the `k6-operator-controller-manager` deployment rollout.
- `preloadK6Image(clusterName)` -- `docker pull grafana/k6:0.56.0 && kind load docker-image`, avoiding Docker Hub rate limits and `latest` non-determinism.
- `applyK6PluginRBAC()` -- applies inline YAML for a ClusterRole granting `k6.io/testruns+privateloadzones {create,get,list,watch,delete}`, `pods/list`, `pods/log/get`, `configmaps/get`, plus a ClusterRoleBinding to the `argo-rollouts` ServiceAccount.
- Wired all three into the `testenv.Setup(...)` chain between `installArgoRollouts()` and `setupFn`.

### Mock server (e2e/mock/main.go)
- Added a `/health` and `/` GET handler returning `200 {"status":"ok"}` so k6 runner pods have a deterministic target. Inserted after all existing k6 Cloud API routes, before the 404 fallthrough.

### E2E tests (e2e/k6_operator_test.go)
- **TestK6OperatorStepPass** -- creates Service, ConfigMap, and Rollout; waits for initial Healthy; patches annotation to trigger canary; asserts Rollout reaches `Healthy` within 5 minutes AND at least one TestRun CR was created in the namespace.
- **TestK6OperatorMetricPass** -- creates ConfigMap, AnalysisTemplate, and inline AnalysisRun; asserts AnalysisRun reaches `Successful` AND reads `.status.metricResults[0].measurements[0].value` via `getAnalysisRunMetricValue()`, asserting it equals `"1"`. This proves the full path from TestRun creation through pod log reading to threshold evaluation.
- **Diagnostic dumps** -- `dumpK6OperatorDiagnostics()` prints TestRuns, labeled runner pods, and all pods in the test namespace on any failure, mirroring the existing `waitForAnalysisRun` timeout-dump pattern.
- Teardown deletes rollout/service/configmap/analysisrun/analysistemplate plus `testruns --all` with `--ignore-not-found`.

### Test fixtures (e2e/testdata/)
- `k6-script-configmap.yaml` -- 10-iteration k6 script targeting `mock-k6.argo-rollouts.svc.cluster.local:8080/health` with `handleSummary(data) => { stdout: JSON.stringify(data) }`.
- `analysistemplate-k6op.yaml` -- k6-operator metric plugin template with `provider: k6-operator`, `configMapRef`, `runnerImage: "grafana/k6:0.56.0"`.
- `rollout-step-k6op.yaml` -- canary Rollout with `jmichalek132/k6-step` plugin step using k6-operator config.

## Review Feedback Addressed

| Severity | Concern | Resolution |
|----------|---------|-----------|
| HIGH | RBAC missing from e2e setup | `applyK6PluginRBAC()` inline YAML in main_test.go setup chain |
| HIGH | Metric test only asserted terminal phase | `getAnalysisRunMetricValue()` + assertion `value == "1"` from metricResults |
| HIGH | `grafana/k6:latest` non-reproducible | Pinned to `grafana/k6:0.56.0` via `runnerImage` in fixtures and `preloadK6Image()` in setup |
| MEDIUM | installK6Operator did not wait for CRDs | `kubectl wait --for=condition=Established crd/testruns.k6.io crd/privateloadzones.k6.io --timeout=60s` |
| MEDIUM | RBAC fixtures missing | Inline RBAC YAML in `applyK6PluginRBAC()` |

## Verification

- `go vet -tags e2e ./e2e/...` -- clean
- `go build ./...` -- clean
- `go test -count=1 ./...` -- 100% green across unit tests (metric, provider, cloud, operator, step)
- Plan success-criteria greps:
  - `TestK6Operator` matches: 4 (2 function defs + 2 comment references)
  - `installK6Operator` matches: 3 (func def + comment + setup call)
  - `applyK6PluginRBAC` matches: 3 (func def + comment + setup call)
  - `condition=Established` matches: 1
  - `handleSummary` in ConfigMap: 1
  - `provider: k6-operator` in analysistemplate-k6op.yaml: present
  - `0.56.0` in rollout-step-k6op.yaml: present
  - `metricResults` in k6_operator_test.go: present (struct + comments)
  - `getAnalysisRunMetricValue` in k6_operator_test.go: present
- No occurrence of `grafana/k6:latest` except in a comment explaining why it's avoided.

## Deviations from Plan

None -- plan executed exactly as written. Every acceptance criterion met.

## Threat Flags

None. All new surface (TestRun CR creation, pod-log reading, ConfigMap read) is already registered in the plan's threat model (T-10-05..T-10-09) with `mitigate` dispositions, each implemented:
- T-10-05 (DoS on test timeout) -- 5-minute timeouts + diagnostic dump + kind Finish() destroys cluster
- T-10-06 (info disclosure via ConfigMap) -- no credentials in script; ephemeral kind
- T-10-07 (bundle URL tampering) -- pinned v1.3.2
- T-10-08 (runner image tampering) -- pinned grafana/k6:0.56.0, preloaded
- T-10-09 (RBAC privilege) -- least-privilege verbs scoped to argo-rollouts SA

## Known Stubs

None. All data paths are wired through real components (real k6-operator controller, real k6 binary, real handleSummary JSON, real metric value assertions).

## Commits

- `6561ed2` feat(10-02): add k6-operator install, RBAC, and /health to mock
- `7c73ba6` feat(10-02): add k6-operator e2e tests with full-path metric validation

## Requirements Satisfied

- TEST-01: e2e test suite validates TestRun creation and full metric extraction path

## Self-Check: PASSED

- e2e/k6_operator_test.go -- FOUND
- e2e/testdata/k6-script-configmap.yaml -- FOUND
- e2e/testdata/analysistemplate-k6op.yaml -- FOUND
- e2e/testdata/rollout-step-k6op.yaml -- FOUND
- e2e/main_test.go -- FOUND (modified)
- e2e/mock/main.go -- FOUND (modified)
- Commit 6561ed2 -- FOUND
- Commit 7c73ba6 -- FOUND
