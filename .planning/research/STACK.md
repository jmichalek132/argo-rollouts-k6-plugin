# Technology Stack

**Project:** argo-rollouts-k6-plugin
**Researched:** 2026-04-09

## Recommended Stack

### Core Framework

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Go | 1.23+ | Plugin language | Argo Rollouts plugins communicate via `net/rpc` (gob encoding) -- must be Go. The controller's go.mod uses Go 1.26.1, but the plugin is a separate binary; Go 1.23 is the minimum to match the k6 client library |
| `github.com/argoproj/argo-rollouts` | v1.9.0 | Plugin interface types | Latest stable (released 2026-03-20). Provides `v1alpha1` CRD types, `utils/plugin/types` (RpcMetricProvider, RpcStep interfaces), and `metricproviders/plugin/rpc` / `rollout/steps/plugin/rpc` RPC wrappers |
| `github.com/hashicorp/go-plugin` | v1.7.0 | Plugin host framework | Argo Rollouts v1.9.0 uses this version. Provides the process management, handshake, and `net/rpc` transport between controller and plugin binary |
| `github.com/grafana/k6-cloud-openapi-client-go` | v1.7.0-0.1.0 | Grafana Cloud k6 API client | Official auto-generated Go client for the k6 Cloud REST API v6. Type-safe, covers all endpoints (LoadTests, TestRuns, Metrics). Released 2025-10-22 |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/sirupsen/logrus` | v1.9.x | Structured logging | All plugin logging. Convention from every official Argo Rollouts plugin sample |
| `encoding/json` | stdlib | Config parsing | Parsing `metric.Provider.Plugin["plugin-name"]` config from AnalysisTemplate and `context.Config` from Rollout step |
| `github.com/stretchr/testify` | v1.9.x | Test assertions | Unit and integration tests |
| `sigs.k8s.io/e2e-framework` | v0.4.x | E2E test framework | Integration tests against kind cluster |

### Infrastructure

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| kind | latest | Local K8s for e2e tests | Standard for Argo Rollouts plugin testing; lightweight, runs in CI |
| GoReleaser | latest | Cross-compilation + release | Build static binaries for linux/amd64 and linux/arm64, generate SHA256 checksums, publish to GitHub Releases |
| GitHub Actions | N/A | CI/CD | Build, test, release pipeline |

## Plugin Communication Architecture

### How It Works

The Argo Rollouts controller starts the plugin as a **child process** and communicates over **`net/rpc`** (not gRPC) via a Unix socket managed by `hashicorp/go-plugin`. This means:

1. Plugin must be a standalone Go binary (not a shared library, not a container)
2. Communication uses Go's `encoding/gob` for serialization
3. The controller discovers the plugin via the `argo-rollouts-config` ConfigMap
4. Controller downloads the binary (HTTP/HTTPS) or reads from a local path (`file://`)
5. Controller verifies SHA256 checksum if provided
6. Controller starts the binary and connects via the handshake protocol

### Handshake Configuration

The controller uses **different** `MagicCookieValue` for each plugin type. Verified from controller source:
- `metricproviders/plugin/client/client.go`: `MagicCookieValue: "metricprovider"`
- `rollout/steps/plugin/client/client.go`: `MagicCookieValue: "step"`

**Metric plugin:**
```go
var handshakeConfig = goPlugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
    MagicCookieValue: "metricprovider",
}
```

**Step plugin:**
```go
var handshakeConfig = goPlugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
    MagicCookieValue: "step",
}
```

### Plugin Map Keys

- Metric plugin: `"RpcMetricProviderPlugin"`
- Step plugin: `"RpcStepPlugin"`

## Two Binaries, One Module

**Recommendation: Two separate binaries with shared internal packages.**

The controller starts separate processes for metric and step plugins with different `MagicCookieValue`. While a single binary could detect the mode via the `ARGO_ROLLOUTS_RPC_PLUGIN` environment variable, two binaries is the cleaner approach:

- Explicit: each binary has one purpose, no runtime dispatch logic
- Matches the convention from all official Argo Rollouts plugin samples
- Separate binary names make ConfigMap entries unambiguous
- GoReleaser builds both from the same module trivially

```
cmd/
  metric-plugin/main.go   # Binary 1: serves RpcMetricProviderPlugin
  step-plugin/main.go     # Binary 2: serves RpcStepPlugin
internal/
  provider/               # Shared: Provider interface + Grafana Cloud implementation
  metric/                 # MetricProviderPlugin implementation
  step/                   # StepPlugin implementation
```

