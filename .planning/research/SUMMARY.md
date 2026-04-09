# Project Research Summary

**Project:** argo-rollouts-k6-plugin
**Domain:** Argo Rollouts plugin (metric provider + canary step) for Grafana Cloud k6
**Researched:** 2026-04-09
**Confidence:** HIGH

## Executive Summary

This project builds two Go plugin binaries that integrate Grafana Cloud k6 load testing into Argo Rollouts progressive delivery workflows. The metric plugin implements the `RpcMetricProvider` interface to expose k6 test metrics (thresholds, error rate, latency percentiles) as AnalysisRun gates; the step plugin implements the `RpcStep` interface to trigger a k6 Cloud test run and block a canary rollout until it completes. Both plugins communicate with the Argo Rollouts controller via `net/rpc` over a Unix socket managed by `hashicorp/go-plugin` — they are standalone Go binaries, not containers or shared libraries. The official Go client `k6-cloud-openapi-client-go` covers the entire Grafana Cloud k6 API surface.

The recommended approach is two binaries sharing a single Go module: `cmd/metric-plugin` and `cmd/step-plugin` each satisfy a different `MagicCookieValue` (`"metricprovider"` vs `"step"`) that the controller hardcodes and cannot be negotiated. A shared `internal/provider` package defines a `Provider` interface with a Grafana Cloud implementation, enabling future in-cluster k6-operator support without changing the plugin's external API. State persistence between polling calls uses `Measurement.Metadata` for the metric plugin and `RpcStepContext.Status` for the step plugin — both are serialized into the relevant Kubernetes resource and survive plugin process restarts.

The primary risks are: (1) stdout pollution corrupting the go-plugin handshake — all logging must go to stderr from the first line of code; (2) non-idempotent `Run` methods triggering duplicate k6 test runs on controller requeue cycles; and (3) the Argo Rollouts controller blocking all rollouts if the plugin binary download fails at startup. All three are preventable with correct patterns established in Phase 1.

## Key Findings

### Recommended Stack

The plugin must be Go (the controller uses `encoding/gob` over `net/rpc` — no other language works). The dependency pinning is exact: `github.com/argoproj/argo-rollouts v1.9.0`, `github.com/hashicorp/go-plugin v1.7.0` (same versions the controller ships), and `github.com/grafana/k6-cloud-openapi-client-go v1.7.0-0.1.0` (official auto-generated client). The argo-rollouts dependency pulls a large Kubernetes transitive tree (~100MB+ binary); `CGO_ENABLED=0` static linking is required for deployment.

**Core technologies:**
- Go 1.23+: only viable language for the plugin — RPC protocol uses `encoding/gob`
- `argoproj/argo-rollouts v1.9.0`: provides `RpcMetricProvider`, `RpcStep` interfaces and CRD types — must match controller version
- `hashicorp/go-plugin v1.7.0`: process management and `net/rpc` transport — must match controller's pinned version
- `grafana/k6-cloud-openapi-client-go v1.7.0-0.1.0`: official type-safe client for Grafana Cloud k6 REST API v6
- `sirupsen/logrus v1.9.x`: structured logging to stderr — convention across all Argo Rollouts plugins
- GoReleaser: cross-compilation for linux/amd64 + linux/arm64, SHA256 checksums, GitHub Releases

### Expected Features

**Must have (table stakes):**
- Step plugin: trigger a k6 Cloud test by `testId` and block the rollout until completion (TS-2)
- Metric plugin: threshold pass/fail, error rate, p95/p99 latency, throughput from a running test run (TS-1, TS-3, TS-5, TS-6, TS-7)
- Kubernetes Secrets for API token and stack ID via `valueFrom.secretKeyRef` (TS-4)
- ConfigMap plugin registration with SHA256-verified binary from GitHub Releases (TS-8)
- Example AnalysisTemplate and Rollout YAML for common use cases (TS-9)

**Should have (differentiators):**
- Provider abstraction interface (`internal/provider`) for future in-cluster k6 backend (D-2)
- Structured multi-metric result object for richer `successCondition` expressions (D-3)
- `GetMetadata` surfacing k6 Cloud dashboard URLs in AnalysisRun status (D-4)
- Graceful termination: `Terminate`/`Abort` stop the k6 test run in Grafana Cloud (D-6)
- Automatic test run ID resolution (`latestRun: true`) to reduce step-metric wiring (D-5)

**Defer (v2+):**
- In-cluster k6 Job execution (RBAC complexity, separate domain)
- ConfigMap-based k6 script upload (versioning and dependencies are a different problem)
- VU/duration overrides at trigger time (creates divergence from k6 Cloud test definition)
- Helm chart or operator distribution

### Architecture Approach

Two binaries, one module. The controller spawns separate processes with different handshake cookies; a single binary cannot serve both plugin types. Shared `internal/provider` provides the `Provider` interface (`TriggerRun`, `GetRunResult`, `StopRun`) with `cloud.Provider` as the only v1 implementation. The metric plugin's async pattern — `Run` triggers and stores `runId` in `Measurement.Metadata`, `Resume` polls — is idiomatic and verified in the Argo Rollouts Web metric provider source. The step plugin uses `RpcStepContext.Status` (json.RawMessage) as the state carrier between requeue cycles.

