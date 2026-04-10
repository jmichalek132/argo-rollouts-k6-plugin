# Phase 3: Step Plugin - Research

**Researched:** 2026-04-10
**Domain:** RpcStep interface implementation, step plugin lifecycle, state persistence via json.RawMessage, Terminate/Abort cleanup
**Confidence:** HIGH

## Summary

Phase 3 implements the `K6StepPlugin` struct in `internal/step/step.go` that satisfies the Argo Rollouts `rpc.StepPlugin` interface (which embeds `types.RpcStep` + `InitPlugin()`). The plugin uses a fire-and-wait lifecycle: the first `Run()` call triggers a k6 test run, persists the run ID and triggeredAt timestamp in `RpcStepResult.Status` (as marshaled JSON), and returns `PhaseRunning` with `RequeueAfter: 15s`. Subsequent `Run()` calls unmarshal the persisted state from `RpcStepContext.Status`, poll `GetRunResult`, and return the appropriate phase once the run reaches a terminal state. `Terminate()` and `Abort()` both call `StopRun` to prevent orphaned cloud test runs.

A critical finding from source code analysis: both `RpcStepContext.Status` and `RpcStepResult.Status` are `json.RawMessage` (not `map[string]string` as CONTEXT.md D-05/D-11 describe at a conceptual level). The implementation must define a Go struct (e.g., `stepState`) with the required fields, marshal it to `json.RawMessage` for `RpcStepResult.Status`, and unmarshal from `json.RawMessage` when reading `RpcStepContext.Status`. The same applies to `RpcStepContext.Config` -- the plugin config is delivered as `json.RawMessage`, not the pre-parsed map used by the metric plugin.

The step plugin's phase constants are `types.StepPhase` values (`PhaseRunning`, `PhaseSuccessful`, `PhaseFailed`, `PhaseError`) from `utils/plugin/types/steps.go`. These are **different types** from the metric plugin's `v1alpha1.AnalysisPhase` constants -- they have the same string values but are distinct Go types.

