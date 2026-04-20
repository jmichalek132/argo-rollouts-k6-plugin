# Milestones

## v0.3.0 In-Cluster Execution (Shipped: 2026-04-20)

**Phases completed:** 7 phases (7, 8, 08.1, 08.2, 08.3, 9, 10), 13 plans.
**Timeline:** 2026-04-15 → 2026-04-20 (5 days).
**Git stats:** ~99 commits scoped to v0.3.0, 8,913 total Go LOC.

**Key accomplishments:**

- Provider routing via Router multiplexer + lazy in-cluster k8s client (`sync.Once`) + ConfigMap script sourcing — grafana-cloud-only deployments never touch the k8s API (Phase 7).
- k6-operator TestRun CRD lifecycle (TriggerRun/GetRunResult/StopRun) via dynamic client; pass/fail from runner pod exit codes (workaround for k6-operator#577); TestRun naming and `managed-by` labels (Phase 8).
- Metadata wiring through plugin layers: `cfg.Namespace`/`RolloutName`/`AnalysisRunUID`/`RolloutUID` flow from AnalysisRun/Rollout ObjectMeta through metric.go and step.go before dispatch; owner-ref precedence AR > Rollout > none in `parentOwnerRef` (Phase 08.1, inserted).
- `cfg.Parallelism=1` default in `buildTestRun` (Phase 08.2, inserted) — unblocked TestRuns that k6-operator was interpreting as `paused` when users omitted `parallelism`.
- Removed `spec.Cleanup=post` from buildTestRun (Phase 08.3, inserted retroactively) — k6-operator was cascade-deleting TestRun CRs before the plugin's Resume poll could read terminal status; 10s race dropped to zero.
- `handleSummary` JSON extraction from runner pod logs for metric plugin — same `successCondition` expressions work across grafana-cloud and k6-operator providers (Phase 9).
- RBAC example ClusterRole + AnalysisTemplate/Rollout example YAML + kind-cluster e2e suite with mock k6 API server; final e2e run 6/6 PASS, 261s (Phase 10).

**Deferred (tech debt):**

- Success-path TestRun/pod cleanup via `GarbageCollect` on metric plugin and symmetric post-terminal hook on step plugin. Cancellation paths (Terminate/Abort) still clean up correctly via `StopRun`.
- e2e coverage for combined step+metric canary with mid-run AnalysisRun deletion (D-07 owner-ref precedence under real Kubernetes GC cascade). Unit-tested; not e2e-tested.

---

## v0.2.0 Hardening (Shipped: 2026-04-14)

**Phases completed:** 2 phases, 2 plans, 2 tasks

**Key accomplishments:**

- Cross-platform DOCKER_HOST in Makefile and e2e workflow with kind v0.31.0 install and make target delegation

---

## v1.0 MVP (Shipped: 2026-04-14)

**Phases completed:** 4 phases, 9 plans, 16 tasks

**Key accomplishments:**

- Provider interface with 4 methods, RunResult/RunState types, and fully tested GrafanaCloudProvider using k6-cloud-openapi-client-go v6 API
- Two static plugin binaries with go-plugin handshake (metricprovider/step), Makefile build pipeline, and golangci-lint v2 with forbidigo stdout detection
- K6MetricProvider implementing all 7 RpcMetricProvider methods with async Run/Resume lifecycle, 4 metric types (thresholds/http_req_failed/http_req_duration/http_reqs), v5 aggregate HTTP client, and 91.7% test coverage
- Metric-plugin binary fully wired with GrafanaCloudProvider -> K6MetricProvider -> RpcMetricProviderPlugin, both binaries compile statically, 91.7% test coverage
- K6StepPlugin with fire-and-wait lifecycle: trigger/poll via Provider interface, timeout management, json.RawMessage state persistence, 89.1% test coverage
- Wired K6StepPlugin into step-plugin binary with GrafanaCloudProvider backend and RpcStepPlugin registration
- GoReleaser v2 multi-arch config (8 binaries, SHA256 checksums) with CI/release/e2e GitHub Actions workflows
- Kind cluster e2e test infrastructure with configurable mock k6 API server and 4 test scenarios validating full plugin binary loading path
- Three example YAML patterns (threshold-gate, error-rate+latency, full canary) plus README installation guide and CONTRIBUTING provider interface documentation

---
