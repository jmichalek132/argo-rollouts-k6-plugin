# Roadmap: argo-rollouts-k6-plugin

## Overview

Two Argo Rollouts plugin binaries (metric + step) that gate canary and blue-green deployments on Grafana Cloud k6 load test results. Extensible provider interface designed for in-cluster k6 Jobs and direct binary execution in future releases.

## Milestones

- ✅ **v1.0 MVP** — Phases 1-4 (shipped 2026-04-14)
- ✅ **v0.2.0 Hardening** — Phases 5-6 (shipped 2026-04-15)
- 🚧 **v0.3.0 In-Cluster Execution** — Phases 7-10 (in progress)

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-4) — SHIPPED 2026-04-14</summary>

- [x] Phase 1: Foundation & Provider (2/2 plans) — completed 2026-04-09
- [x] Phase 2: Metric Plugin (2/2 plans) — completed 2026-04-10
- [x] Phase 3: Step Plugin (2/2 plans) — completed 2026-04-10
- [x] Phase 4: Release & Examples (3/3 plans) — completed 2026-04-10

Full details: [milestones/v1.0-ROADMAP.md](milestones/v1.0-ROADMAP.md)

</details>

<details>
<summary>✅ v0.2.0 Hardening (Phases 5-6) — SHIPPED 2026-04-15</summary>

- [x] Phase 5: CI Pipeline Fix (1/1 plan) — completed 2026-04-15
- [x] Phase 6: Automated Dependency Management (1/1 plan) — completed 2026-04-15

Full details: [milestones/v0.2.0-ROADMAP.md](milestones/v0.2.0-ROADMAP.md)

</details>

### 🚧 v0.3.0 In-Cluster Execution (In Progress)

**Milestone Goal:** Enable k6 test execution inside the Kubernetes cluster via k6-operator, with ConfigMap script sourcing and metric extraction from runner pod logs.

- [ ] **Phase 7: Foundation & Kubernetes Client** - Provider routing, in-cluster k8s client, ConfigMap script sourcing
- [ ] **Phase 8: k6-operator Provider** - TestRun CRD lifecycle with distributed execution support
- [ ] **Phase 9: Metric Integration** - handleSummary JSON extraction from runner pod logs for metric plugin
- [ ] **Phase 10: Documentation & E2E** - RBAC examples, AnalysisTemplate YAML, kind cluster e2e suite

## Phase Details

### Phase 7: Foundation & Kubernetes Client
**Goal**: Plugin can route between execution backends and load k6 scripts from Kubernetes ConfigMaps
**Depends on**: Phase 6 (v0.2.0 complete)
**Requirements**: FOUND-01, FOUND-02, FOUND-03
**Success Criteria** (what must be TRUE):
  1. Plugin config with `provider: "k6-operator"` routes to the k6-operator backend; omitting `provider` or setting `provider: "grafana-cloud"` routes to the existing Grafana Cloud backend (backward compatible)
  2. Plugin creates a working Kubernetes client from in-cluster service account credentials during InitPlugin
  3. Plugin reads a k6 .js script body from a ConfigMap by name and key, and that script content is available to downstream providers
**Plans**: TBD

### Phase 8: k6-operator Provider
**Goal**: Plugin creates and manages k6-operator TestRun CRs for distributed in-cluster k6 execution
**Depends on**: Phase 7
**Requirements**: K6OP-01, K6OP-02, K6OP-03, K6OP-04, K6OP-05, K6OP-06, K6OP-07, K6OP-08
**Success Criteria** (what must be TRUE):
  1. Plugin creates a TestRun CR (k6.io/v1alpha1) and polls its stage field until it reaches a terminal state (finished/error)
  2. Plugin determines pass/fail by inspecting runner pod exit codes after TestRun completes (k6-operator issue #577 workaround)
  3. Plugin supports namespace targeting, parallelism, resource limits, custom runner image, and environment variable injection via plugin config fields
  4. Plugin deletes the TestRun CR when the rollout is aborted or terminated, stopping all running k6 pods
  5. Created TestRun CRs use consistent naming (`k6-<rollout>-<hash>`) and carry `app.kubernetes.io/managed-by` labels
**Plans**: TBD

### Phase 9: Metric Integration
**Goal**: Metric plugin extracts k6 result metrics from in-cluster test runs for AnalysisTemplate successCondition evaluation
**Depends on**: Phase 8
**Requirements**: METR-01, METR-02
**Success Criteria** (what must be TRUE):
  1. Metric plugin extracts p95, error rate, throughput, and threshold results from k6 handleSummary() JSON found in runner pod logs
  2. Metric plugin works with k6-operator provider using the same successCondition expressions as the Grafana Cloud provider (users switch providers without rewriting AnalysisTemplates)
**Plans**: TBD

### Phase 10: Documentation & E2E
**Goal**: Users have working RBAC examples, complete AnalysisTemplate YAML, and the full k6-operator integration is validated end-to-end on a kind cluster
**Depends on**: Phase 9
**Requirements**: DOCS-01, DOCS-02, TEST-01
**Success Criteria** (what must be TRUE):
  1. RBAC example ClusterRole grants all permissions needed for k6-operator TestRun CRDs, pods, pods/log, and configmaps
  2. Example AnalysisTemplate and Rollout YAML for k6-operator provider works out of the box when applied to a cluster with k6-operator installed
  3. e2e test suite on kind cluster creates a TestRun CR, waits for completion, and validates result extraction against a mock target service
**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 7 → 8 → 9 → 10

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Foundation & Provider | v1.0 | 2/2 | Complete | 2026-04-09 |
| 2. Metric Plugin | v1.0 | 2/2 | Complete | 2026-04-10 |
| 3. Step Plugin | v1.0 | 2/2 | Complete | 2026-04-10 |
| 4. Release & Examples | v1.0 | 3/3 | Complete | 2026-04-10 |
| 5. CI Pipeline Fix | v0.2.0 | 1/1 | Complete | 2026-04-15 |
| 6. Automated Dependency Management | v0.2.0 | 1/1 | Complete | 2026-04-15 |
| 7. Foundation & Kubernetes Client | v0.3.0 | 0/0 | Not started | - |
| 8. k6-operator Provider | v0.3.0 | 0/0 | Not started | - |
| 9. Metric Integration | v0.3.0 | 0/0 | Not started | - |
| 10. Documentation & E2E | v0.3.0 | 0/0 | Not started | - |
