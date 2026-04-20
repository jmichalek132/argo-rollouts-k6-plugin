# Contributing to argo-rollouts-k6-plugin

## Development Setup

### Prerequisites

- Go 1.24+
- golangci-lint v2

### Build

```bash
# Build both plugins
make build

# Build individual plugins
make build-metric
make build-step
```

Binaries are output to `bin/metric-plugin` and `bin/step-plugin`. All builds use `CGO_ENABLED=0` for static linking.

### Test

```bash
make test
```

Runs `go test -race -v -count=1 ./...` across all packages.

### Lint

```bash
make lint
```

Runs `golangci-lint run` (configured via `.golangci.yml`) and the `lint-stdout` target that checks for accidental stdout usage in non-test code.

### Clean

```bash
make clean
```

Removes the `bin/` directory.

## Project Structure

```
cmd/metric-plugin/           Metric plugin binary entrypoint
cmd/step-plugin/             Step plugin binary entrypoint
internal/metric/             Metric plugin RPC implementation
internal/step/               Step plugin RPC implementation
internal/provider/           Provider interface, Router multiplexer, shared types
internal/provider/cloud/     Grafana Cloud k6 provider
internal/provider/operator/  k6-operator provider (TestRun CRD, dynamic client,
                             ConfigMap script, handleSummary parsing)
e2e/                         Kind-cluster e2e test suite
examples/                    Example YAML manifests (Cloud + k6-operator)
```

## Provider Interface

The plugin uses a `Provider` interface (`internal/provider/provider.go`) to abstract k6 execution backends. Two providers ship in v0.3.0:

- `grafana-cloud` — trigger runs via the Grafana Cloud k6 REST API.
- `k6-operator` — create `k6.io/v1alpha1` TestRun CRs in-cluster, load the k6 script from a ConfigMap, and parse results from runner pod logs.

The `Router` (`internal/provider/router.go`) is a `Provider` implementation that multiplexes to the concrete backend based on the `provider` field in plugin config. Adding a new backend means implementing the `Provider` interface and registering the type with the Router — no changes to `metric.go`/`step.go` are needed.

### Interface Definition

```go
type Provider interface {
    TriggerRun(ctx context.Context, cfg *PluginConfig) (runID string, err error)
    GetRunResult(ctx context.Context, cfg *PluginConfig, runID string) (*RunResult, error)
    StopRun(ctx context.Context, cfg *PluginConfig, runID string) error
    Name() string
}
```

### Method Contracts

**`TriggerRun(ctx, cfg) (runID, error)`**

Start a new k6 test run. Return a unique run identifier as a string. The run ID is persisted in measurement metadata (metric plugin) or step state (step plugin) and passed to subsequent `GetRunResult` and `StopRun` calls.

**`GetRunResult(ctx, cfg, runID) (*RunResult, error)`**

Return the current status and metrics for a run. Return `RunState == Running` for active runs. Return a terminal state (`Passed`, `Failed`, `Errored`, `Aborted`) when the run completes. Populate the `RunResult` fields:

- `ThresholdsPassed` -- whether all k6 thresholds passed
- `HTTPReqFailed` -- fraction of failed HTTP requests (0.0 to 1.0)
- `HTTPReqDuration` -- percentile values in milliseconds (P50, P95, P99)
- `HTTPReqs` -- requests per second

**`StopRun(ctx, cfg, runID) error`**

Request cancellation of a running test. Must be idempotent -- calling `StopRun` on an already-terminal run must be a no-op, not an error.

**`Name() string`**

Return a provider identifier string used in log messages (e.g., `"grafana-cloud-k6"`).

### RunResult Type

```go
type RunResult struct {
    State            RunState    // Running, Passed, Failed, Errored, Aborted
    TestRunURL       string      // URL to the test run dashboard
    ThresholdsPassed bool
    HTTPReqFailed    float64     // 0.0-1.0
    HTTPReqDuration  Percentiles // {P50, P95, P99} in milliseconds
    HTTPReqs         float64     // requests/second
}
```

## PluginConfig

Both plugins parse configuration from `PluginConfig` (`internal/provider/config.go`):