**Primary recommendation:** Implement `K6StepPlugin` with Provider dependency injection (same pattern as K6MetricProvider), define a `stepState` struct for Status round-tripping, parse config from `json.RawMessage` (reusing `provider.PluginConfig`), and wire into `cmd/step-plugin/main.go` using `rolloutsPlugin.RpcStepPlugin{Impl: impl}`. All state management via `json.RawMessage` marshal/unmarshal -- no `metricutil` helper available for step plugins.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** When elapsed time since first `Run()` exceeds the configured `timeout`, the step plugin calls `StopRun` to stop the k6 run, then returns `PhaseFailed` with message `"timed out after <duration>"`. Fail-safe: timeout = failure triggers rollback. No orphaned cloud runs.
- **D-02:** Timeout is tracked by storing `triggeredAt` (RFC3339 timestamp string) in `RpcStepContext.Status` on the first `Run()` call. Subsequent calls compare `time.Now()` against `triggeredAt + timeout`.
- **D-03:** Default timeout: `5m` (5 minutes) if `PluginConfig.Timeout` is empty. Maximum timeout: `2h`. If `Timeout` cannot be parsed, return an error measurement immediately (don't silently use the default -- force the user to fix their config).
- **D-04:** Fixed `15 * time.Second` -- always requeue after 15 seconds when the run is still active. No `pollInterval` config field.
- **D-05:** Status map keys (all `Run()` calls, not just terminal): `"runId"`, `"testRunURL"`, `"finalStatus"` -- as fields in a JSON struct serialized to `json.RawMessage`.
- **D-06:** On the first `Run()` call, `triggeredAt` is also stored in status (internal timeout tracking).
- **D-07:** Both `Terminate()` and `Abort()` call `provider.StopRun` if a `runId` is present. Both return empty `RpcStepResult{}`.
- **D-08:** If `StopRun` fails during Terminate/Abort, log the error at WARN level but do NOT return an error in the RPC response.
- **D-09:** Reuse `internal/provider/config.go`'s `PluginConfig` struct. Fields `Metric` and `Aggregation` are unused -- ignored silently.
- **D-10:** Required fields: `testId` (or `testRunId`) + `apiToken` + `stackId`. `timeout` optional (defaults to `5m`). Missing required fields return `PhaseFailed`.
- **D-11:** State stored across calls: `runId` + `triggeredAt` (RFC3339 timestamp).
- **D-12:** First `Run()` detection: status has no runId. If `testRunId` in config, use that (poll-only).
- **D-13:** Terminal RunState: populate all status keys, RequeueAfter: 0, return appropriate Phase.
- **D-14:** RunState -> Phase: Running->PhaseRunning, Passed->PhaseSuccessful, Failed->PhaseFailed, Errored->PhaseFailed, Aborted->PhaseFailed, timeout->PhaseFailed.
- **D-15:** Implementation in `internal/step/step.go`. Struct: `K6StepPlugin`.
- **D-16:** `cmd/step-plugin/main.go` registers `"RpcStepPlugin": &stepRpc.RpcStepPlugin{Impl: impl}`.
- **D-17:** Unit tests in `internal/step/step_test.go` with hand-written Provider mock.
- **D-18:** `go test -race -v -count=1 ./internal/step/...` must pass. >=80% coverage.

### Claude's Discretion
- Exact timeout parsing (use `time.ParseDuration` from Go stdlib)
- Whether `triggeredAt` parsing failure on subsequent calls falls back to no-timeout or returns an error
- Internal helper function names and structure
- Error message wording

### Deferred Ideas (OUT OF SCOPE)
- User-configurable `pollInterval` field -- fixed 15s default is sufficient for v1
- Separate step-specific config struct -- reusing PluginConfig keeps code simple
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PLUG-02 | Step plugin binary implements full `RpcStep` interface (`Run`, `Terminate`, `Abort`, `Type`) | Exact interface verified: `rpc.StepPlugin` = `InitPlugin() RpcError` + `RpcStep` (Run, Terminate, Abort, Type). All method signatures documented below. |
| STEP-01 | Accept `testId`, `apiToken` (secretRef), `stackId` (secretRef), and `timeout` in step config | Config arrives as `json.RawMessage` in `RpcStepContext.Config`. Unmarshal to `provider.PluginConfig` which already has all required fields. |
| STEP-02 | Trigger k6 Cloud test run, return `PhaseRunning` with `RequeueAfter`, poll on subsequent `Run` calls until terminal state | Controller calls `Run()` repeatedly. State persisted via `RpcStepResult.Status` (json.RawMessage) -> `RpcStepContext.Status` round-trip. RequeueAfter minimum enforced by controller at 10s; our 15s is above threshold. |
| STEP-03 | Return `testRunId` in `RpcStepResult.Status` so downstream metric plugin can consume it via AnalysisTemplate args | Status is `json.RawMessage` -- downstream consumers must parse it. The `runId` field will be present in the JSON blob persisted on every Run() call. |
| STEP-04 | Return `PhaseSuccessful` if k6 thresholds passed, `PhaseFailed` if thresholds failed or test errored | Phase constants: `types.PhaseSuccessful`, `types.PhaseFailed`. Map RunState.Passed->PhaseSuccessful, RunState.Failed/Errored/Aborted->PhaseFailed per D-14. |
| STEP-05 | Call `StopRun` on the active test run when `Terminate` or `Abort` is called -- no orphaned cloud runs | Controller passes prior Run status via `RpcStepContext.Status` to Terminate/Abort. Unmarshal to get runId, call StopRun. Return empty result. |
</phase_requirements>

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | 1.24 (go.mod 1.24.9) | Language runtime | Already in go.mod. argo-rollouts v1.9.0 requires go 1.24.9+. |
| `github.com/argoproj/argo-rollouts` | v1.9.0 | Step plugin types + RPC wrapper | Already in go.mod. Provides `rollout/steps/plugin/rpc.RpcStepPlugin`, `rpc.StepPlugin` interface, `types.RpcStepContext`, `types.RpcStepResult`, `types.StepPhase` constants. |
| `github.com/hashicorp/go-plugin` | v1.6.3 | Plugin host framework | Already in go.mod. Matches argo-rollouts v1.9.0 dependency. |
| `log/slog` | stdlib | Structured logging | Locked decision from Phase 1. Already in use. |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `encoding/json` | stdlib | Config + Status (de)serialization | Unmarshal `RpcStepContext.Config` to `PluginConfig`, marshal/unmarshal `stepState` struct for Status round-trip |
| `time` | stdlib | Timeout parsing, duration, RFC3339 | `time.ParseDuration` for timeout, `time.Now().UTC().Format(time.RFC3339)` for triggeredAt, `time.Parse(time.RFC3339, ...)` for comparison |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions | Already in go.mod. |

### No New Dependencies

Phase 3 adds zero new dependencies to go.mod. Everything needed is already present from Phases 1 and 2.

## Architecture Patterns

### Project Structure (Phase 3 additions)

```
internal/
  step/
    step.go           # K6StepPlugin implementing rpc.StepPlugin
    step_test.go       # Unit tests with mock Provider
cmd/
  step-plugin/
    main.go            # Wire K6StepPlugin into RpcStepPlugin, serve
```

### Key Interface: `rpc.StepPlugin` (must implement)

```go
// Source: github.com/argoproj/argo-rollouts@v1.9.0/rollout/steps/plugin/rpc/rpc.go
type StepPlugin interface {
    InitPlugin() types.RpcError
    types.RpcStep  // embedded
}

// Source: github.com/argoproj/argo-rollouts@v1.9.0/utils/plugin/types/types.go
type RpcStep interface {
    Run(*v1alpha1.Rollout, *RpcStepContext) (RpcStepResult, RpcError)
    Terminate(*v1alpha1.Rollout, *RpcStepContext) (RpcStepResult, RpcError)
    Abort(*v1alpha1.Rollout, *RpcStepContext) (RpcStepResult, RpcError)
    Type() string
}
```

### Critical Type: `RpcStepContext` (input)

```go
// Source: github.com/argoproj/argo-rollouts@v1.9.0/utils/plugin/types/steps.go
type RpcStepContext struct {
    PluginName string           // name from Rollout spec
    Config     json.RawMessage  // plugin config from Rollout spec PluginStep.Config
    Status     json.RawMessage  // previous RpcStepResult.Status (nil on first call)
}
```

**How the controller builds it** (from `rollout/steps/plugin/plugin.go` line 246-256):
- `Config` comes from `PluginStep.Config` in the Rollout spec (user-defined JSON)
- `Status` comes from `StepPluginStatus.Status` (the plugin's previous `RpcStepResult.Status` value)
- On first Run call, `Status` is nil (no prior execution)

### Critical Type: `RpcStepResult` (output)

```go
// Source: github.com/argoproj/argo-rollouts@v1.9.0/utils/plugin/types/steps.go
type RpcStepResult struct {
    Phase        StepPhase       // "Running", "Successful", "Failed", "Error"
    Message      string          // human-readable status message
    RequeueAfter time.Duration   // how long before controller calls Run again
    Status       json.RawMessage // persisted state, passed back as RpcStepContext.Status
}
```

### Phase Constants: `types.StepPhase` (NOT `v1alpha1.AnalysisPhase`)

```go
// Source: github.com/argoproj/argo-rollouts@v1.9.0/utils/plugin/types/steps.go
const (
    PhaseRunning    StepPhase = "Running"
    PhaseSuccessful StepPhase = "Successful"
    PhaseFailed     StepPhase = "Failed"
    PhaseError      StepPhase = "Error"
)
```

These are **different Go types** from `v1alpha1.AnalysisPhaseRunning` etc. (used by metric plugin). Same string values but NOT interchangeable at compile time.

### Internal State Struct Pattern

```go
// stepState persists between Run() calls via RpcStepResult.Status -> RpcStepContext.Status.
type stepState struct {
    RunID       string `json:"runId"`
    TriggeredAt string `json:"triggeredAt"`       // RFC3339
    TestRunURL  string `json:"testRunURL"`
    FinalStatus string `json:"finalStatus,omitempty"` // empty while running
}
```

Marshal to `json.RawMessage` for `RpcStepResult.Status`:
```go
statusBytes, err := json.Marshal(state)
// ...
return types.RpcStepResult{
    Phase:        types.PhaseRunning,
    RequeueAfter: 15 * time.Second,
    Status:       statusBytes,
}, types.RpcError{}
```

Unmarshal from `RpcStepContext.Status`:
```go
var state stepState
if ctx.Status != nil {
    if err := json.Unmarshal(ctx.Status, &state); err != nil {
        return types.RpcStepResult{}, types.RpcError{ErrorString: err.Error()}
    }
}
```

### Config Parsing Pattern (differs from metric plugin)

The metric plugin reads config from `metric.Provider.Plugin[pluginName]` (a `map[string]json.RawMessage`). The step plugin reads config from `RpcStepContext.Config` (a `json.RawMessage` directly).

```go
func parseConfig(ctx *types.RpcStepContext) (*provider.PluginConfig, error) {
    if ctx == nil || ctx.Config == nil {
        return nil, fmt.Errorf("step config is nil")
    }
    var cfg provider.PluginConfig
    if err := json.Unmarshal(ctx.Config, &cfg); err != nil {
        return nil, fmt.Errorf("unmarshal step config: %w", err)
    }
    // Validate required fields per D-10
    if cfg.TestID == "" && cfg.TestRunID == "" {
        return nil, fmt.Errorf("either testId or testRunId is required")
    }
    if cfg.APIToken == "" {
        return nil, fmt.Errorf("apiToken is required")
    }
    if cfg.StackID == "" {
        return nil, fmt.Errorf("stackId is required")
    }
    return &cfg, nil
}
```

### Wiring Pattern for cmd/step-plugin/main.go

```go
import (
    stepRpc "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
    goPlugin "github.com/hashicorp/go-plugin"

    "github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/cloud"
    "github.com/jmichalek132/argo-rollouts-k6-plugin/internal/step"
)

// In main():
p := cloud.NewGrafanaCloudProvider()
impl := step.New(p)

goPlugin.Serve(&goPlugin.ServeConfig{
    HandshakeConfig: handshakeConfig,
    Plugins: map[string]goPlugin.Plugin{
        "RpcStepPlugin": &stepRpc.RpcStepPlugin{Impl: impl},
    },
})
```

### Controller Behavior to Account For

From `rollout/steps/plugin/plugin.go`:

1. **Backoff enforcement**: Controller enforces minimum 10s requeue (`minRequeueDuration`). If `RequeueAfter < 10s`, controller uses default 30s. Our 15s is above the minimum, so it will be used as-is.

2. **Error handling**: If `RpcError.HasError()` is true, controller sets `StepPluginPhaseError` and applies 30s backoff. This means returning errors from `Run()` is safe -- the controller will retry.

3. **Status persistence on error**: Controller does NOT update `Status` when phase is `PhaseError` (line 102-105 in plugin.go). This means if `Run()` returns an error, the previous status is preserved for retry. Our implementation can safely return errors without corrupting state.

4. **Terminate is synchronous**: Controller logs a warning and overrides to `PhaseFailed` if Terminate returns `PhaseRunning`. Terminate MUST NOT return `PhaseRunning`.

5. **Abort is synchronous**: Same as Terminate -- cannot return `PhaseRunning`.

### Anti-Patterns to Avoid

- **Returning `types.PhaseRunning` from Terminate/Abort**: Controller will override to `PhaseFailed` and log a warning. Always return a terminal phase or empty result.
- **Using `v1alpha1.AnalysisPhase*` constants in step plugin**: Wrong type. Use `types.StepPhase` constants (`types.PhaseRunning`, `types.PhaseSuccessful`, etc.).
- **Assuming `RpcStepContext.Status` is `map[string]string`**: It's `json.RawMessage`. Must define a struct and marshal/unmarshal.
- **Triggering a new run when Status already contains a runId**: Non-idempotent Run causes duplicate k6 runs. Always check status first.
- **Global mutable state in the plugin struct**: The controller may call the plugin for multiple rollouts. All state must be in the `RpcStepResult.Status` / `RpcStepContext.Status` round-trip.
- **Using `metricutil.MarkMeasurementError`**: This helper is for `v1alpha1.Measurement` (metric plugin). Step plugin errors are returned as `types.RpcError{ErrorString: ...}`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Status serialization | Manual string concatenation for Status | `json.Marshal`/`json.Unmarshal` with typed struct | Controller round-trips `json.RawMessage`; structured JSON is the only safe format |
| Duration parsing | Manual regex on timeout string | `time.ParseDuration` | Handles all Go duration formats: "5m", "2h30m", "300s" etc. |
| RFC3339 timestamps | Custom format strings | `time.RFC3339` constant | Standard format, parseable by any tool viewing the status |

## Common Pitfalls

### Pitfall 1: Status is json.RawMessage, Not map[string]string

**What goes wrong:** Developer assumes `RpcStepContext.Status` is a `map[string]string` (as conceptually described in CONTEXT.md D-05/D-11) and tries to access it as such.
**Why it happens:** The CONTEXT.md descriptions use "map keys" language for clarity, but the actual Go type is `json.RawMessage`. The metric plugin uses `map[string]string` for `Measurement.Metadata`, reinforcing the expectation.
**How to avoid:** Define a typed `stepState` struct. Marshal to `json.RawMessage` for `RpcStepResult.Status`. Unmarshal from `json.RawMessage` when reading `RpcStepContext.Status`. Nil check before unmarshal (nil = first call).
**Warning signs:** Compile error: "cannot use map[string]string as json.RawMessage".

### Pitfall 2: Duplicate k6 Runs on Non-Idempotent Run

**What goes wrong:** Every `Run()` call triggers a new k6 test run, burning Grafana Cloud quota.
**Why it happens:** Developer doesn't check `RpcStepContext.Status` for existing runId before triggering.
**How to avoid:** First `Run()` detection: `ctx.Status == nil || len(ctx.Status) == 0` or unmarshaled state has empty `RunID`. Only trigger when no existing run.
**Warning signs:** Multiple test runs appear in k6 Cloud for a single rollout step.

### Pitfall 3: Terminate Returns PhaseRunning

**What goes wrong:** Step plugin returns `PhaseRunning` from `Terminate()` or `Abort()`, causing controller to log warning and override to `PhaseFailed`.
**Why it happens:** Developer returns the same result pattern as `Run()` polling.
**How to avoid:** Per D-07, return empty `RpcStepResult{}` from Terminate/Abort. The controller handles phase assignment. If you do set a phase, use `PhaseSuccessful` or `PhaseFailed` only.
**Warning signs:** Controller logs "terminate cannot run asynchronously" warning.

### Pitfall 4: Timeout Check Uses Wall Clock Without UTC

**What goes wrong:** `triggeredAt` stored in one timezone, compared against `time.Now()` in another. Timeout fires too early or too late.
**Why it happens:** `time.Now()` uses local timezone by default.
**How to avoid:** Always use `time.Now().UTC()` for both storage and comparison. Store as `time.RFC3339` which is UTC-aware. Parse back with `time.Parse(time.RFC3339, ...)`.
**Warning signs:** Tests pass locally but fail in CI (different timezone), or timeout is off by hours.

### Pitfall 5: StopRun Error Blocks Terminate/Abort

**What goes wrong:** `StopRun` fails (network error, already-stopped run) and plugin returns an `RpcError`, preventing the controller from marking the step as terminated.
**Why it happens:** Natural error propagation instinct -- if StopRun fails, return the error.
**How to avoid:** Per D-08, log StopRun errors at WARN level but return empty `RpcStepResult{}` with no error. The controller must be able to mark the step terminated regardless.
**Warning signs:** Rollout stuck in "terminating" state because StopRun consistently fails.

## Code Examples

### Complete Run() Flow (verified pattern from official sample)

```go
// Source: Argo Rollouts test/cmd/step-plugin-sample/internal/plugin/plugin.go
// Adapted for k6 step plugin

func (k *K6StepPlugin) Run(rollout *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
    // 1. Parse config
    cfg, err := parseConfig(ctx)
    if err != nil {
        return types.RpcStepResult{Phase: types.PhaseFailed, Message: err.Error()}, types.RpcError{}
    }

    // 2. Parse timeout
    timeout, err := parseTimeout(cfg.Timeout)
    if err != nil {
        return types.RpcStepResult{Phase: types.PhaseFailed, Message: err.Error()}, types.RpcError{}
    }

    // 3. Unmarshal previous state
    var state stepState
    if ctx.Status != nil {
        if err := json.Unmarshal(ctx.Status, &state); err != nil {
            return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("unmarshal status: %v", err)}
        }
    }

    // 4. First call: trigger or attach
    if state.RunID == "" {
        // ... trigger run, populate state.RunID and state.TriggeredAt
    }

    // 5. Check timeout
    // ... compare time.Now().UTC() against state.TriggeredAt + timeout

    // 6. Poll run result
    // ... GetRunResult, map state to phase

    // 7. Marshal state and return
    statusBytes, _ := json.Marshal(state)
    return types.RpcStepResult{
        Phase:        phase,
        Message:      message,
        RequeueAfter: requeueAfter,
        Status:       statusBytes,
    }, types.RpcError{}
}
```

### Complete Terminate/Abort Flow

```go
func (k *K6StepPlugin) Terminate(rollout *v1alpha1.Rollout, ctx *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
    // Parse config for API credentials
    cfg, err := parseConfig(ctx)
    if err != nil {
        // Can't call StopRun without credentials; log and return empty
        slog.Warn("terminate: cannot parse config", "error", err)
        return types.RpcStepResult{}, types.RpcError{}
    }

    // Unmarshal state to get runId
    var state stepState
    if ctx.Status != nil {
        if err := json.Unmarshal(ctx.Status, &state); err != nil {
            slog.Warn("terminate: cannot unmarshal status", "error", err)
            return types.RpcStepResult{}, types.RpcError{}
        }
    }

    // Stop the run if we have an ID
    if state.RunID != "" {
        if err := k.provider.StopRun(context.Background(), cfg, state.RunID); err != nil {
            slog.Warn("failed to stop run during terminate", "runId", state.RunID, "error", err)
        }
    }

    return types.RpcStepResult{}, types.RpcError{}
}
```

### Error Handling: Validation Errors vs RPC Errors

```go
// Validation errors -> PhaseFailed with message (not RpcError)
// The controller does NOT retry PhaseFailed -- it's a terminal state.
if cfg.TestID == "" && cfg.TestRunID == "" {
    return types.RpcStepResult{
        Phase:   types.PhaseFailed,
        Message: "either testId or testRunId is required",
    }, types.RpcError{}
}

// Unexpected errors -> RpcError
// The controller sets PhaseError and retries with 30s backoff.
// The previous Status is preserved (not overwritten).
result, err := k.provider.TriggerRun(ctx, cfg)
if err != nil {
    return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("trigger run: %v", err)}
}
```

**Key distinction:** Use `PhaseFailed` for user errors (bad config, timeout, k6 failure). Use `RpcError` for infrastructure errors (API unreachable, unmarshal failure). The controller retries `RpcError` but not `PhaseFailed`.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| gRPC-based step plugins | net/rpc (gob encoding) via go-plugin | Always (Argo Rollouts design) | Must use net/rpc, not gRPC |
| Single combined interface | Separate StepPlugin = InitPlugin + RpcStep | v1.7+ | InitPlugin called once at startup; Run/Terminate/Abort per operation |
| No backoff control | Controller enforces min 10s + plugin-set RequeueAfter | v1.7+ | Our 15s respects the minimum |

**Step plugin is relatively new:** Added in Argo Rollouts v1.7 (2024). The API is stable in v1.9.0 but has fewer third-party examples than metric plugins.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib testing + testify v1.11.1 |
| Config file | None (go test defaults) |
| Quick run command | `go test -race -v -count=1 ./internal/step/...` |
| Full suite command | `go test -race -v -count=1 ./...` |

### Phase Requirements -> Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PLUG-02 | K6StepPlugin implements rpc.StepPlugin (all 5 methods) | unit | `go test -race -v -count=1 ./internal/step/... -run TestInterface` | Wave 0 |
| STEP-01 | Config parsing: testId/testRunId + apiToken + stackId + timeout | unit | `go test -race -v -count=1 ./internal/step/... -run TestParseConfig` | Wave 0 |
| STEP-02 | Trigger + poll lifecycle: first Run triggers, subsequent polls | unit | `go test -race -v -count=1 ./internal/step/... -run TestRun` | Wave 0 |
| STEP-03 | runId returned in RpcStepResult.Status on every call | unit | `go test -race -v -count=1 ./internal/step/... -run TestRunStatus` | Wave 0 |
| STEP-04 | Phase mapping: Passed->Successful, Failed/Errored/Aborted->Failed | unit | `go test -race -v -count=1 ./internal/step/... -run TestPhaseMapping` | Wave 0 |
| STEP-05 | Terminate/Abort call StopRun, no orphaned runs | unit | `go test -race -v -count=1 ./internal/step/... -run TestTerminate\|TestAbort` | Wave 0 |
| D-01 | Timeout: StopRun + PhaseFailed after elapsed > timeout | unit | `go test -race -v -count=1 ./internal/step/... -run TestTimeout` | Wave 0 |
| D-03 | Default timeout 5m, max 2h, unparseable returns error | unit | `go test -race -v -count=1 ./internal/step/... -run TestTimeoutParsing` | Wave 0 |
| D-18 | Race detector passes | race | `go test -race -v -count=1 ./internal/step/...` | Wave 0 |
| Build | step-plugin binary compiles | build | `make build-step` | Existing |

### Sampling Rate

- **Per task commit:** `go test -race -v -count=1 ./internal/step/...`
- **Per wave merge:** `go test -race -v -count=1 ./...` + `make build`
- **Phase gate:** Full suite green + `make build` + coverage >= 80%

### Coverage Command

```bash
go test -race -coverprofile=coverage.out -count=1 ./internal/step/...
go tool cover -func=coverage.out | grep total
```

### Wave 0 Gaps

- [ ] `internal/step/step.go` -- K6StepPlugin implementation (new file)
- [ ] `internal/step/step_test.go` -- all test cases listed above (new file)
- No framework install needed -- all dependencies already in go.mod

### Estimated Test Runtime

- Unit tests (./internal/step/...): ~2-3 seconds (mock-based, no I/O)
- Full suite (./...): ~5-8 seconds (includes metric + provider tests)
- Build: ~3-5 seconds

## Sources

### Primary (HIGH confidence)
- `github.com/argoproj/argo-rollouts@v1.9.0/utils/plugin/types/types.go` -- RpcStep interface definition (4 methods)
- `github.com/argoproj/argo-rollouts@v1.9.0/utils/plugin/types/steps.go` -- RpcStepContext, RpcStepResult, StepPhase constants (json.RawMessage confirmed)
- `github.com/argoproj/argo-rollouts@v1.9.0/rollout/steps/plugin/rpc/rpc.go` -- RpcStepPlugin struct, StepPlugin interface (InitPlugin + RpcStep), RPC args/response types
- `github.com/argoproj/argo-rollouts@v1.9.0/rollout/steps/plugin/plugin.go` -- Controller-side behavior: how RpcStepContext is built, backoff enforcement (min 10s), Status not updated on error
- `github.com/argoproj/argo-rollouts@v1.9.0/rollout/steps/plugin/client/client.go` -- Client-side handshake: `MagicCookieValue: "step"`, dispense key `"RpcStepPlugin"`
- `github.com/argoproj/argo-rollouts@v1.9.0/test/cmd/step-plugin-sample/` -- Official reference implementation (main.go wiring, plugin.go lifecycle, State struct pattern)
- `github.com/argoproj/argo-rollouts@v1.9.0/rollout/steps/plugin/rpc/rpc_test.go` -- RPC round-trip test pattern with ServeTestConfig

### Secondary (MEDIUM confidence)
- `.planning/research/PITFALLS.md` -- Pitfalls 5-7 cover step plugin non-idempotency, orphaned runs, concurrent state

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all libraries already in go.mod, no new deps
- Architecture: HIGH -- interface signatures read directly from v1.9.0 source code in module cache
- Pitfalls: HIGH -- verified against controller source code behavior (plugin.go)
- Wiring pattern: HIGH -- confirmed against official step-plugin-sample in argo-rollouts repo

**Research date:** 2026-04-10
**Valid until:** 2026-05-10 (stable; Argo Rollouts v1.9.0 is pinned)