ConfigMap references two different binaries:
```yaml
metricProviderPlugins: |-
  - name: "org/k6"
    location: "https://.../metric-plugin-linux-amd64"
stepPlugins: |-
  - name: "org/k6-step"
    location: "https://.../step-plugin-linux-amd64"
```

## Metric Plugin Interface

**Confidence: HIGH** -- verified from Argo Rollouts v1.9.0 source code at `utils/plugin/types/types.go` and `metricproviders/plugin/rpc/rpc.go`.

The metric plugin must implement `MetricProviderPlugin` which embeds `RpcMetricProvider`:

```go
// In metricproviders/plugin/rpc/rpc.go
type MetricProviderPlugin interface {
    InitPlugin() types.RpcError
    types.RpcMetricProvider
}

// In utils/plugin/types/types.go
type RpcMetricProvider interface {
    // Run starts a new measurement. Should be idempotent.
    Run(*v1alpha1.AnalysisRun, v1alpha1.Metric) v1alpha1.Measurement

    // Resume checks if an in-progress measurement is complete.
    Resume(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement

    // Terminate stops an in-progress measurement.
    Terminate(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement

    // GarbageCollect cleans up completed measurements to the specified limit.
    GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError

    // Type returns the provider type identifier string.
    Type() string

    // GetMetadata returns additional key-value metadata for display.
    GetMetadata(metric v1alpha1.Metric) map[string]string
}
```

### Key types:

```go
type RpcError struct {
    ErrorString string
}
func (e RpcError) HasError() bool  // true if ErrorString != ""
```

### Measurement lifecycle:

1. Controller calls `Run()` -- plugin returns `v1alpha1.Measurement` with `Phase` set to `AnalysisPhaseRunning` or a terminal phase
2. If `Phase == AnalysisPhaseRunning`, controller calls `Resume()` on each interval until terminal phase
3. Terminal phases: `AnalysisPhaseSuccessful`, `AnalysisPhaseFailed`, `AnalysisPhaseError`
4. Controller evaluates `successCondition` / `failureCondition` against `Measurement.Value`
5. State between Run/Resume is persisted via `Measurement.Metadata` (map[string]string)

### Config access pattern:

```go
// In your Run() implementation:
var config K6Config
err := json.Unmarshal(metric.Provider.Plugin["your-org/k6"], &config)
```

The key in the map matches the plugin name from the AnalysisTemplate YAML.

### main.go entry point (cmd/metric-plugin/main.go):

```go
package main

import (
    goPlugin "github.com/hashicorp/go-plugin"
    "github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
    "github.com/your-org/argo-rollouts-k6-plugin/internal/metric"
)

func main() {
    impl := metric.New() // your implementation
    goPlugin.Serve(&goPlugin.ServeConfig{
        HandshakeConfig: goPlugin.HandshakeConfig{
            ProtocolVersion:  1,
            MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
            MagicCookieValue: "metricprovider",
        },
        Plugins: map[string]goPlugin.Plugin{
            "RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{Impl: impl},
        },
    })
}
```

## Step Plugin Interface

**Confidence: HIGH** -- verified from Argo Rollouts v1.9.0 source code at `utils/plugin/types/types.go` and `rollout/steps/plugin/rpc/rpc.go`.

The step plugin must implement `StepPlugin` which embeds `RpcStep`:

```go
// In rollout/steps/plugin/rpc/rpc.go
type StepPlugin interface {
    InitPlugin() types.RpcError
    types.RpcStep
}

// In utils/plugin/types/types.go  (added in v1.8.0)
type RpcStep interface {
    // Run executes the step. May be called multiple times (must be idempotent).
    // Return PhaseRunning + RequeueAfter for polling-based workflows.
    Run(*v1alpha1.Rollout, *types.RpcStepContext) (types.RpcStepResult, types.RpcError)

    // Terminate stops an in-progress Run (e.g., rollout promoted while step running).
    Terminate(*v1alpha1.Rollout, *types.RpcStepContext) (types.RpcStepResult, types.RpcError)

    // Abort reverts Run actions. Called in reverse order for previously successful steps.
    Abort(*v1alpha1.Rollout, *types.RpcStepContext) (types.RpcStepResult, types.RpcError)

    // Type returns the plugin type identifier string.
    Type() string
}
```

### Key types:

