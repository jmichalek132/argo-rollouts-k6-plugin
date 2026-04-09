# Architecture Patterns

**Domain:** Argo Rollouts k6 Load Testing Plugin
**Researched:** 2026-04-09

## Recommended Architecture

### Two Binaries, One Repository (Monorepo with Shared Core)

The Argo Rollouts controller uses **different hashicorp/go-plugin handshake configurations** for metric plugins (`MagicCookieValue: "metricprovider"`) and step plugins (`MagicCookieValue: "step"`). Each plugin type is loaded as a separate OS process with its own handshake. A single binary **cannot** serve both plugin types -- the handshake would fail for whichever type doesn't match.

**Decision: Two binaries, one Go module, shared provider package.**

```
argo-rollouts-k6-plugin/
  cmd/
    metric-plugin/         # Binary 1: metric provider plugin
      main.go              #   HandshakeConfig{MagicCookieValue: "metricprovider"}
    step-plugin/           # Binary 2: step plugin
      main.go              #   HandshakeConfig{MagicCookieValue: "step"}
  internal/
    provider/              # Shared provider abstraction (pure Go interfaces)
      provider.go          #   Provider interface definition
      config.go            #   Shared configuration types
      cloud/               #   Grafana Cloud k6 implementation (v1)
        client.go          #   HTTP client for k6 Cloud API
        client_test.go
        provider.go        #   Implements Provider interface
        provider_test.go
        types.go           #   API request/response types
      # Future providers:
      # job/               #   In-cluster k6 Job (v2)
      # binary/            #   Direct k6 binary (v3)
    metric/                # Metric plugin implementation
      plugin.go            #   Implements rpc.MetricProviderPlugin
      plugin_test.go
      state.go             #   In-process state tracking for async runs
      state_test.go
    step/                  # Step plugin implementation
      plugin.go            #   Implements rpc.StepPlugin
      plugin_test.go
  test/
    e2e/                   # Integration tests (kind cluster)
      suite_test.go
      metric_plugin_test.go
      step_plugin_test.go
      testdata/             # k6 scripts, AnalysisTemplates, Rollout manifests
  go.mod
  go.sum
  Makefile
```

**Confidence: HIGH** -- Verified by reading Argo Rollouts source code. The controller-side client packages (`metricproviders/plugin/client/client.go` and `rollout/steps/plugin/client/client.go`) hardcode different `HandshakeConfig` values and dispense different plugin keys (`"RpcMetricProviderPlugin"` vs `"RpcStepPlugin"`).

### Component Boundaries

| Component | Responsibility | Communicates With |
|-----------|---------------|-------------------|
| `cmd/metric-plugin` | Binary entrypoint; registers metric RPC server with go-plugin | Argo Rollouts controller (via net/rpc over unix socket) |
| `cmd/step-plugin` | Binary entrypoint; registers step RPC server with go-plugin | Argo Rollouts controller (via net/rpc over unix socket) |
| `internal/metric` | Implements `rpc.MetricProviderPlugin` interface; manages measurement state; delegates to provider | `internal/provider` for k6 operations |
| `internal/step` | Implements `rpc.StepPlugin` interface; delegates to provider | `internal/provider` for k6 operations |
| `internal/provider` | Defines `Provider` interface; contains provider implementations | Grafana Cloud k6 API (HTTP), future: Kubernetes API, local binary |
| `internal/provider/cloud` | Grafana Cloud k6 HTTP client and Provider implementation | `https://api.k6.io/cloud/v6/*` |

### Data Flow: Metric Plugin (Primary Use Case)

The metric plugin is the more complex of the two because it must handle **asynchronous test runs** across multiple polling intervals. The Argo Rollouts controller calls `Run()` on the first measurement, then `Resume()` on subsequent intervals to check progress.