**Major components:**
1. `cmd/metric-plugin` — serves `RpcMetricProviderPlugin` with `MagicCookieValue: "metricprovider"`
2. `cmd/step-plugin` — serves `RpcStepPlugin` with `MagicCookieValue: "step"`
3. `internal/metric` — `Run`/`Resume`/`Terminate`/`GarbageCollect`; runId in `Measurement.Metadata`
4. `internal/step` — `Run`/`Terminate`/`Abort`; runId in `RpcStepResult.Status`; all k6 terminal states
5. `internal/provider/cloud` — HTTP client wrapping `k6-cloud-openapi-client-go`; implements `Provider`
6. `internal/provider/provider.go` — `Provider` interface; `RunStatus` with `IsTerminal()`/`IsSuccess()`

### Critical Pitfalls

1. **stdout pollution breaks go-plugin handshake** — any output to stdout before `plugin.Serve()` kills the plugin. CI lint rule banning `fmt.Print*`/`os.Stdout` must land in Phase 1.
2. **Non-idempotent `Run` triggers duplicate k6 test runs** — metric plugin must check `Measurement.Metadata["runId"]`; step plugin must check `RpcStepContext.Status` before triggering. Missing this burns cloud quota and cross-contaminates measurements.
3. **Controller startup blocked by plugin download failure** — the controller downloads plugin binaries eagerly; failure blocks ALL rollouts. Always publish SHA256 checksums via GoReleaser CI; document `file://` init-container fallback.
4. **Missing k6 terminal states cause infinite polling** — step plugin must handle all variants: `finished`, `timed_out`, `aborted_by_user`, `aborted_by_system`, `aborted_by_script_error`, `aborted_by_threshold`.
5. **Concurrent AnalysisRuns share plugin process memory** — one plugin process serves all concurrent runs. Never use global mutable state. Use `Measurement.Metadata` and `RpcStepContext.Status` for per-run state. Run CI with `-race`.

## Implications for Roadmap

### Phase 1: Foundation and Build Pipeline
**Rationale:** go-plugin handshake and binary distribution constraints must be correct before any feature code lands. Stdout pollution and handshake mismatches are only caught by running the real binary against a real controller. Establish logging conventions, go.mod pins, and GoReleaser pipeline here.
**Delivers:** Two-binary scaffolding with correct handshake configs, logrus-to-stderr enforced by CI lint, GoReleaser producing linux/amd64+arm64 binaries with SHA256 checksums, `internal/provider` interface skeleton.
**Addresses:** TS-8 (ConfigMap registration), D-2 (provider abstraction)
**Avoids:** Pitfall 1 (stdout), Pitfall 2 (controller startup block), Pitfall 3 (protocol mismatch), Pitfall 10 (CGO/DNS)

### Phase 2: Grafana Cloud Provider and Metric Plugin
**Rationale:** The provider layer (`internal/provider/cloud`) has no dependency on Argo Rollouts types and can be unit-tested against a mock HTTP server. Building it first makes Phase 2 plugin implementation purely glue between provider and RPC interface. The metric plugin's `Run`/`Resume` async pattern is the harder concept — solve it before the step plugin.
**Delivers:** Full `internal/provider/cloud` with `TriggerRun`/`GetRunResult`/`StopRun`, `internal/metric` with threshold/error-rate/latency/throughput metrics, `GetMetadata` with k6 Cloud URLs, auth validation in `InitPlugin`.
**Uses:** `k6-cloud-openapi-client-go` for all API calls; `Measurement.Metadata` for runId persistence; mock Provider for unit tests
**Addresses:** TS-1, TS-3, TS-4, TS-5, TS-6, TS-7, D-3, D-4
**Avoids:** Pitfall 4 (non-idempotent metric Run), Pitfall 7 (concurrent safety), Pitfall 8 (auth format), Pitfall 9 (secret resolution)

### Phase 3: Step Plugin
**Rationale:** The step plugin reuses the Phase 2 provider. It introduces `RpcStepContext.Status` state persistence and the full terminal state machine for k6 runs. Simpler interface than metric plugin but requires comprehensive terminal state coverage.
**Delivers:** `internal/step` with `Run`/`Terminate`/`Abort`, configurable timeout, graceful test run cancellation, all k6 terminal state handling.
**Addresses:** TS-2, D-6 (graceful termination)
**Avoids:** Pitfall 5 (non-idempotent step Run), Pitfall 6 (missing terminal states)

### Phase 4: Rich Examples and Differentiating Features
**Rationale:** With both plugins working, the combined step+metric workflow is demonstrable. Automatic test run ID resolution and structured multi-metric results reduce boilerplate. Example templates are the primary adoption surface and should be polished after the feature set stabilizes.
**Delivers:** Example AnalysisTemplates (threshold gate, error rate, latency, combined step+metric), `latestRun` auto-resolution, custom metric support, structured result objects.
**Addresses:** TS-9, D-1, D-5, D-7

