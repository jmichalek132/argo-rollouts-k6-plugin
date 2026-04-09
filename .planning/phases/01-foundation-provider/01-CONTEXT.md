# Phase 1: Foundation & Provider - Context

**Gathered:** 2026-04-09
**Status:** Ready for planning

<domain>
## Phase Boundary

A working Go module (`github.com/jmichalek132/argo-rollouts-k6-plugin`) with two binary entrypoints (`cmd/metric-plugin`, `cmd/step-plugin`) that compile to static CGO-disabled binaries, an internal provider abstraction interface with a fully tested Grafana Cloud k6 implementation, and a Makefile-based build pipeline. No Argo Rollouts plugin interfaces yet — those are Phase 2 and 3. This phase is purely the foundation layer that subsequent phases build on.

</domain>

<decisions>
## Implementation Decisions

### Provider interface
- **D-01:** Unified interface — 4 methods only: `Name() string`, `TriggerRun(ctx, cfg)`, `GetRunResult(ctx, cfg, runID)`, `StopRun(ctx, cfg, runID)`
- **D-02:** `GetRunResult` returns a `RunResult` struct with ALL metrics always populated (not split into lifecycle vs metrics methods)
- **D-03:** Partial/live metrics — `GetRunResult` returns current metric values even during active runs (State == Running). Metric plugin returns live measurements each poll cycle; Argo Rollouts evaluates thresholds incrementally, enabling early abort.
- **D-04:** `RunResult` struct fields: `State RunState`, `TestRunURL string`, `ThresholdsPassed bool`, `HTTPReqFailed float64` (0.0–1.0), `HTTPReqDuration Percentiles` (p50/p95/p99 ms), `HTTPReqs float64` (req/s)
- **D-05:** `RunState` enum: `Running`, `Passed`, `Failed`, `Errored`, `Aborted` — covers all Grafana Cloud k6 terminal states

### Credential passing
- **D-06:** Stateless provider — credentials are NOT injected at construction time
- **D-07:** Each provider method receives `*PluginConfig` (parsed from AnalysisTemplate plugin config JSON per Run call). Provider creates an authenticated HTTP client per call using config.APIToken and config.StackID.
- **D-08:** `PluginConfig` struct: `TestRunID string`, `TestID string`, `APIToken string`, `StackID string`, `Timeout string` — JSON tags `testRunId`, `testId`, `apiToken`, `stackId`, `timeout`

### k6 Cloud client
- **D-09:** Use official `k6-cloud-openapi-client-go` v1.7.0-0.1.0 for test run lifecycle (trigger `POST /loadtests/v2/tests/{id}/start-testrun`, status `GET /loadtests/v2/test_runs/{id}`, stop `POST /loadtests/v2/test_runs/{id}/stop`)
- **D-10:** Fall back to hand-rolled `net/http` calls for metric/threshold endpoints (v5 API: `/cloud/v5/test_runs/{id}/query_aggregate_k6`, `/loadtests/v2/thresholds`) if the Go client does not expose them
- **D-11:** ~~Verify auth header format during implementation~~ **RESOLVED BY RESEARCH:** Use `Bearer <token>` via `k6.ContextAccessToken` — confirmed in k6-cloud-openapi-client-go source (`client.go`). `Token <key>` was the deprecated v2 API format.

### Logging
- **D-12:** `log/slog` (Go stdlib) — structured JSON logging, outputs to stderr. No external logging dependency.
- **D-13:** Log level controlled via environment variable `LOG_LEVEL` (default: `info`). Plugin binaries set up slog handler in `main()`.

### Module & build
- **D-14:** Go module: `github.com/jmichalek132/argo-rollouts-k6-plugin`
- **D-15:** ~~Go version: 1.22~~ **SUPERSEDED BY RESEARCH:** Go 1.24 required — argo-rollouts v1.9.0 declares `go 1.24.9` in its go.mod. See `01-RESEARCH.md` Go Version section.
- **D-16:** Build tooling: Makefile with targets: `make build`, `make test`, `make lint`, `make clean`
- **D-17:** Package layout: `cmd/metric-plugin/main.go`, `cmd/step-plugin/main.go`, `internal/provider/provider.go` (interface + types), `internal/provider/cloud/cloud.go` (Grafana Cloud impl)

### Claude's Discretion
- Mock HTTP server approach for provider unit tests (httptest.NewServer or testify mock)
- Internal error type design (wrapped stdlib errors vs typed sentinel errors)
- go.mod dependency management (direct vs replace directives)
- Exact Makefile structure beyond the four required targets
- Whether to use `context.Background()` or pass through ctx from plugin lifecycle

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

No external specs in this greenfield repo — requirements and decisions are fully captured above and in the planning files below.

### Project planning
- `.planning/PROJECT.md` — Project goals, constraints, out-of-scope decisions
- `.planning/REQUIREMENTS.md` — Phase 1 requirements: PLUG-04, PROV-01, PROV-02, PROV-03, PROV-04, DIST-01, DIST-04

### Research
- `.planning/research/STACK.md` — Go interface signatures, hashicorp/go-plugin v1.7.0 details, k6-cloud-openapi-client-go, exact Grafana Cloud API endpoints
- `.planning/research/ARCHITECTURE.md` — Two-binary architecture rationale, provider interface design, package boundaries, build order
- `.planning/research/PITFALLS.md` — Critical: stdout-before-Serve pitfall, CGO DNS issues, go-plugin handshake conventions

### External references (for implementation)
- `https://github.com/argoproj/argo-rollouts/tree/master/test/cmd/metrics-plugin-sample` — Reference metric plugin binary structure
- `https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types` — Go types for RpcMetricProvider and RpcStep interfaces (Phase 2/3 reference, but import in go.mod needed Phase 1)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- None — greenfield project, no existing code

### Established Patterns
- None yet — this phase establishes the patterns all subsequent phases follow

### Integration Points
- `internal/provider/provider.go` — Provider interface defined here; Phase 2 (metric plugin) and Phase 3 (step plugin) import and use it
- `internal/provider/cloud/cloud.go` — GrafanaCloudProvider struct implementing Provider; future providers (Phase 2 v2: KubernetesJobProvider) add new files in `internal/provider/`
- `cmd/metric-plugin/main.go` and `cmd/step-plugin/main.go` — Entry points where `plugin.Serve()` is called with the hashicorp/go-plugin handshake config; Phase 2 and 3 add the RpcMetricProvider and RpcStep implementations here

</code_context>

<specifics>
## Specific Ideas

- GitHub remote: `git@github.com:jmichalek132/argo-rollouts-k6-plugin.git` — module path and release URLs should use `jmichalek132`
- Provider interface should be minimal enough that a v2 `KubernetesJobProvider` or `LocalBinaryProvider` can implement it without needing Grafana Cloud API types
- The `TriggerRun(ctx, cfg)` → returns `(runID string, error)` keeps run identity as a plain string so future providers can use any opaque ID format

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within Phase 1 scope.

</deferred>

---

*Phase: 01-foundation-provider*
*Context gathered: 2026-04-09*