```
AnalysisRun created by controller
    |
    v
[1] Controller calls plugin.Run(analysisRun, metric)
    |
    v
[2] MetricPlugin.Run():
    - Unmarshal config from metric.Provider.Plugin["k6-cloud/k6"]
    - Call provider.TriggerRun(testID) -> returns runID
    - Store runID in Measurement.Metadata
    - Set Measurement.Phase = Running
    - Set Measurement.ResumeAt = now + 10s
    - Return Measurement
    |
    v
[3] Controller waits for ResumeAt, then calls plugin.Resume(analysisRun, metric, measurement)
    |
    v
[4] MetricPlugin.Resume():
    - Read runID from measurement.Metadata
    - Call provider.GetRunStatus(runID) -> status, metrics
    - If status == Running:
        - Set measurement.ResumeAt = now + 10s
        - Return measurement (still Running phase)
    - If status == Finished:
        - Extract requested metric value (e.g., "http_req_duration_p95")
        - Set measurement.Value = metric value as string
        - Set measurement.Phase = Successful (controller evaluates successCondition)
        - Set measurement.FinishedAt = now
        - Return measurement
    - If status == Failed/TimedOut/Aborted:
        - Set measurement.Phase = Failed
        - Set measurement.Message = error details
        - Return measurement
    |
    v
[5] Controller evaluates successCondition against measurement.Value
    (e.g., "result < 500" for p95 latency < 500ms)
```

**Key insight from Argo Rollouts source:** The `RpcMetricProvider` interface comment states: `Run` should "start a new external system call for a measurement" and "should be idempotent and do nothing if a call has already been started." `Resume` "checks if the external system call is finished and returns the current measurement." This maps perfectly to the async k6 pattern: `Run` triggers the test, `Resume` polls for completion.

### Data Flow: Step Plugin

The step plugin is simpler -- it uses `RpcStepContext.Status` (a `json.RawMessage`) to persist state between `Run()` calls. The controller re-calls `Run()` when `RequeueAfter` expires.

```
Rollout step reached
    |
    v
[1] Controller calls plugin.Run(rollout, context)
    - context.Config = user config from Rollout spec
    - context.Status = nil (first call)
    |
    v
[2] StepPlugin.Run():
    - Unmarshal config from context.Config
    - If context.Status is nil (first call):
        - Call provider.TriggerRun(testID) -> runID
        - Marshal {runID, startTime} into status
        - Return RpcStepResult{Phase: Running, RequeueAfter: 15s, Status: status}
    - If context.Status has runID (subsequent call):
        - Unmarshal state from context.Status
        - Call provider.GetRunStatus(runID) -> status
        - If Running: return {Phase: Running, RequeueAfter: 15s, Status: state}
        - If Finished + thresholds pass: return {Phase: Successful}
        - If Failed: return {Phase: Failed, Message: "..."}
    |
    v
[3] Controller advances or rolls back based on Phase
```

## Provider Interface Definition

```go
// Package provider defines the abstraction layer for k6 execution backends.
package provider

import (
    "context"
    "time"
)

// RunStatus represents the current state of a k6 test run.
type RunStatus string

const (
    RunStatusCreated      RunStatus = "created"
    RunStatusInitializing RunStatus = "initializing"
    RunStatusRunning      RunStatus = "running"
    RunStatusFinished     RunStatus = "finished"
    RunStatusTimedOut     RunStatus = "timed_out"
    RunStatusAbortedUser  RunStatus = "aborted_user"
    RunStatusAbortedSystem RunStatus = "aborted_system"
)

// IsTerminal returns true if the run has reached a final state.
func (s RunStatus) IsTerminal() bool {
    switch s {
    case RunStatusFinished, RunStatusTimedOut, RunStatusAbortedUser, RunStatusAbortedSystem:
        return true
    }
    return false
}

// IsSuccess returns true if the run finished successfully.
func (s RunStatus) IsSuccess() bool {
    return s == RunStatusFinished
}

// RunResult holds the outcome of a completed (or in-progress) k6 test run.
type RunResult struct {
    // RunID is the unique identifier for this test run.
    RunID string

    // Status is the current state of the run.
    Status RunStatus

    // ThresholdsPassed indicates whether all k6 thresholds passed.
    // nil if run is not yet finished or thresholds are not available.
    ThresholdsPassed *bool

    // Metrics contains named metric values from the run.
    // Keys follow k6 metric naming: "http_req_duration_p95", "http_req_failed_rate", etc.
    Metrics map[string]float64

    // StartedAt is when the run began execution.
    StartedAt time.Time

    // EndedAt is when the run completed (zero value if still running).
    EndedAt time.Time

    // Error contains error details if the run failed.
    Error string
}

// TriggerConfig holds parameters for starting a k6 test run.
type TriggerConfig struct {
    // TestID is the Grafana Cloud k6 test ID (v1) or reference to a test script.
    TestID string

    // Options override runtime options (future use for in-cluster/binary providers).
    Options map[string]interface{}
}

// Provider is the interface that all k6 execution backends must implement.
// Implementations: cloud.Provider (Grafana Cloud k6), job.Provider (in-cluster), binary.Provider (direct).
type Provider interface {
    // TriggerRun starts a new k6 test run and returns the run ID.
    // Must be safe to retry -- if a run is already in progress for the same
    // test, implementations should return that run's ID rather than starting another.
    TriggerRun(ctx context.Context, cfg TriggerConfig) (runID string, err error)

    // GetRunResult returns the current status and metrics for a run.
    // For in-progress runs, Metrics may be partially populated or empty.
    GetRunResult(ctx context.Context, runID string) (*RunResult, error)

    // StopRun requests cancellation of an in-progress run.
    // No-op if the run is already in a terminal state.
    StopRun(ctx context.Context, runID string) error

    // Name returns the provider name for logging and identification.
    Name() string
}
```

