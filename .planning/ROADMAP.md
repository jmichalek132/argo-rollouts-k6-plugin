# Roadmap: argo-rollouts-k6-plugin

## Overview

Two Argo Rollouts plugin binaries (metric + step) that gate canary and blue-green deployments on Grafana Cloud k6 load test results. Extensible provider interface designed for in-cluster k6 Jobs and direct binary execution in future releases.

## Milestones

- ✅ **v1.0 MVP** — Phases 1-4 (shipped 2026-04-14)
- 🚧 **v0.2.0 Hardening** — Phases 5-6 (in progress)

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-4) — SHIPPED 2026-04-14</summary>

- [x] Phase 1: Foundation & Provider (2/2 plans) — completed 2026-04-09
- [x] Phase 2: Metric Plugin (2/2 plans) — completed 2026-04-10
- [x] Phase 3: Step Plugin (2/2 plans) — completed 2026-04-10
- [x] Phase 4: Release & Examples (3/3 plans) — completed 2026-04-10

Full details: [milestones/v1.0-ROADMAP.md](milestones/v1.0-ROADMAP.md)

</details>

### 🚧 v0.2.0 Hardening (In Progress)

**Milestone Goal:** Fix CI gaps from v1.0 and set up automated dependency management.

- [ ] **Phase 5: CI Pipeline Fix** - Fix e2e workflow to install kind and use correct timeout
- [ ] **Phase 6: Automated Dependency Management** - Configure Renovate for Go modules and GitHub Actions

## Phase Details

### Phase 5: CI Pipeline Fix
**Goal**: e2e tests run successfully in GitHub Actions without manual intervention
**Depends on**: Nothing (independent of Phase 6)
**Requirements**: CI-01, CI-02
**Success Criteria** (what must be TRUE):
  1. e2e GitHub Actions workflow installs the `kind` binary before test execution
  2. e2e workflow passes the `-timeout=15m` flag to `go test`, matching the Makefile `test-e2e` target
  3. A push to main triggers the e2e workflow and it completes without timeout or missing-binary errors
**Plans:** 1 plan
Plans:
- [ ] 05-01-PLAN.md — Fix Makefile DOCKER_HOST and e2e workflow (kind install + make targets)

### Phase 6: Automated Dependency Management
**Goal**: Go module and GitHub Actions dependencies receive automated update PRs via Renovate
**Depends on**: Nothing (independent of Phase 5)
**Requirements**: DEPS-01, DEPS-02
**Success Criteria** (what must be TRUE):
  1. A `renovate.json` config file exists in the repository root with Go module update rules
  2. Renovate is configured to update GitHub Actions dependencies (actions/checkout, actions/setup-go, goreleaser, golangci-lint)
  3. After merging, Renovate bot opens its onboarding PR or begins creating dependency update PRs
**Plans**: TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Foundation & Provider | v1.0 | 2/2 | Complete | 2026-04-09 |
| 2. Metric Plugin | v1.0 | 2/2 | Complete | 2026-04-10 |
| 3. Step Plugin | v1.0 | 2/2 | Complete | 2026-04-10 |
| 4. Release & Examples | v1.0 | 3/3 | Complete | 2026-04-10 |
| 5. CI Pipeline Fix | v0.2.0 | 0/1 | Planned | - |
| 6. Automated Dependency Management | v0.2.0 | 0/0 | Not started | - |
