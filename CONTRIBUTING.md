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
cmd/metric-plugin/        Metric plugin binary entrypoint
cmd/step-plugin/          Step plugin binary entrypoint
internal/metric/          Metric plugin RPC implementation
internal/step/            Step plugin RPC implementation
internal/provider/        Provider interface and shared types
internal/provider/cloud/  Grafana Cloud k6 provider implementation
examples/                 Example YAML manifests
```

## Provider Interface

The plugin uses a `Provider` interface (`internal/provider/provider.go`) to abstract k6 execution backends. The current implementation targets Grafana Cloud k6, but the interface is designed for future backends (in-cluster k6 Jobs, direct binary execution).

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

### 3. Wire into the plugin binaries

In `cmd/metric-plugin/main.go`, replace (or conditionally select) the provider:

```go
// Before:
p := cloud.NewGrafanaCloudProvider()

// After (simple replacement):
p := myprovider.NewMyProvider()

// Or (conditional selection based on config/env):
var p provider.Provider
if os.Getenv("K6_PROVIDER") == "my-provider" {
    p = myprovider.NewMyProvider()
} else {
    p = cloud.NewGrafanaCloudProvider()
}
```

Do the same in `cmd/step-plugin/main.go`.

### 4. Build and test

```bash
make build
make test
```

## Stdout Rule

All plugin output MUST go to stderr via `log/slog`. Stdout is reserved for the `go-plugin` handshake protocol between the Argo Rollouts controller and the plugin binary. Writing anything to stdout before or during the handshake will break plugin loading.

The linter enforces this: `golangci-lint` is configured with `forbidigo` to flag `fmt.Print*` and `os.Stdout` usage. The `make lint` target also runs a grep-based check as a backup.