```go
type PluginConfig struct {
    TestRunID   string `json:"testRunId,omitempty"`
    TestID      string `json:"testId"`
    APIToken    string `json:"apiToken"`
    StackID     string `json:"stackId"`
    Timeout     string `json:"timeout,omitempty"`
    Metric      string `json:"metric"`
    Aggregation string `json:"aggregation,omitempty"`
}
```

| Field | Required by | Description |
|-------|------------|-------------|
| `testId` | Both (unless `testRunId` set) | Grafana Cloud k6 test ID to trigger |
| `testRunId` | Neither (optional) | Existing run ID for poll-only mode |
| `apiToken` | Both | Grafana Cloud API token |
| `stackId` | Both | Grafana Cloud stack ID |
| `metric` | Metric plugin | Metric type: `thresholds`, `http_req_failed`, `http_req_duration`, `http_reqs` |
| `aggregation` | Metric plugin (for `http_req_duration`) | Percentile: `p50`, `p95`, `p99` |
| `timeout` | Step plugin | Max wait duration (e.g., `10m`). Default: 5m, max: 2h |

The table above covers the Grafana Cloud provider. The k6-operator provider (v0.3.0) adds `provider`, `configMapRef`, `namespace`, `parallelism`, `runnerImage`, `env`, `arguments`, and `resources`. See `internal/provider/config.go` for the full struct and `examples/k6-operator/` for worked examples.

## Error Conventions

- **Validation errors** (bad config, missing fields): Return `PhaseFailed` with a descriptive message. The analysis or step fails cleanly.
- **Infrastructure errors** (API unreachable, serialization failure): Return `RpcError`. The controller treats this as a plugin-level error.

This distinction matters: `PhaseFailed` is a normal analysis outcome (the gate rejected the deployment). `RpcError` indicates the plugin itself is broken.

## Wiring a New Provider

To add a new k6 execution backend (e.g., in-cluster Kubernetes Jobs):

### 1. Implement the Provider interface

Create `internal/provider/myprovider/myprovider.go`:

```go
package myprovider

import (
    "context"
    "github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

var _ provider.Provider = (*MyProvider)(nil)

type MyProvider struct {
    // provider-specific fields
}

func NewMyProvider() *MyProvider {
    return &MyProvider{}
}

func (p *MyProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
    // Start a test run, return the run ID
}

func (p *MyProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
    // Poll run status and populate all RunResult fields
}

func (p *MyProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
    // Cancel the run (idempotent)
}

func (p *MyProvider) Name() string {
    return "my-provider"
}
```

### 2. Write tests

Use `net/http/httptest` to mock your backend's HTTP API. See `internal/provider/cloud/cloud_test.go` as a reference for the pattern.

### 3. Register with the Router

`cmd/metric-plugin/main.go` and `cmd/step-plugin/main.go` each wire the concrete providers into a `provider.Router`, which dispatches per-call based on the `provider` field in plugin config. Adding a new backend is one line per binary:

```go
cloudProvider := cloud.NewGrafanaCloudProvider(cloudOpts...)
operatorProvider := operator.NewK6OperatorProvider()
myProvider := myprovider.NewMyProvider()

router := provider.NewRouter(
    provider.WithProvider("grafana-cloud", cloudProvider),
    provider.WithProvider("k6-operator",   operatorProvider),
    provider.WithProvider("my-provider",   myProvider), // register here
)
impl := metric.New(router) // or step.New(router)
```

Users then opt into your backend by setting `provider: "my-provider"` in their plugin config. `grafana-cloud` stays the default when no `provider` field is set (backward compatibility).

### 4. Build and test

```bash
make build
make test
```

## Stdout Rule

All plugin output MUST go to stderr via `log/slog`. Stdout is reserved for the `go-plugin` handshake protocol between the Argo Rollouts controller and the plugin binary. Writing anything to stdout before or during the handshake will break plugin loading.

The linter enforces this: `golangci-lint` is configured with `forbidigo` to flag `fmt.Print*` and `os.Stdout` usage. The `make lint` target also runs a grep-based check as a backup.