### Phase 5: Testing Infrastructure and Release
**Rationale:** E2E tests on kind verify the full binary-loading path that unit tests cannot cover. This phase requires stable binaries from all previous phases. A concurrent rollout scenario validates race safety.
**Delivers:** kind-based e2e test suite (AnalysisRun lifecycle, Rollout step plugin, concurrent runs with `-race`), CI pipeline (lint → unit → build → e2e → release), v0.1.0 published with multi-arch binaries and checksums.
**Avoids:** Pitfall 2 (checksum verification in release CI), Pitfall 7 (concurrent safety in e2e)

### Phase Ordering Rationale

- Provider before plugins: `internal/provider/cloud` is independent of Argo Rollouts types and drives out the k6 API integration before introducing plugin system complexity.
- Metric before step: the `Run`/`Resume` async pattern with `Measurement.Metadata` is the harder concept; solving it first makes the step plugin's `RpcStepContext.Status` pattern straightforward.
- Examples last: YAML examples depend on stable plugin names and config field names; premature examples create maintenance debt during Phase 2/3 iteration.
- E2E last: kind cluster tests are the slowest feedback loop; they confirm correctness but don't drive design decisions.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2 (Grafana Cloud metrics API):** The `k6-cloud-openapi-client-go` v6 client covers test runs and load test management. Whether v6 includes the aggregate metrics query endpoint (previously at `/cloud/v5/test_runs/{id}/query_aggregate_k6`) needs validation. p95/custom metrics may require a direct call against the v5 endpoint.
- **Phase 5 (E2E binary loading in kind):** How to mount plugin binaries into the Argo Rollouts controller pod in kind without modifying the controller image is not fully documented and needs a concrete solution before the phase begins.

Phases with standard patterns (skip research-phase):
- **Phase 1 (scaffold):** Handshake configs verified from source. GoReleaser config is templated. No unknowns.
- **Phase 3 (step plugin):** Reuses Phase 2 patterns. `RpcStep` interface is simpler than `RpcMetricProvider`. Terminal state machine is documented in k6 API reference.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All versions verified from controller source (go.mod, client.go files). Official Grafana client confirmed from GitHub. |
| Features | HIGH | Feature set maps directly to RPC interface methods and k6 API surface. Table stakes have no ambiguity. |
| Architecture | HIGH | Two-binary requirement verified by reading hardcoded handshake constants in controller source. State persistence patterns verified in Web metric provider. |
| Pitfalls | HIGH | Sourced from go-plugin GitHub issues, Argo Rollouts community discussions, and k6 API auth documentation. The stdout pitfall has a confirmed upstream issue. |

**Overall confidence:** HIGH

### Gaps to Address

- **Metrics query API for aggregate values:** Confirm whether `k6-cloud-openapi-client-go` v6 exposes aggregate metric queries (p95, custom metrics) or whether the v5 query endpoint must be called directly. Validate before Phase 2 begins.
- **`Authorization: Token` vs `Authorization: Bearer`:** STACK.md documents `Bearer` but PITFALLS.md flags `Token` as the correct prefix. The generated client's `ContextAccessToken` should resolve this — verify the client's auth header format against the k6 API reference before writing the cloud provider.
- **Step plugin alpha status:** The step plugin feature was added in v1.8.0 and may still have rough edges in v1.9.0. Monitor `argoproj/argo-rollouts` issue tracker for step plugin bugs during Phase 3.

## Sources

### Primary (HIGH confidence)
- Argo Rollouts v1.9.0 source: `metricproviders/plugin/client/client.go`, `rollout/steps/plugin/client/client.go` — handshake config values
- Argo Rollouts v1.9.0 source: `utils/plugin/types/types.go`, `metricproviders/plugin/rpc/rpc.go`, `rollout/steps/plugin/rpc/rpc.go` — interface definitions
- `github.com/grafana/k6-cloud-openapi-client-go` README and source — API client, endpoints, auth
- `github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus` — reference metric plugin implementation
- `github.com/argoproj/argo-rollouts/tree/master/test/cmd/step-plugin-sample` — reference step plugin with state management
- HashiCorp go-plugin issues #164, #306 — stdout pollution behavior

### Secondary (MEDIUM confidence)
- Grafana Cloud k6 REST API docs — endpoint shapes, status codes, auth headers
- k6 Cloud test status codes reference — terminal state enumeration
- Argo Rollouts Plugin Documentation — installation, ConfigMap format, binary loading
- Argo Rollouts Analysis Overview — AnalysisRun, successCondition, failureCondition semantics

### Tertiary (LOW confidence)
- Grafana Cloud k6 aggregate metrics query API (v5) — needs direct validation against `k6-cloud-openapi-client-go` to confirm v6 coverage

---
*Research completed: 2026-04-09*
*Ready for roadmap: yes*