```go
type RpcStepContext struct {
    PluginName string          // Plugin name as defined in the Rollout
    Config     json.RawMessage // User config from the Rollout step spec
    Status     json.RawMessage // Previous execution status (for state persistence)
}

type RpcStepResult struct {
    Phase        StepPhase       // Running, Successful, Failed, Error
    Message      string          // Human-readable status message
    RequeueAfter time.Duration   // When to call Run again (for polling)
    Status       json.RawMessage // Persisted state between executions
}

type StepPhase string
const (
    PhaseRunning    StepPhase = "Running"
    PhaseSuccessful StepPhase = "Successful"
    PhaseFailed     StepPhase = "Failed"
    PhaseError      StepPhase = "Error"
)
```

### Step plugin lifecycle (for k6 "fire-and-wait"):

1. Controller calls `Run()` -- plugin triggers k6 test, returns `PhaseRunning` + `RequeueAfter: 15s` + serialized run ID in `Status`
2. Controller calls `Run()` again after RequeueAfter -- plugin deserializes run ID from `context.Status`, polls k6 API for status
3. Repeat until k6 run completes, then return `PhaseSuccessful` or `PhaseFailed`
4. If rollout is promoted/aborted mid-test, controller calls `Terminate()` or `Abort()`

### main.go entry point (cmd/step-plugin/main.go):

```go
package main

import (
    goPlugin "github.com/hashicorp/go-plugin"
    stepRpc "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
    "github.com/your-org/argo-rollouts-k6-plugin/internal/step"
)

func main() {
    impl := step.New() // your implementation
    goPlugin.Serve(&goPlugin.ServeConfig{
        HandshakeConfig: goPlugin.HandshakeConfig{
            ProtocolVersion:  1,
            MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
            MagicCookieValue: "step",
        },
        Plugins: map[string]goPlugin.Plugin{
            "RpcStepPlugin": &stepRpc.RpcStepPlugin{Impl: impl},
        },
    })
}
```

## Grafana Cloud k6 REST API v6

**Confidence: HIGH** -- verified from `github.com/grafana/k6-cloud-openapi-client-go` source code, README, and Grafana docs.

### Base URL

`https://api.k6.io/cloud/v6/`

### Authentication

HTTP Bearer token via `Authorization: Bearer <token>` header, plus `X-Stack-Id: <stack-id>` header for all requests.

Token types:
- **Personal API token**: user-scoped, generated in Grafana Cloud UI under Testing & Synthetics > Performance > Settings > Access > Personal token
- **Stack API token**: service-account-scoped, not tied to a user

### Client initialization:

```go
import k6 "github.com/grafana/k6-cloud-openapi-client-go/k6"

cfg := k6.NewConfiguration()
client := k6.NewAPIClient(cfg)
ctx := context.WithValue(context.Background(), k6.ContextAccessToken, token)
```

### Key API operations for this plugin:

**Trigger a test run:**
```go
// LoadTestsStart starts a test in Grafana Cloud
testRun, httpRes, err := client.LoadTestsAPI.LoadTestsStart(ctx, testID).
    XStackId(stackID).
    Execute()
// Returns: *TestRunApiModel
```

**Poll test run status:**
```go
// TestRunsRetrieve gets a single test run
testRun, httpRes, err := client.TestRunsAPI.TestRunsRetrieve(ctx, runID).
    XStackId(stackID).
    Execute()
// Returns: *TestRunApiModel
```

**Abort a test run:**
```go
httpRes, err := client.TestRunsAPI.TestRunsAbort(ctx, runID).
    XStackId(stackID).
    Execute()
```

**List test runs for a load test:**
```go
runs, httpRes, err := client.TestRunsAPI.LoadTestsTestRunsRetrieve(ctx, testID).
    XStackId(stackID).
    Execute()
// Returns: *TestRunListResponse
```

### TestRunApiModel key fields:

```go
type TestRunApiModel struct {
    Id            int32
    TestId        int32
    ProjectId     int32
    Status        string           // "created", "running", "finished", "aborted", "timed_out"
    Result        NullableString   // "passed", "failed", "error" (null while running)
    ResultDetails map[string]interface{}
    Created       time.Time
    Ended         NullableTime
    StatusDetails StatusApiModel
    StatusHistory []StatusApiModel
    Options       map[string]interface{}
    // ...
}
```

### Test run status lifecycle:

```
created -> running -> finished (normal completion)
                   -> aborted  (user/API abort)
                   -> timed_out
```

### Result values (available after run completes):

| Result | Meaning |
|--------|---------|
| `"passed"` | All k6 thresholds passed |
| `"failed"` | One or more k6 thresholds breached |
| `"error"` | Execution error (script crash, infra issue) |

## Plugin Distribution

**Confidence: HIGH** -- verified from Argo Rollouts docs and multiple plugin examples.

