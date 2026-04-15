# Phase 8: k6-operator Provider - Context

**Gathered:** 2026-04-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Plugin creates and manages k6-operator TestRun CRs (and PrivateLoadZone CRs for cloud-connected execution) for distributed in-cluster k6 execution. Implements TriggerRun, GetRunResult, and StopRun on the K6OperatorProvider stub from Phase 7. Determines pass/fail from runner pod exit codes. Supports parallelism, resource limits, custom runner image, and environment variable injection.

</domain>

<decisions>
## Implementation Decisions

### TestRun CRD Strategy
- **D-01:** Import k6-operator Go types directly from `github.com/grafana/k6-operator`. Typed TestRun and PrivateLoadZone structs with compile-time safety. Adds a direct dependency on the k6-operator module.
- **D-02:** Target both k6.io/v1alpha1 TestRun (pure in-cluster) and PrivateLoadZone (Grafana Cloud-connected in-cluster). Auto-detect which CRD to use: if Grafana Cloud fields (apiToken, stackId) are present alongside `provider: "k6-operator"`, use PrivateLoadZone; otherwise use TestRun. No explicit override field — just auto-detect.
- **D-03:** CRD client uses the same lazy-init Kubernetes client from Phase 7 (sync.Once pattern). No additional client initialization needed.

### Polling & Result Detection
- **D-04:** No internal polling loop. The plugin piggybacks on the Argo Rollouts controller's existing polling cadence. Each GetRunResult/Run call does a single GET to check TestRun status. No goroutines, no watches, no reconnection logic.
- **D-05:** Pass/fail detection via runner pod exit codes (k6-operator issue #577 workaround). After TestRun reaches 'finished' stage, list runner pods by label, check exit codes. Exit 0 = all k6 thresholds passed, non-zero = failed. Log parsing for detailed metrics is deferred to Phase 9.

### Config Extension Surface
- **D-06:** Essential fields only. Expose: parallelism (int), resource requests/limits (corev1.ResourceRequirements), runner image (string), environment variables ([]corev1.EnvVar), k6 arguments ([]string). Covers 90% of use cases without bloating PluginConfig.
- **D-07:** Namespace auto-injected from rollout. Plugin reads namespace from the AnalysisRun/Rollout ObjectMeta passed via RPC. User can override via the existing `namespace` config field from Phase 7. TestRun CR created in the resolved namespace.

### Cleanup & Abort Handling
- **D-08:** Primary cleanup: explicit Delete on TestRun/PLZ CR when StopRun is called (rollout abort/terminate). Always works, predictable.
- **D-09:** Safety net: set owner reference on the TestRun CR pointing to the AnalysisRun (if UID is available via RPC context). Kubernetes garbage collection auto-deletes orphaned TestRuns when the AnalysisRun is cleaned up. If AnalysisRun UID is not available, fall back to label-based identification for manual/script cleanup.
- **D-10:** Consistent naming: `k6-<rollout-name>-<hash>` for TestRun CRs. Labels: `app.kubernetes.io/managed-by: argo-rollouts-k6-plugin`, `k6-plugin/rollout: <rollout-name>`.

### Claude's Discretion
- TestRun spec construction helper functions (builder pattern or direct struct)
- Pod label selector construction for runner pod discovery
- Error message wording for CRD creation failures and pod exit code interpretation
- Internal state management for tracking created TestRun names between TriggerRun/GetRunResult/StopRun calls

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 7 Foundation (already built)
- `internal/provider/operator/operator.go` — K6OperatorProvider stub with lazy k8s client, ConfigMap reading, TriggerRun/GetRunResult/StopRun stubs to be replaced
- `internal/provider/operator/operator_test.go` — Existing test patterns (WithClient fake injection, ConfigMap mocking)
- `internal/provider/config.go` — PluginConfig with Provider, ConfigMapRef, Namespace, ValidateK6Operator(), ValidateGrafanaCloud()
- `internal/provider/router.go` — Router that dispatches to K6OperatorProvider
- `internal/provider/provider.go` — Provider interface (TriggerRun, GetRunResult, StopRun, Name, Validate)

### Plugin Interface (Argo Rollouts)
- `internal/metric/metric.go` — K6MetricProvider (receives Provider, calls GetRunResult)
- `internal/step/step.go` — K6StepPlugin (receives Provider, calls TriggerRun/GetRunResult/StopRun)

### Plugin Entry Points
- `cmd/metric-plugin/main.go` — Already wires Router with k6-operator provider
- `cmd/step-plugin/main.go` — Already wires Router with k6-operator provider

### Requirements
- `.planning/REQUIREMENTS.md` — K6OP-01 through K6OP-08 definitions

### External References
- k6-operator TestRun CRD: github.com/grafana/k6-operator API types
- k6-operator issue #577: TestRun status doesn't carry threshold results (exit code workaround)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `K6OperatorProvider` struct with lazy k8s client (sync.Once) — extend, don't replace
- `WithClient(fake)` option — reuse for Phase 8 tests with fake k8s clientset
- `readScript()` method — already reads ConfigMap scripts, called by TriggerRun
- `provider.RunState` / `provider.RunResult` — shared result types for status reporting
- `provider.PluginConfig` — extend with Phase 8 fields (parallelism, resources, etc.)

### Established Patterns
- Functional options for provider construction (WithClient, WithBaseURL)
- slog structured logging to stderr with JSON handler
- providerCallTimeout context timeout on every provider call
- Compile-time interface check: `var _ Provider = (*K6OperatorProvider)(nil)`
- Per-provider validation: Validate() method on each provider

### Integration Points
- Replace TriggerRun/GetRunResult/StopRun stubs in operator.go with real implementations
- Extend PluginConfig with parallelism, resources, runnerImage, env, arguments fields
- Extend ValidateK6Operator() to validate new fields
- Add k6-operator module to go.mod as direct dependency

</code_context>

<specifics>
## Specific Ideas

- Auto-detect TestRun vs PrivateLoadZone based on presence of Grafana Cloud credentials in config — no explicit CRD selection field
- Runner pod exit code 0 = pass, non-zero = fail — simple and reliable for Phase 8 scope
- Phase 9 will add log parsing for detailed metric extraction (handleSummary JSON)

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 08-k6-operator-provider*
*Context gathered: 2026-04-15*
