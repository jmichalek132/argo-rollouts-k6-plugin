# Roadmap: argo-rollouts-k6-plugin

## Overview

Two Argo Rollouts plugin binaries (metric + step) that gate canary and blue-green deployments on Grafana Cloud k6 load test results OR in-cluster k6-operator runs. Extensible provider interface designed for future execution backends (direct binary, Kubernetes Job).

## Milestones

- ✅ **v1.0 MVP** — Phases 1-4 (shipped 2026-04-14)
- ✅ **v0.2.0 Hardening** — Phases 5-6 (shipped 2026-04-15)
- ✅ **v0.3.0 In-Cluster Execution** — Phases 7-10 + decimals 08.1/08.2/08.3 (shipped 2026-04-20)

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

<details>
<summary>✅ v0.3.0 In-Cluster Execution (Phases 7-10 + 08.1/08.2/08.3) — SHIPPED 2026-04-20</summary>

- [x] Phase 7: Foundation & Kubernetes Client (2/2 plans) — completed 2026-04-16
- [x] Phase 8: k6-operator Provider (2/2 plans) — completed 2026-04-16
- [x] Phase 08.1: Wire AnalysisRun/Rollout metadata through plugin layers (3/3 plans, inserted) — completed 2026-04-17
- [x] Phase 08.2: Default cfg.Parallelism=1 when unset (1/1 plan, inserted) — completed 2026-04-19
- [x] Phase 08.3: Remove spec.Cleanup=post (1/1 plan, inserted retroactively) — completed 2026-04-20
- [x] Phase 9: Metric Integration (2/2 plans) — completed 2026-04-16
- [x] Phase 10: Documentation & E2E (2/2 plans) — completed 2026-04-16

Full details: [milestones/v0.3.0-ROADMAP.md](milestones/v0.3.0-ROADMAP.md)

</details>

## Next Milestone

No milestone currently in progress. Next milestone not yet defined.

Run `/gsd-new-milestone` to start planning the next milestone (likely candidates: success-path TestRun/pod cleanup via `GarbageCollect`, extended e2e coverage, k6 Job provider).

## Progress

| Milestone | Phases | Plans | Status | Shipped |
|-----------|--------|-------|--------|---------|
| v1.0 MVP | 1-4 | 9/9 | Complete | 2026-04-14 |
| v0.2.0 Hardening | 5-6 | 2/2 | Complete | 2026-04-15 |
| v0.3.0 In-Cluster Execution | 7-10 + 08.1/08.2/08.3 | 13/13 | Complete | 2026-04-20 |