### Controller ConfigMap (`argo-rollouts-config`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  metricProviderPlugins: |-
    - name: "your-org/k6"
      location: "https://github.com/your-org/argo-rollouts-k6-plugin/releases/download/v0.1.0/metric-plugin-linux-amd64"
      sha256: "abc123..."
  stepPlugins: |-
    - name: "your-org/k6-step"
      location: "https://github.com/your-org/argo-rollouts-k6-plugin/releases/download/v0.1.0/step-plugin-linux-amd64"
      sha256: "def456..."
```

### Plugin naming convention:

`<org>/<name>` -- e.g., `argoproj-labs/sample-prometheus`. Must match GitHub username/org + repo name regex.

### AnalysisTemplate referencing the metric plugin:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-error-rate
spec:
  args:
    - name: test-run-id
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
      successCondition: "result == \"passed\""
      failureLimit: 3
      provider:
        plugin:
          your-org/k6:
            testRunId: "{{ args.test-run-id }}"
            apiToken: "{{ args.k6-api-token }}"
            stackId: "{{ args.k6-stack-id }}"
            metric: "thresholds"
```

### Rollout referencing the step plugin:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: my-app
spec:
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: your-org/k6-step
            config:
              testId: "12345"
              apiToken: "{{ secrets.k6-cloud-credentials.api-token }}"
              stackId: "{{ secrets.k6-cloud-credentials.stack-id }}"
              timeout: "10m"
        - setWeight: 50
```

## Go Module Structure

**Confidence: MEDIUM** -- synthesized from sample plugins and Go best practices. No single official template exists.

```
argo-rollouts-k6-plugin/
  cmd/
    metric-plugin/
      main.go                    # Metric plugin binary entrypoint
    step-plugin/
      main.go                    # Step plugin binary entrypoint
  internal/
    provider/
      provider.go                # Provider interface definition
      config.go                  # Shared config types
      cloud/
        client.go                # Grafana Cloud k6 API wrapper
        client_test.go
        provider.go              # Implements Provider interface
        provider_test.go
        types.go                 # API response mapping types
    metric/
      plugin.go                  # MetricProviderPlugin implementation
      plugin_test.go
    step/
      plugin.go                  # StepPlugin implementation
      plugin_test.go
  test/
    e2e/
      suite_test.go              # kind cluster setup/teardown
      metric_plugin_test.go
      step_plugin_test.go
      testdata/
        analysistemplate.yaml
        rollout.yaml
        configmap.yaml
  examples/
    analysistemplate-thresholds.yaml
    analysistemplate-error-rate.yaml
    rollout-canary-step.yaml
    configmap.yaml
    secret.yaml
  go.mod
  go.sum
  Makefile
  .goreleaser.yaml
```

### go.mod:

```
module github.com/your-org/argo-rollouts-k6-plugin

go 1.23

require (
    github.com/argoproj/argo-rollouts v1.9.0
    github.com/grafana/k6-cloud-openapi-client-go v1.7.0-0.1.0
    github.com/hashicorp/go-plugin v1.7.0
    github.com/sirupsen/logrus v1.9.3
)

