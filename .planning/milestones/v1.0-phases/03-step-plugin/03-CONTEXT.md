# Phase 3: Step Plugin — Context

**Gathered:** 2026-04-10
**Status:** Ready for planning

<domain>
## Phase Boundary

A fully functional `RpcStep` implementation in `cmd/step-plugin/main.go` backed by `internal/step/` logic. The plugin implements a fire-and-wait lifecycle: the first `Run()` call triggers a k6 test run and stores the `runId` in `RpcStepContext.Status`; subsequent `Run()` calls poll via `GetRunResult` and return `PhaseRunning` (with `RequeueAfter: 15s`) until the run reaches a terminal state, then return `PhaseSuccessful` or `PhaseFailed`. `Terminate()` and `Abort()` both call `StopRun` to prevent orphaned cloud runs. `testRunId` is returned in `RpcStepResult.Status` for downstream metric plugin coordination.

Phase 2's metric plugin is already complete and not modified in this phase. This phase does NOT add new metric types or modify `internal/metric/`.

</domain>

<decisions>
## Implementation Decisions

### Timeout behavior
- **D-01:** When elapsed time since first `Run()` exceeds the configured `timeout`, the step plugin calls `StopRun` to stop the k6 run, then returns `PhaseFailed` with message `"timed out after <duration>"`. Fail-safe: timeout = failure triggers rollback. No orphaned cloud runs.
- **D-02:** Timeout is tracked by storing `triggeredAt` (RFC3339 timestamp string) in `RpcStepContext.Status` on the first `Run()` call. Subsequent calls compare `time.Now()` against `triggeredAt + timeout`.
- **D-03:** Default timeout: `5m` (5 minutes) if `PluginConfig.Timeout` is empty. Maximum timeout: `2h`. If `Timeout` cannot be parsed, return an error measurement immediately (don't silently use the default — force the user to fix their config).

### RequeueAfter interval
- **D-04:** Fixed `15 * time.Second` — always requeue after 15 seconds when the run is still active. No `pollInterval` config field. k6 Cloud evaluates thresholds every 60s so sub-15s polling provides no benefit.

### RpcStepResult.Status content
- **D-05:** Status map keys (all `Run()` calls, not just terminal):
  - `"runId"` — k6 Cloud test run ID (for downstream metric plugin coordination via AnalysisTemplate args)
  - `"testRunURL"` — full URL to k6 Cloud dashboard for this run
  - `"finalStatus"` — RunState string on terminal calls; empty string on `PhaseRunning` calls
- **D-06:** On the first `Run()` call, `triggeredAt` is also stored in `RpcStepContext.Status` (not in RpcStepResult.Status — it's internal timeout tracking only).

### Terminate vs Abort semantics
- **D-07:** Both `Terminate()` and `Abort()` call `provider.StopRun` if a `runId` is present in `rolloutContext.Status["runId"]`. Both return an empty `RpcStepResult{}` (no phase — Argo Rollouts sets the phase based on the call type). This matches the reference implementation pattern.
- **D-08:** If `StopRun` fails during Terminate/Abort, log the error but do NOT return an error in the RPC response — the run must be marked terminated regardless (controller requires this). Log at WARN level.

### Step config
- **D-09:** Reuse `internal/provider/config.go`'s `PluginConfig` struct. Fields `Metric` and `Aggregation` (added in Phase 2) are unused by the step plugin — they are ignored silently. No separate step-specific config struct.
- **D-10:** Required fields for the step plugin: `testId` (or `testRunId` for poll-only mode) + `apiToken` + `stackId`. `timeout` is optional (defaults to `5m`). Missing required fields → return `PhaseFailed` with descriptive error message.

### Run/poll lifecycle
- **D-11:** State stored in `RpcStepContext.Status` (a `map[string]string`) across calls:
  - `"runId"` — set on first Run() trigger, read on all subsequent calls
  - `"triggeredAt"` — RFC3339 timestamp, set on first Run(), used for timeout calculation
- **D-12:** First `Run()` detection: `rolloutContext.Status["runId"]` is empty → trigger path. If `testRunId` is provided in config, use that as runId instead of triggering (poll-only, consistent with D-04 from Phase 2).
- **D-13:** On terminal `RunState`: populate all 3 Status keys (`runId`, `testRunURL`, `finalStatus`), set `RequeueAfter: 0`, return appropriate `Phase` (PhaseSuccessful for Passed, PhaseFailed for Failed/Errored/Aborted/timeout).

### Phase mapping
- **D-14:** RunState → RpcStepResult.Phase:
  - `Running` → `PhaseRunning` (+ RequeueAfter: 15s)
  - `Passed` → `PhaseSuccessful`
  - `Failed` → `PhaseFailed`
  - `Errored` → `PhaseFailed` (step plugin treats errors as failures — rollback is the safe default for a one-shot step; differs from metric plugin which returns PhaseError)
  - `Aborted` → `PhaseFailed` (same rationale)
  - timeout exceeded → `PhaseFailed`

### Package layout
- **D-15:** RpcStep implementation in `internal/step/step.go`. Struct: `K6StepPlugin` implementing the `types.RpcStep` interface.
- **D-16:** `cmd/step-plugin/main.go` registers `"RpcStepPlugin": &stepRpc.RpcStepPlugin{Impl: impl}` where `impl = step.New(cloud.NewGrafanaCloudProvider())`.

### Test approach
- **D-17:** Unit tests in `internal/step/step_test.go` mock the `Provider` interface (same hand-written mock pattern as Phase 2). Tests cover: first Run (trigger path), first Run (poll-only path), subsequent Run (polling, PhaseRunning), terminal Run (Passed → PhaseSuccessful), terminal Run (Failed → PhaseFailed), timeout Run (stop + PhaseFailed), Terminate (StopRun called), Abort (StopRun called), missing required field validation.
- **D-18:** `go test -race -v -count=1 ./internal/step/...` must pass. ≥80% coverage on `internal/step/`.

### Claude's Discretion
- Exact timeout parsing (use `time.ParseDuration` from Go stdlib)
- Whether `triggeredAt` parsing failure on subsequent calls falls back to no-timeout or returns an error
- Internal helper function names and structure
- Error message wording

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project planning
- `.planning/PROJECT.md` — Project goals, constraints, out-of-scope decisions
- `.planning/REQUIREMENTS.md` — Phase 3 requirements: PLUG-02, STEP-01, STEP-02, STEP-03, STEP-04, STEP-05

### Prior phase artifacts
- `.planning/phases/01-foundation-provider/01-CONTEXT.md` — Locked decisions D-01–D-17 (provider interface, RunResult, PluginConfig, slog, Go 1.24)
- `.planning/phases/02-metric-plugin/02-CONTEXT.md` — Locked decisions D-01–D-18 (metric dispatch, trigger-or-poll, Errored→PhaseError)
- `internal/provider/provider.go` — Provider interface (TriggerRun, GetRunResult, StopRun, Name)
- `internal/provider/config.go` — PluginConfig struct (reused as-is, Metric/Aggregation fields ignored)
- `internal/provider/cloud/cloud.go` — GrafanaCloudProvider implementation
- `internal/metric/metric.go` — K6MetricProvider (reference pattern for step plugin structure)
- `cmd/step-plugin/main.go` — Existing stub with empty plugin map

### Research (prior phases)
- `.planning/research/STACK.md` — RpcStep interface signatures, go-plugin RPC details
- `.planning/research/PITFALLS.md` — Non-idempotent Run pitfall, orphaned runs, concurrent races
- `.planning/phases/02-metric-plugin/02-RESEARCH.md` — argo-rollouts import paths, wiring pattern confirmed

### External references
- `https://github.com/argoproj/argo-rollouts/blob/v1.9.0/rollout/steps/plugin/rpc/rpc.go` — RpcStep interface and RpcStepPlugin struct
- `https://github.com/argoproj/argo-rollouts/blob/v1.9.0/utils/plugin/types/types.go` — RpcStepContext, RpcStepResult types
- `https://github.com/argoproj/argo-rollouts/tree/master/test/cmd/step-plugin-sample` — Reference step plugin binary structure

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/provider/provider.go` — Provider interface + RunState + RunResult — use directly
- `internal/provider/config.go` — PluginConfig struct — reuse as-is
- `internal/provider/cloud/cloud.go` — GrafanaCloudProvider — instantiate in cmd/step-plugin/main.go
- `cmd/step-plugin/main.go` — setupLogging() + handshakeConfig already present; fill in plugin map
- `internal/metric/metric.go` — Reference: parseConfig(), stateless struct pattern, provider injection, error handling with metricutil

### Established Patterns
- `var _ SomeInterface = (*SomeStruct)(nil)` compile-time check
- `slog.With("provider", p.Name())` for structured logging
- Hand-written Provider mock in tests (no testify mock framework)
- JSON config parsing via `json.Unmarshal([]byte(metric.Provider.Plugin[pluginName]), &cfg)`
- `metav1.Now()` for timestamps

### Integration Points
- `cmd/step-plugin/main.go` comment: "Phase 3 will add: RpcStepPlugin" — replace empty map
- `internal/step/step.go` (new) — K6StepPlugin implementing `types.RpcStep`

</code_context>

<specifics>
## Specific Ideas

- `RpcStepContext.Status` is a `map[string]string` — all values must be strings (no nested JSON)
- `triggeredAt` stored as `time.Now().UTC().Format(time.RFC3339)` for consistent parsing
- Step plugin name constant: `"jmichalek132/k6-step"` (distinct from metric plugin's `"jmichalek132/k6"`)
- Phase 2's Errored→PhaseError decision applies to metric plugin only; step plugin maps Errored→PhaseFailed (D-14) — the one-shot nature of the step makes error = rollback the correct default
- For the test of `testRunURL` in Status: use `RunResult.TestRunURL` which GrafanaCloudProvider already populates

</specifics>

<deferred>
## Deferred Ideas

- User-configurable `pollInterval` field — deferred; fixed 15s default is sufficient for v1
- Separate step-specific config struct — deferred; reusing PluginConfig keeps code simple

</deferred>

---

*Phase: 03-step-plugin*
*Context gathered: 2026-04-10*
