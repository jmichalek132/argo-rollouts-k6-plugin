# argo-rollouts-k6-plugin

## What This Is

An open-source Argo Rollouts plugin written in Go that integrates k6 load testing as analysis gates in canary and blue-green deployments. It ships as both a **metric plugin** (polls k6 metrics on interval for AnalysisTemplate threshold evaluation) and a **step plugin** (one-shot: trigger a k6 run, wait for completion, return pass/fail). Initially targets Grafana Cloud k6 as the execution backend, with an extensible provider interface designed for in-cluster k6 Jobs and direct binary execution in future releases.

## Core Value

Rollouts automatically pass or roll back based on real load test results — no manual gates, no guesswork.

## Current State

Shipped **v1.0** on 2026-04-14. Two fully functional plugin binaries with Grafana Cloud k6 backend, GoReleaser multi-arch CI/CD, e2e test suite, three example patterns, and community-ready documentation.

- **Go LOC:** ~23,800 across 95 files
- **Test coverage:** 91.7% (metric), 89.1% (step)
- **Binaries:** 8 platform variants (linux/darwin x amd64/arm64) via GoReleaser
- **e2e:** 4 mock scenarios on kind cluster

## Current Milestone: v0.2.0 Hardening

**Goal:** Fix CI gaps from v1.0 and set up automated dependency management.

**Target features:**
- Fix e2e GitHub Actions workflow: add `kind` install step and `-timeout=15m` flag
- Set up Renovate bot for automated Go module and GitHub Actions dependency updates

## Requirements

### Validated

- ✓ Extensible provider interface — v1.0 (Phase 1)
- ✓ Metric plugin with 4 metric types (thresholds, http_req_failed, http_req_duration, http_reqs) — v1.0 (Phase 2)
- ✓ Step plugin with trigger/poll/stop lifecycle and graceful termination — v1.0 (Phase 3)
- ✓ Test script sourcing via Grafana Cloud k6 test ID — v1.0 (Phase 1-3)
- ✓ Example AnalysisTemplates for 3 common use cases — v1.0 (Phase 4)
- ✓ e2e test suite on kind cluster with mock k6 API — v1.0 (Phase 4)
- ✓ GoReleaser multi-arch release pipeline with SHA256 checksums — v1.0 (Phase 4)
- ✓ README, CONTRIBUTING, and community documentation — v1.0 (Phase 4)

### Active

- [ ] Test script sourcing (v2): k6 .js script stored in a Kubernetes ConfigMap
- [ ] Step plugin secret handling: step plugin config has no secretKeyRef support; API tokens visible in Rollout spec and dashboard UI — upstream Argo Rollouts limitation
- [ ] In-cluster k6 Job execution via KubernetesJobProvider
- [ ] Direct k6 binary execution via LocalBinaryProvider
- [ ] Custom k6 metric support (user-defined Counter/Gauge/Rate/Trend)

### Out of Scope

- Test script sourcing from URL or git ref — network reliability concerns, git auth complexity
- Grafana dashboards or alerting configuration — separate concern
- Support for non-Grafana k6 execution backends (k6 Cloud legacy API) — different auth and endpoint structure
- Helm chart or Kubernetes operator — plugins are binaries registered via ConfigMap, no operator needed
- Real-time streaming metrics — k6 Cloud evaluates thresholds every 60s; polling is sufficient
- VUs/duration override in step plugin — creates divergence from k6 Cloud test definition

## Context

- Argo Rollouts analysis plugins implement a gRPC-based interface; the controller loads the plugin binary from a ConfigMap-referenced URL and communicates via RPC
- k6 is a Go-native load testing tool; the Grafana Cloud k6 API supports triggering test runs by test ID and polling run status/metrics
- Both plugin types (metric and step) have distinct gRPC interfaces in Argo Rollouts and must be implemented separately
- The metric plugin is more composable — users can combine k6 metrics with Prometheus/Datadog metrics in a single AnalysisTemplate
- v1.0 shipped with 4 phases in 5 days; all 30 requirements satisfied, cross-phase integration verified
- Known tech debt: e2e CI workflow needs `kind` install step and `-timeout=15m` flag

## Constraints

- **Tech stack**: Go — matches Argo Rollouts ecosystem, k6 is also Go-native
- **Plugin interface**: Must implement the Argo Rollouts plugin gRPC interface exactly — breaking changes to the interface are not permitted
- **Distribution**: Binary must be statically linked and published to GitHub Releases with SHA256 checksum for controller verification
- **Dependencies**: Grafana Cloud k6 API credentials (token + org/project ID) passed via Kubernetes Secret, referenced in AnalysisTemplate

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Grafana Cloud k6 first | Best API for triggering runs and querying structured metrics; most users already have it | ✓ Validated — shipped in v1.0 |
| Provider abstraction from day 1 | Avoids breaking API when adding in-cluster or binary backends | ✓ Validated — `internal/provider.Provider` interface |
| Both metric + step plugins | Metric plugin for threshold-based gates; step plugin for fire-and-wait use case | ✓ Validated — both shipped and e2e tested |
| Two binaries, one Go module | Controller requires different MagicCookieValue per plugin type | ✓ Good — clean separation, no runtime dispatch |
| Stateless provider pattern | Credentials via PluginConfig per call, client created per call — concurrent safe by design | ✓ Good — enabled 91.7% coverage, race-clean |
| slog over logrus | Zero external deps, structured JSON to stderr, available since Go 1.21 | ✓ Good — simplified dependencies |
| v5 aggregate API via hand-rolled HTTP | k6-cloud-openapi-client-go v6 doesn't expose aggregate endpoints | ⚠️ Revisit when v7+ client ships |
| ConfigMap script sourcing in v2 | Grafana Cloud test ID covers immediate need | — Deferred as planned |
| No custom metrics/tracing | Plugin is subprocess, Argo Rollouts covers outcomes, go-plugin has no OTel support | ✓ Good — structured slog with runId correlation is sufficient |
| Canary example uses independent k6 runs | No run ID handoff between step and metric plugins | ✓ Good — simpler, avoids coupling |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition:**
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone:**
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-04-15 after v1.0 milestone*