require (
    github.com/stretchr/testify v1.9.0
    sigs.k8s.io/e2e-framework v0.4.0
)
```

**Important:** The `argo-rollouts` dependency will pull in a large transitive dependency tree (Kubernetes client-go, API machinery, etc.). This is unavoidable -- the plugin binary will import `v1alpha1` CRD types. Plan for ~100MB+ binary size; static linking with `CGO_ENABLED=0` is required.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| k6 API client | `k6-cloud-openapi-client-go` | Raw `net/http` + manual JSON | Official client is auto-generated from OpenAPI spec, type-safe, handles auth, maintained by Grafana. No reason to DIY |
| Plugin protocol | `net/rpc` (as Argo Rollouts requires) | gRPC | Argo Rollouts uses `net/rpc` exclusively for plugins. `go-plugin` supports gRPC but Argo Rollouts does not use it. Must use `net/rpc` |
| Logging | logrus | zerolog, slog | Every Argo Rollouts plugin sample uses logrus. Consistency with ecosystem matters more than logger performance |
| Build tool | GoReleaser | Manual `go build` scripts | GoReleaser handles cross-compilation, checksums, GitHub Release uploads in one config file |
| Testing framework | testify + e2e-framework | Go stdlib testing | testify for assertions/mocking, e2e-framework for kind cluster lifecycle. Standard in the Kubernetes ecosystem |
| Binary count | Two binaries (metric + step) | Single binary with env var dispatch | Two binaries is explicit, matches Argo Rollouts conventions. Single binary requires runtime detection via `ARGO_ROLLOUTS_RPC_PLUGIN` env var which is an implementation detail of go-plugin, not a public API |

## What NOT to Use

| Technology | Why Not |
|------------|---------|
| gRPC for plugin communication | Argo Rollouts plugins use `net/rpc`, not gRPC. The `go-plugin` library supports both but Argo Rollouts only implements the `net/rpc` path. Using gRPC will silently fail |
| `k6` Go library directly | `go.k6.io/k6` is the k6 runtime. It does not provide a client for Grafana Cloud k6 API. The OpenAPI client is what you need for triggering cloud runs |
| Cobra/CLI framework | Plugin binary is started by the controller, not by users. No CLI needed. Only the `hashicorp/go-plugin` Serve() entrypoint |
| Kubernetes client-go directly | Plugin runs as a child process of the controller, not as a separate pod. It has no direct K8s API access and doesn't need client-go. The controller passes all needed data via RPC |
| Helm chart | Out of scope per PROJECT.md. The plugin is configured via ConfigMap, not deployed separately |
| Protobuf code generation | `net/rpc` uses `encoding/gob`, not protobuf. The RPC interfaces are already defined in the argo-rollouts module |

## Build & Release

### GoReleaser config (`.goreleaser.yaml`):

```yaml
builds:
  - id: metric-plugin
    main: ./cmd/metric-plugin/
    binary: metric-plugin
    env:
      - CGO_ENABLED=0
    goos:
      - linux
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}

  - id: step-plugin
    main: ./cmd/step-plugin/
    binary: step-plugin
    env:
      - CGO_ENABLED=0
    goos:
      - linux
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  github:
    owner: your-org
    name: argo-rollouts-k6-plugin
```

### SHA256 checksum:

The controller verifies the binary checksum at download time if `sha256` is specified in the ConfigMap. Always publish checksums with releases.

## Testing Strategy

### Unit tests:

- Mock the Provider interface to test metric plugin logic (Run/Resume/Terminate/GarbageCollect)
- Mock the Provider interface to test step plugin polling and state management (Run/Terminate/Abort)
- Test config parsing from `metric.Provider.Plugin` and `context.Config`
- Test provider state machine (status transitions, result extraction)

### RPC integration tests:

- Use `goPlugin.ServeTestConfig` for in-process plugin testing without starting a real binary
- Verify gob serialization round-trips correctly for all RPC argument types
- Test handshake with mock implementations

### E2E tests (kind cluster):

- Use `sigs.k8s.io/e2e-framework` to manage kind cluster lifecycle
- Install Argo Rollouts CRDs and controller
- Deploy the plugin binaries (file:// path)
- Create AnalysisTemplate + AnalysisRun, verify measurement results
- Create Rollout with step plugin, verify step execution
- Mock the k6 API with a local HTTP server for deterministic tests (no real Grafana Cloud account needed)

### CI pipeline:

```
lint (golangci-lint) -> unit tests -> build -> e2e tests (kind) -> release (goreleaser)
```

## Sources

- [Argo Rollouts Plugin Docs](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: metricproviders/plugin/rpc/rpc.go](https://github.com/argoproj/argo-rollouts/blob/master/metricproviders/plugin/rpc/rpc.go) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: rollout/steps/plugin/rpc/rpc.go](https://github.com/argoproj/argo-rollouts/blob/master/rollout/steps/plugin/rpc/rpc.go) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: utils/plugin/types/types.go](https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: metricproviders/plugin/client/client.go](https://github.com/argoproj/argo-rollouts/blob/master/metricproviders/plugin/client/client.go) -- HIGH confidence (handshake verification)
- [Argo Rollouts v1.9.0 source: rollout/steps/plugin/client/client.go](https://github.com/argoproj/argo-rollouts/blob/master/rollout/steps/plugin/client/client.go) -- HIGH confidence (handshake verification)
- [Argo Rollouts metrics plugin sample (Prometheus)](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus) -- HIGH confidence
- [Grafana k6-cloud-openapi-client-go](https://github.com/grafana/k6-cloud-openapi-client-go) -- HIGH confidence
- [Grafana Cloud k6 REST API docs](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/) -- HIGH confidence
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin) -- HIGH confidence
- [Argo Rollouts Canary Step Plugin docs](https://argoproj.github.io/argo-rollouts/features/canary/plugins/) -- HIGH confidence
- [Argo Rollouts Releases](https://github.com/argoproj/argo-rollouts/releases) -- HIGH confidence