**Confidence: HIGH** on the interface shape. Derived from:
- The Grafana Cloud k6 API surface (trigger run, poll status, get metrics)
- The Argo Rollouts plugin lifecycle requirements (async trigger + poll pattern)
- Future extensibility needs (in-cluster k6 Jobs use the same trigger/poll pattern)

## Configuration Schema

### Metric Plugin AnalysisTemplate

The metric plugin config is embedded in `metric.Provider.Plugin` as a `map[string]json.RawMessage`. The key is the plugin name registered in the ConfigMap.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-load-test-analysis
spec:
  args:
    - name: k6-test-id
    - name: k6-api-token
      valueFrom:
        secretKeyRef:
          name: k6-cloud-credentials
          key: api-token
    - name: k6-stack-id
      valueFrom:
        secretKeyRef:
          name: k6-cloud-credentials
          key: stack-id
  metrics:
    - name: k6-error-rate
      interval: 30s
      count: 10
      failureLimit: 3
      successCondition: "result < 0.05"       # Error rate below 5%
      provider:
        plugin:
          k6-cloud/k6:                         # Plugin name matches ConfigMap entry
            provider: cloud                    # Which provider backend
            testId: "{{args.k6-test-id}}"      # Grafana Cloud k6 test ID
            apiToken: "{{args.k6-api-token}}"
            stackId: "{{args.k6-stack-id}}"
            metric: "http_req_failed_rate"     # Which k6 metric to return as measurement value
```

#### Plugin Config Go Type (parsed from json.RawMessage)

```go
// PluginConfig is the configuration passed via metric.Provider.Plugin["k6-cloud/k6"]
type PluginConfig struct {
    // Provider selects the execution backend: "cloud" (default), "job", "binary"
    Provider string `json:"provider,omitempty"`

    // TestID is the Grafana Cloud k6 test ID (for cloud provider)
    TestID string `json:"testId"`

    // APIToken is the Grafana Cloud k6 API token
    APIToken string `json:"apiToken"`

    // StackID is the Grafana Stack instance ID
    StackID string `json:"stackId"`

    // Metric is the k6 metric name to return as the measurement value.
    // Examples: "http_req_duration_p95", "http_req_duration_p99",
    //           "http_req_failed_rate", "thresholds_passed"
    // Default: "thresholds_passed" (returns 1.0 if all thresholds pass, 0.0 otherwise)
    Metric string `json:"metric,omitempty"`

    // BaseURL overrides the Grafana Cloud k6 API base URL (for testing)
    BaseURL string `json:"baseUrl,omitempty"`
}
```

### Step Plugin Rollout Spec

The step plugin config is embedded in `RpcStepContext.Config` as `json.RawMessage`.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      steps:
        - setWeight: 20
        - pause: {duration: 30s}
        - plugin:
            name: k6-cloud/k6-step
            config:
              provider: cloud
              testId: "12345"
              apiToken: "{{secrets.k6-cloud-credentials.api-token}}"
              stackId: "{{secrets.k6-cloud-credentials.stack-id}}"
              timeout: 10m                    # Max time to wait for test completion
        - setWeight: 50
```

