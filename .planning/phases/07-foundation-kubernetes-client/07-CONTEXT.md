# Phase 7: Foundation & Kubernetes Client - Context

**Gathered:** 2026-04-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Plugin can route between execution backends (grafana-cloud, k6-operator) based on per-call config, load k6 scripts from Kubernetes ConfigMaps, and initialize a working Kubernetes client from in-cluster credentials. Backward compatible — existing Grafana Cloud users are unaffected.

</domain>

<decisions>
## Implementation Decisions

### Provider Routing
- **D-01:** Router implements Provider pattern — a thin `provider.Router` multiplexer that implements the `provider.Provider` interface and delegates to the correct backend based on the `provider` field in PluginConfig. metric/step plugins receive the Router and remain provider-agnostic.
- **D-02:** Routing is per-call, not per-binary. The `provider` field in each AnalysisTemplate/Rollout config selects the backend. Empty or `"grafana-cloud"` defaults to existing Grafana Cloud provider. `"k6-operator"` routes to the k6-operator provider.
- **D-03:** main.go creates the Router via `provider.NewRouter()` with `WithProvider()` functional options. Both providers registered at startup; k6-operator uses lazy client init so grafana-cloud-only deployments never touch the k8s API.

### Config Structure
- **D-04:** Single PluginConfig struct with optional fields. All provider-specific fields added with `omitempty`. Provider-specific fields grouped by comments. New fields for Phase 7: `Provider string`, `ConfigMapRef *ConfigMapRef`, `Namespace string`.
- **D-05:** Validation is per-provider. Each provider implements a `Validate(cfg *PluginConfig) error` method. Shared validation (metric field required, timeout parsing) stays on `PluginConfig.Validate()`. The Router calls provider-specific validation before dispatching.

### K8s Client Lifecycle
- **D-06:** K6OperatorProvider owns its Kubernetes client as a struct field. Lazy initialization via `sync.Once` in an `ensureClient()` method that calls `rest.InClusterConfig()` + `kubernetes.NewForConfig()`. Provider is self-contained.
- **D-07:** Testing uses functional option `WithClient(fake)` to inject a fake client, bypassing lazy InClusterConfig. Matches the existing `WithBaseURL()` pattern on GrafanaCloudProvider.

### Script Sourcing
- **D-08:** ConfigMap reading lives inside K6OperatorProvider, not a separate package. The provider reads the ConfigMap as part of its flow. The Provider interface does not change.
- **D-09:** ConfigMapRef is a simple struct with `Name` and `Key` fields. Namespace defaults to the rollout's namespace. Mirrors how Kubernetes volumes reference ConfigMaps.
- **D-10:** Phase 7 validates ConfigMap existence + key presence + non-empty content only. No script content parsing or k6-specific validation. Let k6 validate the script when it runs. Fail-fast on infrastructure issues.

### Claude's Discretion
- Package layout for the k6-operator provider (e.g., `internal/provider/operator/`)
- ConfigMapRef struct location (config.go or separate file)
- Error message wording and slog field names

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Plugin Interface
- `internal/provider/provider.go` — Provider interface definition (TriggerRun, GetRunResult, StopRun, Name)
- `internal/provider/config.go` — Current PluginConfig struct (will be extended)
- `internal/provider/cloud/cloud.go` — GrafanaCloudProvider implementation (pattern to follow)

### Plugin Entry Points
- `cmd/metric-plugin/main.go` — Metric plugin binary entry point (needs Router wiring)
- `cmd/step-plugin/main.go` — Step plugin binary entry point (needs Router wiring)

### Plugin Consumers
- `internal/metric/metric.go` — K6MetricProvider (receives Provider, should not change)
- `internal/step/step.go` — K6StepPlugin (receives Provider, should not change)

### Test Patterns
- `internal/provider/providertest/mock.go` — Existing mock provider
- `internal/provider/cloud/cloud_test.go` — GrafanaCloudProvider test patterns

### Requirements
- `.planning/REQUIREMENTS.md` — FOUND-01, FOUND-02, FOUND-03 definitions

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `provider.Provider` interface — Router will implement this, no changes needed
- `cloud.Option` / `cloud.WithBaseURL()` pattern — Reuse for k6-operator provider options
- `provider.RunState` / `provider.RunResult` — Shared result types, usable by k6-operator provider
- `providertest/mock.go` — Mock provider for testing Router dispatch

### Established Patterns
- Stateless provider (credentials per call via PluginConfig) — k6-operator provider follows same pattern except k8s client is persistent
- Functional options for provider construction (`WithBaseURL`, `WithClient`)
- slog structured logging to stderr with JSON handler
- `providerCallTimeout` context timeout on every provider call

### Integration Points
- `cmd/*/main.go` — Replace `cloud.NewGrafanaCloudProvider()` with `provider.NewRouter()`
- `provider.PluginConfig` — Add Provider, ConfigMapRef, Namespace fields
- `go.mod` — Promote `k8s.io/client-go` from indirect to direct dependency

</code_context>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 07-foundation-kubernetes-client*
*Context gathered: 2026-04-15*
