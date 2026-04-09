# Requirements: argo-rollouts-k6-plugin

**Defined:** 2026-04-09
**Core Value:** Rollouts automatically pass or roll back based on real load test results -- no manual gates, no guesswork.

## v1 Requirements

### Plugin Infrastructure

- [ ] **PLUG-01**: Metric plugin binary implements full `RpcMetricProvider` interface (`InitPlugin`, `Run`, `Resume`, `Terminate`, `GarbageCollect`, `Type`, `GetMetadata`)
- [ ] **PLUG-02**: Step plugin binary implements full `RpcStep` interface (`Run`, `Terminate`, `Abort`, `Type`)
- [ ] **PLUG-03**: Both binaries register via `argo-rollouts-config` ConfigMap with name, location (GitHub Releases URL), and SHA256 checksum
- [x] **PLUG-04**: Internal provider abstraction interface (`Provider`) with unified 4-method design: `Name() string`, `TriggerRun(ctx, cfg)`, `GetRunResult(ctx, cfg, runID)`, `StopRun(ctx, cfg, runID)` — `GetRunResult` returns all metrics + state in one struct; Grafana Cloud k6 is the only v1 implementation

### Grafana Cloud Provider

- [x] **PROV-01**: Authenticate to Grafana Cloud k6 API using API token + stack ID, sourced from Kubernetes Secret via `secretKeyRef` in AnalysisTemplate args
- [x] **PROV-02**: Trigger a k6 test run by test ID (`POST /loadtests/v2/tests/{id}/start-testrun`) and return the resulting test run ID
- [x] **PROV-03**: Poll test run status (`GET /loadtests/v2/test_runs/{id}`) to determine running vs terminal state
- [x] **PROV-04**: Stop a running k6 test run (`POST /loadtests/v2/test_runs/{id}/stop`) when requested

### Metrics (Metric Plugin)

- [ ] **METR-01**: Return k6 threshold pass/fail as a boolean (`metric: thresholds`) -- primary pass/fail signal using k6's native threshold definitions
- [ ] **METR-02**: Return HTTP error rate as a float 0.0-1.0 (`metric: http_req_failed`) -- fraction of failed requests
- [ ] **METR-03**: Return HTTP latency percentiles in milliseconds (`metric: http_req_duration`, `aggregation: p50|p95|p99`)
- [ ] **METR-04**: Return HTTP throughput as requests/second (`metric: http_reqs`, `aggregation: rate`)
- [ ] **METR-05**: Return k6 Cloud test run URL and status in `Measurement.Metadata` for `kubectl get analysisrun -o yaml` debugging

### Step Plugin

- [ ] **STEP-01**: Accept `testId` (k6 Cloud test definition ID), `apiToken` (secretRef), `stackId` (secretRef), and `timeout` in step config
- [ ] **STEP-02**: Trigger k6 Cloud test run, return `PhaseRunning` with `RequeueAfter`, poll on subsequent `Run` calls until terminal state
- [ ] **STEP-03**: Return `testRunId` in `RpcStepResult.Status` so downstream metric plugin can consume it via AnalysisTemplate args
- [ ] **STEP-04**: Return `PhaseSuccessful` if k6 thresholds passed, `PhaseFailed` if thresholds failed or test errored
- [ ] **STEP-05**: Call `StopRun` on the active test run when `Terminate` or `Abort` is called -- no orphaned Grafana Cloud test runs

### Build & Distribution

- [x] **DIST-01**: Two statically linked Go binaries: `metric-plugin` and `step-plugin`, both CGO-disabled (`CGO_ENABLED=0`)
- [ ] **DIST-02**: goreleaser configuration produces multi-arch binaries (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) with SHA256 checksums
- [ ] **DIST-03**: GitHub Actions CI: lint (`golangci-lint`), test (`go test -race`), build on PR and push; release on tag
- [x] **DIST-04**: All plugin output to stderr only -- stdout reserved for hashicorp/go-plugin handshake protocol

### Examples & Documentation

- [ ] **EXAM-01**: Example `AnalysisTemplate` for threshold-only gate (simplest pattern -- metric: thresholds)
- [ ] **EXAM-02**: Example `AnalysisTemplate` for HTTP error rate + p95 latency combined
- [ ] **EXAM-03**: Example `Rollout` showing step plugin (trigger) + metric analysis (gate) combined canary workflow
- [ ] **EXAM-04**: README with: installation (ConfigMap setup, binary download), credential management (Secret YAML), quick-start walkthrough
- [ ] **EXAM-05**: CLAUDE.md or CONTRIBUTING.md for contributors