### ConfigMap Registration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  metricProviderPlugins: |
    - name: "k6-cloud/k6"
      location: "https://github.com/ORG/argo-rollouts-k6-plugin/releases/download/v0.1.0/metric-plugin-linux-amd64"
      sha256: "abc123..."
  stepPlugins: |
    - name: "k6-cloud/k6-step"
      location: "https://github.com/ORG/argo-rollouts-k6-plugin/releases/download/v0.1.0/step-plugin-linux-amd64"
      sha256: "def456..."
```

## State Management: Metric Plugin

The metric plugin must track in-flight k6 test runs across `Run()` and `Resume()` calls. The Argo Rollouts framework provides two mechanisms:

### Approach: Measurement.Metadata (Recommended)

The `v1alpha1.Measurement` struct has a `Metadata map[string]string` field designed for providers to store state. The metric plugin stores the run ID in `Metadata["runId"]` during `Run()` and reads it back during `Resume()`.

```go
func (p *MetricPlugin) Run(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
    m := v1alpha1.Measurement{StartedAt: &now}

    // Parse config
    cfg, err := parseConfig(metric)
    if err != nil {
        return markError(m, err)
    }

    // Trigger test run via provider
    runID, err := p.provider.TriggerRun(ctx, provider.TriggerConfig{TestID: cfg.TestID})
    if err != nil {
        return markError(m, err)
    }

    // Store state in metadata for Resume()
    m.Metadata = map[string]string{
        "runId":    runID,
        "metric":   cfg.Metric,
        "provider": cfg.Provider,
    }
    m.Phase = v1alpha1.AnalysisPhaseRunning

    // Schedule Resume() after 10 seconds
    resumeAt := metav1.NewTime(time.Now().Add(10 * time.Second))
    m.ResumeAt = &resumeAt

    return m
}

