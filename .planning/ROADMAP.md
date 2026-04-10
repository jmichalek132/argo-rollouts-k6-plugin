# Roadmap: argo-rollouts-k6-plugin

## Overview

This roadmap delivers two Argo Rollouts plugin binaries (metric + step) that gate canary and blue-green deployments on Grafana Cloud k6 load test results. The four phases follow the natural dependency chain: provider interface and Grafana Cloud implementation first (testable in isolation), then metric plugin (the harder async pattern), then step plugin (reuses provider), then release infrastructure with e2e tests and examples. Each phase delivers a coherent, independently verifiable capability.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Foundation & Provider** - Go module scaffolding, build pipeline, provider interface with Grafana Cloud k6 implementation
- [ ] **Phase 2: Metric Plugin** - Full RpcMetricProvider implementation with all k6 metrics and unit tests
- [ ] **Phase 3: Step Plugin** - Full RpcStep implementation with trigger/poll/stop lifecycle and graceful termination
- [ ] **Phase 4: Release & Examples** - e2e tests, example manifests, documentation, goreleaser CI/CD pipeline

## Phase Details

### Phase 1: Foundation & Provider
**Goal**: A working Go module with two binary entrypoints that compile, a provider interface with a fully tested Grafana Cloud k6 implementation, and a build pipeline that produces static binaries with correct go-plugin handshake conventions
**Depends on**: Nothing (first phase)
**Requirements**: PLUG-04, PROV-01, PROV-02, PROV-03, PROV-04, DIST-01, DIST-04
**Success Criteria** (what must be TRUE):
  1. `go build ./cmd/metric-plugin` and `go build ./cmd/step-plugin` both produce static binaries with CGO_ENABLED=0
  2. Both binaries write all output to stderr only -- running them produces no stdout before go-plugin handshake
  3. The Grafana Cloud provider can authenticate, trigger a test run by ID, poll run status to terminal state, and stop a running test -- verified by unit tests against a mock HTTP server
  4. The Provider interface is defined in `internal/provider/provider.go` with TriggerRun, GetRunResult, StopRun, and Name methods
**Plans**: 2 plans

Plans:
- [x] 01-01-PLAN.md -- Go module, Provider interface, types, and fully tested Grafana Cloud k6 provider implementation
- [x] 01-02-PLAN.md -- Binary stubs with go-plugin handshake, Makefile build pipeline, golangci-lint v2 config

### Phase 2: Metric Plugin
**Goal**: The metric plugin binary implements the full RpcMetricProvider interface, returning k6 threshold pass/fail, HTTP error rate, latency percentiles, and throughput as AnalysisRun measurement values
**Depends on**: Phase 1
**Requirements**: PLUG-01, METR-01, METR-02, METR-03, METR-04, METR-05, TEST-01, TEST-03
**Success Criteria** (what must be TRUE):
  1. The metric plugin handles the full Run/Resume async lifecycle -- Run triggers a k6 test and stores runId in Measurement.Metadata, Resume polls until terminal state and returns the requested metric value
  2. All four metric types return correct values: `thresholds` (boolean 0/1), `http_req_failed` (float 0.0-1.0), `http_req_duration` with p50/p95/p99 aggregation (milliseconds), `http_reqs` rate (req/s)
  3. Measurement.Metadata contains k6 Cloud test run URL and status for debugging via `kubectl get analysisrun -o yaml`
  4. Unit tests cover config parsing, all metric calculations, Run/Resume state management, and error handling at >=80% coverage on internal packages
  5. Running `go test -race ./internal/...` passes -- concurrent AnalysisRun polls do not cross-contaminate state
**Plans**: 2 plans

Plans:
- [x] 02-01-PLAN.md -- K6MetricProvider implementation with v5 aggregate metrics client and full TDD unit test suite
- [x] 02-02-PLAN.md -- Wire K6MetricProvider into metric-plugin binary and verify full build pipeline

### Phase 3: Step Plugin
**Goal**: The step plugin binary triggers a Grafana Cloud k6 test run, polls until completion, and returns pass/fail based on k6 threshold results -- with graceful termination that stops orphaned cloud test runs
**Depends on**: Phase 2
**Requirements**: PLUG-02, STEP-01, STEP-02, STEP-03, STEP-04, STEP-05
**Success Criteria** (what must be TRUE):
  1. The step plugin accepts testId, apiToken (secretRef), stackId (secretRef), and timeout in config, triggers a k6 Cloud test run, and returns PhaseRunning with RequeueAfter on first Run call
  2. Subsequent Run calls read runId from RpcStepContext.Status, poll run status, and return PhaseSuccessful (thresholds passed) or PhaseFailed (thresholds failed, timed out, or errored)
  3. The testRunId is available in RpcStepResult.Status so downstream metric plugins can consume it via AnalysisTemplate args
  4. Calling Terminate or Abort stops the active k6 Cloud test run via provider.StopRun -- no orphaned Grafana Cloud test runs
**Plans**: 2 plans

Plans:
- [x] 03-01-PLAN.md -- K6StepPlugin TDD implementation with full lifecycle tests and >=80% coverage
- [x] 03-02-PLAN.md -- Wire K6StepPlugin into step-plugin binary and verify full build pipeline

### Phase 4: Release & Examples
**Goal**: The project is ready for community consumption -- e2e tests validate the full binary-loading path in a real cluster, example manifests demonstrate common workflows, and tagged releases produce multi-arch binaries with checksums
**Depends on**: Phase 3
**Requirements**: PLUG-03, DIST-02, DIST-03, EXAM-01, EXAM-02, EXAM-03, EXAM-04, EXAM-05, TEST-02
**Success Criteria** (what must be TRUE):
  1. e2e tests on a kind cluster verify the complete lifecycle: controller loads plugin binaries, AnalysisRun completes with expected phase, Rollout progresses or rolls back based on test results
  2. Example AnalysisTemplates exist for threshold-only gate, error rate + p95 latency combined, and a Rollout showing step plugin trigger + metric analysis gate in a canary workflow
  3. README covers installation (ConfigMap setup, binary download), credential management (Secret YAML), and a quick-start walkthrough
  4. `goreleaser` produces linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 binaries with SHA256 checksums; GitHub Actions CI runs lint, test, build on PR and release on tag
  5. Both binaries are registered in argo-rollouts-config ConfigMap with name, GitHub Releases URL, and SHA256 checksum
**Plans**: TBD

Plans:
- [ ] 04-01: TBD
- [ ] 04-02: TBD
- [ ] 04-03: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3 -> 4

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation & Provider | 2/2 | Complete | 2026-04-09 |
| 2. Metric Plugin | 2/2 | Complete | 2026-04-10 |
| 3. Step Plugin | 0/2 | Planning complete | - |
| 4. Release & Examples | 0/3 | Not started | - |