### Testing

- [ ] **TEST-01**: Unit tests for config parsing, metric calculation, Run/Resume state management, error handling (>=80% coverage on internal packages)
- [ ] **TEST-02**: e2e integration tests against a kind cluster with real Argo Rollouts controller and mocked Grafana Cloud k6 API (mock server or VCR)
- [ ] **TEST-03**: Concurrent AnalysisRun test -- verify multiple simultaneous metric plugin polls don't cross-contaminate state (`go test -race`)

## v2 Requirements

### Provider Extensions

- **PROV2-01**: In-cluster k6 Job execution -- `KubernetesJobProvider` implementing the `Provider` interface, running k6 as a Kubernetes Job
- **PROV2-02**: ConfigMap-based k6 script sourcing -- accept `scriptConfigMap` as an alternative to `testId`, upload script to k6 Cloud for execution
- **PROV2-03**: Direct k6 binary execution -- `LocalBinaryProvider` running k6 as a subprocess

### Metric Enhancements

- **METR2-01**: Custom k6 metric support -- `metric: custom` with `metricName` and `aggregation` for user-defined Counter/Gauge/Rate/Trend metrics
- **METR2-02**: Structured multi-metric result object -- single metric query returns map with `httpReqDuration.p95`, `httpReqFailedRate`, `thresholdsPassed`, etc. for compound `successCondition` expressions
- **METR2-03**: Automatic test run ID resolution -- accept `testId` + `latestRun: true` to find most recent run without explicit `testRunId`

### Step Enhancements

- **STEP2-01**: Combined trigger+monitor workflow -- step plugin returns `testRunId` that metric plugin reads via `{{steps.plugin.status.testRunId}}` template variable (requires Argo Rollouts expression engine validation)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Helm chart or Kubernetes operator | Argo Rollouts plugins are binaries registered via ConfigMap -- no operator needed; adds maintenance burden without clear value |
| Grafana dashboard generation | Separate concern; users already have Grafana Cloud dashboards for their k6 tests |
| VUs/duration override in step plugin | Creates divergence between k6 Cloud test definition and what runs; confusing UX |
| Real-time streaming metrics | k6 Cloud evaluates thresholds every 60s; polling at sensible intervals is sufficient |
| Multi-provider in single binary | Separate binaries have cleaner registration, upgrade, and trust model |
| Script sourcing from URL/git | Network reliability concerns, git auth complexity -- defer to v3+ |
| k6 Cloud legacy API (non-Grafana) | Grafana Cloud k6 is the target; legacy Cloud API has different auth and endpoint structure |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PLUG-01 | Phase 2 | Pending |
| PLUG-02 | Phase 3 | Pending |
| PLUG-03 | Phase 4 | Pending |
| PLUG-04 | Phase 1 | Complete |
| PROV-01 | Phase 1 | Complete |
| PROV-02 | Phase 1 | Complete |
| PROV-03 | Phase 1 | Complete |
| PROV-04 | Phase 1 | Complete |
| METR-01 | Phase 2 | Pending |
| METR-02 | Phase 2 | Pending |
| METR-03 | Phase 2 | Pending |
| METR-04 | Phase 2 | Pending |
| METR-05 | Phase 2 | Pending |
| STEP-01 | Phase 3 | Pending |
| STEP-02 | Phase 3 | Pending |
| STEP-03 | Phase 3 | Pending |
| STEP-04 | Phase 3 | Pending |
| STEP-05 | Phase 3 | Pending |
| DIST-01 | Phase 1 | Complete |
| DIST-02 | Phase 4 | Pending |
| DIST-03 | Phase 4 | Pending |
| DIST-04 | Phase 1 | Complete |
| EXAM-01 | Phase 4 | Pending |
| EXAM-02 | Phase 4 | Pending |
| EXAM-03 | Phase 4 | Pending |
| EXAM-04 | Phase 4 | Pending |
| EXAM-05 | Phase 4 | Pending |
| TEST-01 | Phase 2 | Pending |
| TEST-02 | Phase 4 | Pending |
| TEST-03 | Phase 2 | Pending |

**Coverage:**
- v1 requirements: 30 total
- Mapped to phases: 30
- Unmapped: 0

---
*Requirements defined: 2026-04-09*
*Last updated: 2026-04-09 after roadmap creation*
