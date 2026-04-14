# argo-rollouts-k6-plugin

## What This Is

An open-source Argo Rollouts plugin written in Go that integrates k6 load testing as analysis gates in canary and blue-green deployments. It ships as both a **metric plugin** (polls k6 metrics on interval for AnalysisTemplate threshold evaluation) and a **step plugin** (one-shot: trigger a k6 run, wait for completion, return pass/fail). Initially targets Grafana Cloud k6 as the execution backend, with an extensible provider interface designed for in-cluster k6 Jobs and direct binary execution in future releases.

## Core Value

Rollouts automatically pass or roll back based on real load test results — no manual gates, no guesswork.

## Requirements

### Validated

- [x] Extensible provider interface: abstraction layer that allows adding in-cluster k6 Job execution and direct k6 binary invocation without breaking existing users — *Validated in Phase 1: foundation-provider*
- [x] Metric plugin: polls Grafana Cloud k6 API on each AnalysisRun interval and returns configurable metric values (error rate, p95/p99 latency, k6 threshold result) — *Validated in Phase 2: metric-plugin*
- [x] Step plugin: triggers a Grafana Cloud k6 test run by test ID, waits for completion, returns pass/fail — *Validated in Phase 3: step-plugin*

### Active

- [ ] Test script sourcing (v2): k6 .js script stored in a Kubernetes ConfigMap — deferred to v2
- [ ] Step plugin secret handling: step plugin config has no secretKeyRef support; API tokens visible in Rollout spec and dashboard UI — upstream Argo Rollouts limitation

### Completed in v0.1.0

- [x] Test script sourcing (v1): reference an existing Grafana Cloud k6 test by ID — *Validated in Phase 1-3*
- [x] Configurable metrics with sane defaults: HTTP error rate, response time percentiles (p50/p95/p99), k6 threshold pass/fail, HTTP throughput — *Validated in Phase 2: metric-plugin*
- [x] Example AnalysisTemplates for common use cases — *Validated in Phase 4: three example patterns*
- [x] Integration tests: e2e test suite running against a kind cluster — *Validated in Phase 4: 4 mock scenarios + 4 live scenarios*
- [x] Release binary packaged following Argo Rollouts plugin conventions (checksum, GitHub Releases) — *Validated in Phase 4: goreleaser + GitHub Actions*
- [x] README and setup guide for community consumption — *Validated in Phase 4: README + CONTRIBUTING*

### Out of Scope

- Test script sourcing from URL or git ref — deferred to v3+
- In-cluster k6 Job execution — provider interface built but deferred to v2
- Direct k6 binary execution — provider interface built but deferred to v2
- Grafana dashboards or alerting configuration
- Support for non-Grafana k6 execution backends (k6 Cloud legacy API)
- Helm chart or Kubernetes operator for plugin deployment

## Context

- Argo Rollouts analysis plugins implement a gRPC-based interface; the controller loads the plugin binary from a ConfigMap-referenced URL and communicates via RPC
- k6 is a Go-native load testing tool; the Grafana Cloud k6 API supports triggering test runs by test ID and polling run status/metrics
- Plugin conventions follow the sample at `github.com/argoproj/argo-rollouts/tree/master/test/cmd/metrics-plugin-sample`
- Both plugin types (metric and step) have distinct gRPC interfaces in Argo Rollouts and must be implemented separately
- The metric plugin is more composable — users can combine k6 metrics with Prometheus/Datadog metrics in a single AnalysisTemplate

## Constraints

- **Tech stack**: Go — matches Argo Rollouts ecosystem, k6 is also Go-native
- **Plugin interface**: Must implement the Argo Rollouts plugin gRPC interface exactly — breaking changes to the interface are not permitted
- **Distribution**: Binary must be statically linked and published to GitHub Releases with SHA256 checksum for controller verification
- **Dependencies**: Grafana Cloud k6 API credentials (token + org/project ID) passed via Kubernetes Secret, referenced in AnalysisTemplate

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Grafana Cloud k6 first | Best API for triggering runs and querying structured metrics; most users already have it | Validated — shipped in v0.1.0 |
| Provider abstraction from day 1 | Avoids breaking API when adding in-cluster or binary backends | Validated — `internal/provider.Provider` interface in Phase 1 |
| Both metric + step plugins | Metric plugin for threshold-based gates; step plugin for fire-and-wait use case — different workflows need both | Validated — both plugins shipped and e2e tested |
| ConfigMap script sourcing in v2 | Grafana Cloud test ID covers immediate need; ConfigMap is next natural step for users without Grafana Cloud | Deferred to v2 as planned |
| No custom metrics/tracing | Plugin is a subprocess with no scrape endpoint; Argo Rollouts already covers outcomes; go-plugin has no OTel support | Decided in code review — structured slog with runId correlation is sufficient |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-04-14 — v0.1.0 released: all 4 phases complete, two code review rounds, live and mock e2e verified*