func (p *MetricPlugin) Resume(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
    runID := measurement.Metadata["runId"]
    metricName := measurement.Metadata["metric"]

    result, err := p.provider.GetRunResult(ctx, runID)
    if err != nil {
        return markError(measurement, err)
    }

    if !result.Status.IsTerminal() {
        // Still running -- schedule another Resume
        resumeAt := metav1.NewTime(time.Now().Add(10 * time.Second))
        measurement.ResumeAt = &resumeAt
        return measurement
    }

    // Run completed -- extract the requested metric value
    if result.Status.IsSuccess() {
        if metricName == "thresholds_passed" {
            if result.ThresholdsPassed != nil && *result.ThresholdsPassed {
                measurement.Value = "1"
            } else {
                measurement.Value = "0"
            }
        } else if val, ok := result.Metrics[metricName]; ok {
            measurement.Value = fmt.Sprintf("%f", val)
        } else {
            return markError(measurement, fmt.Errorf("metric %q not found in run results", metricName))
        }
        measurement.Phase = v1alpha1.AnalysisPhaseSuccessful
    } else {
        measurement.Phase = v1alpha1.AnalysisPhaseFailed
        measurement.Message = fmt.Sprintf("k6 run %s: %s", result.Status, result.Error)
    }

    finishedAt := metav1.Now()
    measurement.FinishedAt = &finishedAt
    return measurement
}
```

**Why not in-process state?** The Prometheus sample plugin is stateless (instant query, instant result). For k6 cloud runs that take minutes, we need persistent state across `Run()` -> `Resume()` calls. `Measurement.Metadata` is the idiomatic approach -- it persists in the AnalysisRun CR and survives plugin process restarts.

**Confidence: HIGH** -- `Measurement.Metadata` and `Measurement.ResumeAt` are used by the Web metric provider in the Argo Rollouts codebase for exactly this async pattern.

## State Management: Step Plugin

The step plugin uses `RpcStepContext.Status` (`json.RawMessage`) for state persistence. This is explicitly designed for the re-queue pattern -- the controller passes the previous `Status` back on each `Run()` call.

```go
type StepState struct {
    RunID     string    `json:"runId"`
    StartedAt time.Time `json:"startedAt"`
}
```

The step plugin marshals `StepState` into `RpcStepResult.Status` and reads it back from `RpcStepContext.Status` on re-invocation. This is the standard pattern demonstrated in the official step-plugin-sample.

## Patterns to Follow

### Pattern 1: Config Parsing by Plugin Name Key

The metric plugin config lives in `metric.Provider.Plugin[pluginName]` as `json.RawMessage`. Always look up by the exact plugin name registered in the ConfigMap.

```go
func parseConfig(metric v1alpha1.Metric) (*PluginConfig, error) {
    raw, ok := metric.Provider.Plugin[PluginName]
    if !ok {
        return nil, fmt.Errorf("plugin config not found for %s", PluginName)
    }
    var cfg PluginConfig
    if err := json.Unmarshal(raw, &cfg); err != nil {
        return nil, fmt.Errorf("failed to unmarshal plugin config: %w", err)
    }
    // Apply defaults
    if cfg.Metric == "" {
        cfg.Metric = "thresholds_passed"
    }
    if cfg.Provider == "" {
        cfg.Provider = "cloud"
    }
    return &cfg, nil
}
```

### Pattern 2: Provider Factory

Select provider implementation based on config string. This is the extensibility point for v2/v3.

```go
func NewProvider(providerType string, cfg *PluginConfig) (provider.Provider, error) {
    switch providerType {
    case "cloud", "":
        return cloud.New(cloud.Config{
            APIToken: cfg.APIToken,
            StackID:  cfg.StackID,
            BaseURL:  cfg.BaseURL,
        })
    // case "job":
    //     return job.New(...)    // v2
    // case "binary":
    //     return binary.New(...) // v3
    default:
        return nil, fmt.Errorf("unknown provider type: %s", providerType)
    }
}
```

### Pattern 3: go-plugin Test Harness

The sample plugins use `goPlugin.ServeTestConfig` for in-process plugin testing without starting a real binary. This is the canonical testing approach.

```go
func TestPluginRPC(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    impl := &MetricPlugin{provider: mockProvider}
    pluginMap := map[string]goPlugin.Plugin{
        "RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{Impl: impl},
    }

    ch := make(chan *goPlugin.ReattachConfig, 1)
    closeCh := make(chan struct{})
    go goPlugin.Serve(&goPlugin.ServeConfig{
        HandshakeConfig: handshakeConfig,
        Plugins:         pluginMap,
        Test: &goPlugin.ServeTestConfig{
            Context: ctx, ReattachConfigCh: ch, CloseCh: closeCh,
        },
    })

    config := <-ch
    client := goPlugin.NewClient(&goPlugin.ClientConfig{
        HandshakeConfig: handshakeConfig,
        Plugins:         pluginMap,
        Reattach:        config,
    })
    // ... dispense and test
}
```

### Pattern 4: Idempotent Run

The `RpcMetricProvider.Run` comment explicitly states it "should be idempotent and do nothing if a call has already been started." For the k6 plugin, check if a run is already active before triggering a new one.

## Anti-Patterns to Avoid

### Anti-Pattern 1: In-Process State for Cross-Call Persistence (Metric Plugin)

**What:** Storing run IDs in a map or struct field on the plugin instance.
**Why bad:** The plugin process can restart between `Run()` and `Resume()` calls. The Argo Rollouts controller will re-connect to a fresh plugin process, and in-process state is lost.
**Instead:** Use `Measurement.Metadata` (metric plugin) or `RpcStepContext.Status` (step plugin) for all state that must survive across calls.

### Anti-Pattern 2: Single Binary for Both Plugin Types

**What:** Trying to make one binary that handles both metric and step plugin RPC.
**Why bad:** Argo Rollouts uses different `MagicCookieValue` for each plugin type. The handshake will fail. The controller starts separate processes and expects separate binaries.
**Instead:** Two binaries sharing a common `internal/` package.

### Anti-Pattern 3: Blocking in Run() Until Test Completes

**What:** Running a k6 test synchronously and blocking the `Run()` call for minutes.
**Why bad:** The RPC call will time out. The controller expects `Run()` to return quickly with a `Running` phase, then polls via `Resume()`.
**Instead:** Use the async Run/Resume pattern with `Measurement.ResumeAt`.

### Anti-Pattern 4: Hardcoding Metric Names

**What:** Only supporting "error_rate" and "p95_latency" as metric names.
**Why bad:** k6 has dozens of built-in metrics plus custom metrics. Users need to specify which metric they care about.
**Instead:** Accept the metric name as a config parameter and look it up dynamically in the run results.

### Anti-Pattern 5: Creating the Provider in Every Run() Call

**What:** Instantiating a new HTTP client and provider on every `Run()` call.
**Why bad:** Wasteful, slow, and prevents connection reuse.
**Instead:** Create the provider once in `InitPlugin()` and reuse it. `InitPlugin()` is called exactly once when the controller starts the plugin process.

## Build Order (Dependency Chain)

The following order reflects implementation dependencies -- each layer depends on the one before it:

```
Phase 1: Foundation
  [1] internal/provider/provider.go        -- Provider interface + types
  [2] internal/provider/config.go          -- Shared config types
  [3] internal/provider/cloud/types.go     -- Grafana Cloud API types
  [4] internal/provider/cloud/client.go    -- HTTP client (no provider logic)
  [5] internal/provider/cloud/provider.go  -- Provider implementation

