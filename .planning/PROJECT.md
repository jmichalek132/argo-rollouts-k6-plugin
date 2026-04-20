# argo-rollouts-k6-plugin

## What This Is

An open-source Argo Rollouts plugin written in Go that integrates k6 load testing as analysis gates in canary and blue-green deployments. It ships as both a **metric plugin** (polls k6 metrics on interval for AnalysisTemplate threshold evaluation) and a **step plugin** (one-shot: trigger a k6 run, wait for completion, return pass/fail). Initially targets Grafana Cloud k6 as the execution backend, with an extensible provider interface designed for in-cluster k6 Jobs and direct binary execution in future releases.

## Core Value

Rollouts automatically pass or roll back based on real load test results — no manual gates, no guesswork.

## Current State

Shipped **v0.3.0 In-Cluster Execution** on 2026-04-20. k6-operator TestRun CRDs are now a fully supported execution backend alongside Grafana Cloud k6 — users get in-cluster distributed load tests gating canary/blue-green rollouts, with metric extraction from runner pod logs.

- **Go LOC:** 8,913 across `internal/`, `e2e/`, `cmd/`
- **Tests:** 237+ unit tests (5 packages), 6/6 e2e tests on kind cluster (PASS in 261s)
- **Binaries:** 8 platform variants (linux/darwin × amd64/arm64) via GoReleaser
- **Dependencies:** Renovate bot configured; `client-go` v0.34.1 promoted to direct dep; k6-operator v1.3.2 added as a dev/test dependency (not an import — plugin uses dynamic client)
- **Known deferred:** success-path TestRun/pod cleanup (GarbageCollect hook) and combined step+metric+AR-deletion e2e — captured in Active below.

Three decimal phases (08.1, 08.2, 08.3) were inserted during milestone execution to fix bugs surfaced by the e2e suite itself — metadata discarding, parallelism=0→paused, and TestRun-GC-before-Resume race. Each fix unblocked the next layer; final e2e after 08.3 was clean.

## Next Milestone

No milestone in progress. Candidates for next (in priority order): success-path TestRun cleanup (GarbageCollect), extended e2e coverage (combined canary + AR deletion), Kubernetes Job provider as a lighter-weight alternative to k6-operator for simple use cases.

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
- ✓ e2e CI pipeline with kind install and correct timeout — v0.2.0 (Phase 5)
- ✓ Renovate bot for automated Go module and GitHub Actions dependency updates — v0.2.0 (Phase 6)
- ✓ Provider routing between execution backends (grafana-cloud, k6-operator) — v0.3.0 (Phase 7)
- ✓ In-cluster Kubernetes client with lazy init and ConfigMap script reading — v0.3.0 (Phase 7)
- ✓ ConfigMap script sourcing: k6 .js script stored in a Kubernetes ConfigMap, referenced by name/key in plugin config — v0.3.0 (Phase 7)
- ✓ k6-operator CRD support: TestRun CR lifecycle (Trigger/Get/Stop) via dynamic client, namespace targeting, parallelism, resource limits, custom runner image, env var injection — v0.3.0 (Phase 8)
- ✓ AnalysisRun/Rollout metadata wiring through plugin layers with AR > Rollout > none owner-ref precedence — v0.3.0 (Phase 08.1)
- ✓ `cfg.Parallelism=1` default in `buildTestRun` when unset (k6-operator treats 0 as paused) — v0.3.0 (Phase 08.2)
- ✓ TestRun CR persists past terminal stage so plugin can read status (removed `spec.Cleanup=post`) — v0.3.0 (Phase 08.3)
- ✓ k6-operator metric extraction: handleSummary JSON parsing from runner pod logs, metric parity with Grafana Cloud provider — v0.3.0 (Phase 9)
- ✓ RBAC example + AnalysisTemplate/Rollout examples + kind-cluster e2e suite — v0.3.0 (Phase 10)

### Active

No active requirements. Next milestone not yet defined.

### Deferred

- [ ] Success-path TestRun/pod cleanup: implement `GarbageCollect` on the metric plugin and a symmetric post-terminal hook on the step plugin so completed TestRun CRs and runner pods don't leak. `StopRun` already handles cancellation (Terminate/Abort). Deferred from v0.3.0 Phase 08.3 when `spec.Cleanup=post` was removed.
- [ ] Extended e2e coverage: combined step+metric canary with mid-run AnalysisRun deletion to exercise D-07 owner-ref precedence under real Kubernetes GC cascade. Unit-tested; not e2e-tested.
- [ ] In-cluster k6 Job execution: KubernetesJobProvider creates `batch/v1` Jobs with k6 container — lighter-weight alternative to k6-operator for single-pod runs.
- [ ] Local binary execution research: investigate running k6 as a subprocess of the plugin process — flagged as risky in v0.3.0 (grafana/k6#3744 re: stdout protocol corruption).
- [ ] Step plugin secret handling: step plugin config has no `secretKeyRef` support; API tokens visible in Rollout spec and dashboard UI — upstream Argo Rollouts limitation.
- [ ] Custom k6 metric support (user-defined Counter/Gauge/Rate/Trend).

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
- v0.2.0 shipped same day: e2e CI fixed (kind install, DOCKER_HOST conditional), Renovate configured

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
| Dynamic client (unstructured) for TestRun CRD | Avoid importing controller-runtime; keeps plugin dep tree small and scoped | ✓ Good — v0.3.0 shipped without controller-runtime |
| Pod exit codes for pass/fail (k6-operator #577 workaround) | k6-operator doesn't surface threshold-violation signal in CR status | ✓ Good — deterministic, no false positives |
| `handleSummary` JSON from pod logs for metric extraction | No sidecar, no extra CR, same metrics as Grafana Cloud provider | ✓ Good — `internal/provider/operator/summary.go` shipped in Phase 9 |
| PluginConfig field defaults live at consumer site (builder fn), not parseConfig or validation | Preserves "0 means unset" at the boundary; keeps builders pure (no slog, no cfg mutation) | ✓ Good — v0.3.0 Phase 08.2 ratified this pattern |
| e2e regression guards read back emitted CRs via `kubectl jsonpath` | Higher signal than Rollout-Healthy inference | ✓ Good — caught both 08.2 and 08.3 regressions reliably |
| Do NOT set `spec.Cleanup` on TestRun CRs | k6-operator v1.3.x `Cleanup=post` cascade-deletes CRs before plugin can observe terminal state | ✓ Good — v0.3.0 Phase 08.3 removed; success-path cleanup deferred to proper GarbageCollect hook |
| argo-rollouts controller logs are authoritative source of plugin stderr | go-plugin pipes binary stderr into controller log stream; diagnostic harness must capture it | ✓ Good — enshrined in e2e `dumpK6OperatorDiagnostics` after 08.3 debugging |

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
*Last updated: 2026-04-20 after v0.3.0 milestone completion.*