Phase 2: Metric Plugin
  [6] internal/metric/plugin.go            -- MetricProviderPlugin impl
  [7] cmd/metric-plugin/main.go            -- Binary entrypoint

Phase 3: Step Plugin
  [8] internal/step/plugin.go              -- StepPlugin impl
  [9] cmd/step-plugin/main.go              -- Binary entrypoint

Phase 4: Testing & Release
  [10] Unit tests for all packages
  [11] RPC integration tests (go-plugin test harness)
  [12] e2e tests (kind cluster + controller + plugin binaries)
  [13] Makefile, goreleaser, GitHub Actions
```

**Rationale:** The provider layer is independent of the plugin system -- it can be developed and tested against the Grafana Cloud k6 API in isolation. The metric plugin is the primary use case (composable with other metrics) and should ship first. The step plugin reuses the provider and can be built quickly after.

## Scalability Considerations

| Concern | At 1 AnalysisRun | At 100 Concurrent | Notes |
|---------|-------------------|-------------------|-------|
| k6 Cloud API rate limits | No issue | May need rate limiting | Grafana Cloud k6 has API rate limits; add backoff/retry |
| Plugin process memory | Minimal (~10MB) | Same (stateless) | State lives in CR, not process memory |
| Concurrent runs | Single test run | 100 parallel API calls | HTTP client with connection pooling handles this |
| Resume polling interval | 10s default | 10s * 100 = trivial | Controller manages scheduling, not the plugin |

## Integration Test Architecture

```
kind cluster
  |
  +-- argo-rollouts controller (deployed via manifests)
  |     |
  |     +-- loads metric-plugin binary from file:// path
  |     +-- loads step-plugin binary from file:// path
  |
  +-- test application (nginx or rollouts-demo)
  |
  +-- AnalysisTemplate CRs (referencing plugin)
  |
  +-- Rollout CRs (triggering analysis)

Test harness (Go test):
  1. Create kind cluster
  2. Deploy argo-rollouts controller
  3. Mount plugin binaries into controller pod (or serve via HTTP)
  4. Deploy test workloads
  5. Apply Rollout with AnalysisTemplate
  6. Assert AnalysisRun completes with expected phase
  7. Assert Rollout progresses or rolls back as expected
```

For unit and integration tests that don't need a real Grafana Cloud k6 account, mock the `Provider` interface.

## Sources

- [Argo Rollouts Plugin Documentation](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- Plugin system overview
- [Argo Rollouts Step Plugin Documentation](https://argoproj.github.io/argo-rollouts/features/canary/plugins/) -- Step plugin lifecycle and requeue
- [Argo Rollouts Metric Plugin Sample (argoproj-labs)](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus) -- Reference implementation
- [Argo Rollouts Step Plugin Sample (in-tree)](https://github.com/argoproj/argo-rollouts/tree/master/test/cmd/step-plugin-sample) -- Reference implementation with state management
- [Argo Rollouts Plugin Types (pkg.go.dev)](https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types) -- RpcStepContext, RpcStepResult, RpcMetricProvider
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin) -- Plugin framework documentation
- [Grafana Cloud k6 Cloud REST API](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/) -- Test runs and metrics endpoints
- [Grafana Cloud k6 Test Status Codes](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/deprecated-rest-api/cloud-test-status-codes/) -- Run status state machine
- Source code verified: `metricproviders/plugin/client/client.go` (MagicCookieValue: "metricprovider"), `rollout/steps/plugin/client/client.go` (MagicCookieValue: "step") in argoproj/argo-rollouts at v1.9.x
