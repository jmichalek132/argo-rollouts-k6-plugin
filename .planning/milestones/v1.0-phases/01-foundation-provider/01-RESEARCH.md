# Phase 1: Foundation & Provider - Research

**Researched:** 2026-04-09
**Domain:** Go module scaffolding, hashicorp/go-plugin binary stubs, Grafana Cloud k6 provider implementation, CGO-disabled static builds
**Confidence:** HIGH

## Summary

Phase 1 establishes the Go module, two compilable binary entrypoints, the Provider interface, and a fully tested Grafana Cloud k6 implementation. Research verified all key dependencies, resolved the auth header ambiguity (Bearer, not Token), mapped the v6 API surface for test run lifecycle operations, and discovered a critical Go version constraint: argo-rollouts v1.9.0 requires Go 1.24.9+, not Go 1.22 as originally planned.

The k6-cloud-openapi-client-go library covers trigger (`LoadTestsStart`), status poll (`TestRunsRetrieve`), and abort (`TestRunsAbort`) operations through its v6 API (`/cloud/v6/`). Metric and threshold endpoints are NOT exposed by this client, confirming the need for hand-rolled net/http calls in Phase 2. The library's Git tags lack the `v` prefix, requiring a pseudo-version in go.mod (`v0.0.0-20251022100644-dd6cfbb68f85`).

**Primary recommendation:** Use Go 1.24+ in go.mod (not 1.22), pin argo-rollouts at v1.9.0, use go-plugin v1.6.3 (matching argo-rollouts), and import k6-cloud-openapi-client-go via pseudo-version. Build stubs that call `plugin.Serve()` as the very first action in `main()` with zero stdout before it.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Unified interface -- 4 methods only: `Name() string`, `TriggerRun(ctx, cfg)`, `GetRunResult(ctx, cfg, runID)`, `StopRun(ctx, cfg, runID)`
- **D-02:** `GetRunResult` returns a `RunResult` struct with ALL metrics always populated (not split into lifecycle vs metrics methods)
- **D-03:** Partial/live metrics -- `GetRunResult` returns current metric values even during active runs (State == Running). Metric plugin returns live measurements each poll cycle; Argo Rollouts evaluates thresholds incrementally, enabling early abort.
- **D-04:** `RunResult` struct fields: `State RunState`, `TestRunURL string`, `ThresholdsPassed bool`, `HTTPReqFailed float64` (0.0-1.0), `HTTPReqDuration Percentiles` (p50/p95/p99 ms), `HTTPReqs float64` (req/s)
- **D-05:** `RunState` enum: `Running`, `Passed`, `Failed`, `Errored`, `Aborted` -- covers all Grafana Cloud k6 terminal states
- **D-06:** Stateless provider -- credentials are NOT injected at construction time
- **D-07:** Each provider method receives `*PluginConfig` (parsed from AnalysisTemplate plugin config JSON per Run call). Provider creates an authenticated HTTP client per call using config.APIToken and config.StackID.
- **D-08:** `PluginConfig` struct: `TestRunID string`, `TestID string`, `APIToken string`, `StackID string`, `Timeout string` -- JSON tags `testRunId`, `testId`, `apiToken`, `stackId`, `timeout`
- **D-09:** Use official `k6-cloud-openapi-client-go` for test run lifecycle (trigger, status, stop)
- **D-10:** Fall back to hand-rolled `net/http` calls for metric/threshold endpoints (v5 API) if the Go client does not expose them
- **D-11:** Verify auth header format (`Token <key>` vs `Bearer <key>`) during Phase 1 implementation
- **D-12:** `log/slog` (Go stdlib) -- structured JSON logging, outputs to stderr
- **D-13:** Log level controlled via environment variable `LOG_LEVEL` (default: `info`)
- **D-14:** Go module: `github.com/jmichalek132/argo-rollouts-k6-plugin`
- **D-15:** Go version: 1.22 (matches Argo Rollouts v1.9.x toolchain) -- **SUPERSEDED by research: must be 1.24.9+**
- **D-16:** Build tooling: Makefile with targets: `make build`, `make test`, `make lint`, `make clean`
- **D-17:** Package layout: `cmd/metric-plugin/main.go`, `cmd/step-plugin/main.go`, `internal/provider/provider.go` (interface + types), `internal/provider/cloud/cloud.go` (Grafana Cloud impl)

### Claude's Discretion
- Mock HTTP server approach for provider unit tests (httptest.NewServer or testify mock)
- Internal error type design (wrapped stdlib errors vs typed sentinel errors)
- go.mod dependency management (direct vs replace directives)
- Exact Makefile structure beyond the four required targets
- Whether to use `context.Background()` or pass through ctx from plugin lifecycle

### Deferred Ideas (OUT OF SCOPE)
None -- discussion stayed within Phase 1 scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PLUG-04 | Provider abstraction interface with TriggerRun, GetRunResult, StopRun, Name | Provider interface design verified; stateless per-call pattern with PluginConfig |
| PROV-01 | Authenticate to Grafana Cloud k6 API using API token + stack ID | Auth resolved: `Authorization: Bearer <token>` + `X-Stack-Id: <int>` via k6.ContextAccessToken |
| PROV-02 | Trigger a k6 test run by test ID and return test run ID | `client.LoadTestsAPI.LoadTestsStart(ctx, int32(testID)).XStackId(int32(stackID)).Execute()` returns `*TestRunApiModel` |
| PROV-03 | Poll test run status to determine running vs terminal state | `client.TestRunsAPI.TestRunsRetrieve(ctx, int32(runID)).XStackId(int32(stackID)).Execute()` -- status field values and result mapping documented |
| PROV-04 | Stop a running k6 test run when requested | `client.TestRunsAPI.TestRunsAbort(ctx, int32(runID)).XStackId(int32(stackID)).Execute()` |
| DIST-01 | Two statically linked Go binaries with CGO_ENABLED=0 | Makefile build flags documented; verified Go 1.26.1 available on build machine |
| DIST-04 | All plugin output to stderr only -- stdout reserved for go-plugin handshake | go-plugin Serve() prints handshake to stdout then redirects; zero output before Serve() is mandatory |
</phase_requirements>

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | 1.24.9+ (use 1.24 in go.mod) | Language runtime | argo-rollouts v1.9.0 go.mod declares `go 1.24.9`; go-plugin v1.6.3 declares `go 1.21`. Module consumers must satisfy the highest. System has Go 1.26.1 installed. |
| `github.com/argoproj/argo-rollouts` | v1.9.0 | Plugin interface types (Phase 2/3), CRD types | Latest stable (2026-03-20). Provides `metricproviders/plugin/rpc` and `rollout/steps/plugin/rpc` RPC wrappers. Required even in Phase 1 for go-plugin handshake constants. |
| `github.com/hashicorp/go-plugin` | v1.6.3 | Plugin host framework | Matches the version used by argo-rollouts v1.9.0. v1.7.0 also works but gains nothing for Phase 1. |
| `github.com/grafana/k6-cloud-openapi-client-go` | v0.0.0-20251022100644-dd6cfbb68f85 | Grafana Cloud k6 API v6 client | Official auto-generated client. Tags lack `v` prefix so Go modules requires pseudo-version. Corresponds to tag `1.7.0-0.1.0` (2025-10-22). |
| `log/slog` | stdlib (Go 1.21+) | Structured logging to stderr | Locked decision D-12. No external dependency needed. |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.11.1 | Test assertions and require | Unit tests for provider and config parsing |
| `encoding/json` | stdlib | Config JSON parsing | Parsing PluginConfig from AnalysisTemplate |
| `net/http/httptest` | stdlib | Mock HTTP server for tests | All provider unit tests |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| log/slog | logrus (Argo Rollouts convention) | slog is stdlib, zero dependency, locked by D-12. logrus is ecosystem convention but adds a dependency. |
| go-plugin v1.6.3 | go-plugin v1.7.0 | v1.7.0 requires Go 1.24, gains no features needed here. v1.6.3 matches argo-rollouts. |
| Pseudo-version for k6 client | go get with commit hash | Same thing -- pseudo-version is what `go mod tidy` resolves to |

### Critical: Go Version Correction

**D-15 specified Go 1.22 but this is impossible.** Verified dependency requirements:

| Dependency | go.mod `go` directive | Minimum Go |
|------------|----------------------|------------|
| `github.com/argoproj/argo-rollouts` v1.9.0 | `go 1.24.9` | 1.24.9 |
| `github.com/hashicorp/go-plugin` v1.7.0 | `go 1.24` | 1.24.0 |
| `github.com/hashicorp/go-plugin` v1.6.3 | `go 1.21` | 1.21.0 |
| `github.com/grafana/k6-cloud-openapi-client-go` | `go 1.23.4` | 1.23.4 |

**Resolution:** Use `go 1.24` in go.mod. The system has Go 1.26.1 installed, which satisfies all requirements. Using `go 1.24` (not `go 1.24.9`) in go.mod provides maximum backward compatibility while satisfying argo-rollouts.

**Installation:**
```bash
# No package install needed -- all Go modules, fetched by go mod tidy
# go.mod require block:
# github.com/argoproj/argo-rollouts v1.9.0
# github.com/hashicorp/go-plugin v1.6.3
# github.com/grafana/k6-cloud-openapi-client-go v0.0.0-20251022100644-dd6cfbb68f85
# github.com/stretchr/testify v1.11.1
```

## Architecture Patterns

### Recommended Project Structure (Phase 1 Only)
```
argo-rollouts-k6-plugin/
  cmd/
    metric-plugin/
      main.go              # Stub: handshake + Serve() only, no plugin logic yet
    step-plugin/
      main.go              # Stub: handshake + Serve() only, no plugin logic yet
  internal/
    provider/
      provider.go          # Provider interface + RunResult + RunState + Percentiles types
      config.go            # PluginConfig struct with JSON tags
      cloud/
        cloud.go           # GrafanaCloudProvider implementing Provider
        cloud_test.go      # Unit tests with httptest mock server
        types.go           # Mapping types between k6 API model and Provider types
  go.mod
  go.sum
  Makefile
  .golangci.yml
```

### Pattern 1: Minimal go-plugin Binary Stub (Phase 1 Entrypoints)

**What:** Phase 1 binary stubs that compile and satisfy the go-plugin handshake without implementing any plugin logic (that's Phase 2/3).

**When to use:** Phase 1 -- the binaries must compile and produce no stdout before Serve().

**Example:**
```go
// cmd/metric-plugin/main.go
package main

import (
    "log/slog"
    "os"

    goPlugin "github.com/hashicorp/go-plugin"
)

// handshakeConfig must match the controller's config exactly.
// Source: github.com/argoproj/argo-rollouts/metricproviders/plugin/client/client.go
var handshakeConfig = goPlugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
    MagicCookieValue: "metricprovider",
}

func main() {
    // All logging to stderr -- stdout is reserved for go-plugin handshake.
    logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))
    slog.SetDefault(logger)

    // Serve() prints handshake to stdout, then redirects os.Stdout to a pipe.
    // NOTHING must write to stdout before this line.
    goPlugin.Serve(&goPlugin.ServeConfig{
        HandshakeConfig: handshakeConfig,
        Plugins:         map[string]goPlugin.Plugin{
            // Phase 2 adds: "RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{Impl: impl},
        },
    })
}
```

```go
// cmd/step-plugin/main.go
package main

import (
    "log/slog"
    "os"

    goPlugin "github.com/hashicorp/go-plugin"
)

var handshakeConfig = goPlugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
    MagicCookieValue: "step",
}

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))
    slog.SetDefault(logger)

    goPlugin.Serve(&goPlugin.ServeConfig{
        HandshakeConfig: handshakeConfig,
        Plugins:         map[string]goPlugin.Plugin{
            // Phase 3 adds: "RpcStepPlugin": &stepRpc.RpcStepPlugin{Impl: impl},
        },
    })
}
```

**Confidence: HIGH** -- Verified from hashicorp/go-plugin source: `Serve()` does `fmt.Printf("%s\n", protocolLine)` then `os.Stdout = stdout_w` to redirect. Empty plugin map is valid (binary compiles, handshake works, but no plugins are dispensed).

### Pattern 2: Stateless Provider with Per-Call Auth

**What:** Provider methods receive `*PluginConfig` on every call. The provider creates an authenticated HTTP client using the config's APIToken and StackID each time.

**When to use:** Phase 1 Grafana Cloud provider. This aligns with D-06/D-07 (stateless provider).

**Example:**
```go
// internal/provider/cloud/cloud.go
func (p *GrafanaCloudProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
    client := p.newK6Client(cfg)
    testID, err := strconv.ParseInt(cfg.TestID, 10, 32)
    if err != nil {
        return "", fmt.Errorf("invalid testId %q: %w", cfg.TestID, err)
    }
    stackID, err := strconv.ParseInt(cfg.StackID, 10, 32)
    if err != nil {
        return "", fmt.Errorf("invalid stackId %q: %w", cfg.StackID, err)
    }

    ctx = context.WithValue(ctx, k6.ContextAccessToken, cfg.APIToken)
    testRun, _, err := client.LoadTestsAPI.LoadTestsStart(ctx, int32(testID)).
        XStackId(int32(stackID)).
        Execute()
    if err != nil {
        return "", fmt.Errorf("trigger run for test %s: %w", cfg.TestID, err)
    }
    return strconv.FormatInt(int64(testRun.GetId()), 10), nil
}
```

### Pattern 3: httptest Mock Server for Provider Unit Tests

**What:** Use Go stdlib `httptest.NewServer` with `http.ServeMux` to mock the Grafana Cloud k6 API v6 endpoints in unit tests.

**When to use:** All provider unit tests in Phase 1.

**Example:**
```go
// internal/provider/cloud/cloud_test.go
func TestTriggerRun(t *testing.T) {
    mux := http.NewServeMux()
    mux.HandleFunc("POST /cloud/v6/load_tests/{id}/start", func(w http.ResponseWriter, r *http.Request) {
        // Verify auth header
        assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
        assert.Equal(t, "12345", r.Header.Get("X-Stack-Id"))

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
            "id":      99,
            "test_id": 42,
            "status":  "created",
        })
    })
    server := httptest.NewServer(mux)
    defer server.Close()

    p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
    cfg := &provider.PluginConfig{
        TestID:   "42",
        APIToken: "test-token",
        StackID:  "12345",
    }
    runID, err := p.TriggerRun(context.Background(), cfg)
    require.NoError(t, err)
    assert.Equal(t, "99", runID)
}
```

**Note:** Go 1.22+ `http.ServeMux` supports method-based routing (`"POST /path/{param}"`), making this pattern clean without external routers.

### Anti-Patterns to Avoid

- **Any stdout before Serve():** `fmt.Println`, `log.Println` (default log writes to stderr, but verify), `os.Stdout.Write` -- all corrupt the go-plugin handshake.
- **Creating HTTP client per field access:** Create one `*k6.APIClient` per provider method call (per PluginConfig), reuse for all API calls within that method.
- **Hardcoding API base URL:** Always accept a `BaseURL` override for testing. The k6 client supports `cfg.Servers = []k6.ServerConfiguration{{URL: baseURL}}`.
- **Using int for test/run IDs in Provider interface:** Use `string` -- the Provider interface is provider-agnostic; other providers may use UUIDs or non-numeric IDs.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| k6 Cloud API client | Custom HTTP wrapper for trigger/status/stop | `k6-cloud-openapi-client-go` | Auto-generated from OpenAPI spec, type-safe, handles request building, maintained by Grafana |
| go-plugin handshake | Custom IPC protocol | `hashicorp/go-plugin` | Argo Rollouts mandates it; handles process lifecycle, RPC transport, handshake |
| JSON config parsing | Custom parser | `encoding/json` stdlib | Standard Go approach, PluginConfig struct with json tags |
| Test HTTP server | Custom TCP listener for tests | `net/http/httptest` stdlib | Purpose-built for exactly this; handles port allocation, TLS, cleanup |

**Key insight:** The k6-cloud-openapi-client-go covers the test run lifecycle (trigger, poll, abort) but does NOT cover metric query endpoints (aggregate metrics, thresholds). Phase 2 will need hand-rolled net/http for those. Phase 1 only needs lifecycle operations.

## Auth Header Resolution (D-11)

**Resolved: `Authorization: Bearer <token>`**

**Confidence: HIGH** -- verified in k6-cloud-openapi-client-go source code (`client.go`):

```go
localVarRequest.Header.Add("Authorization", "Bearer "+auth)
```

The client uses `k6.ContextAccessToken` context key to inject the Bearer token. The `X-Stack-Id` header is passed as a method parameter (integer):

```go
client.LoadTestsAPI.LoadTestsStart(ctx, testID).XStackId(stackID).Execute()
```

**Important:** `X-Stack-Id` is an `int32`, and `TestID`/`RunID` are also `int32` in the OpenAPI client. The Provider interface uses `string` for these (D-08), so the cloud provider must convert between `string` and `int32`.

**Previous research ambiguity:** The PITFALLS.md and CONTEXT.md mentioned `Token <key>` format. This was based on the deprecated v2 API (`api.k6.io/loadtests/v2/`). The current v6 API uses `Bearer <key>`. Since we use the official v6 client, this is handled automatically.

## RunState Mapping (v6 API to Provider Interface)

### v6 API Status Values (from StatusApiModel.Type)

The v6 API uses string status values, NOT the legacy numeric codes:

| v6 API `status_details.type` | Description | Terminal? |
|------------------------------|-------------|-----------|
| `created` | Test run created, not yet validated | No |
| `queued` | Validated and waiting for load generators | No |
| `initializing` | Assigned to load generators, not yet sending traffic | No |
| `running` | Actively executing the test | No |
| `processing_metrics` | Test execution done, aggregating metrics | No |
| `completed` | Fully done, metrics available | Yes |
| `aborted` | Stopped before completion (check `extra.message` for reason) | Yes |

### v6 API Result Values (from TestRunApiModel.Result)

| v6 API `result` | Description | Available When |
|------------------|-------------|----------------|
| `null` | Run not yet finished | status != completed |
| `"passed"` | All k6 thresholds passed | status == completed |
| `"failed"` | One or more thresholds breached | status == completed |
| `"error"` | Execution error (script crash, infra issue) | status == completed or aborted |

### Mapping to Provider RunState (D-05)

```go
func mapToRunState(statusType string, result *string) provider.RunState {
    switch statusType {
    case "created", "queued", "initializing", "running", "processing_metrics":
        return provider.Running
    case "completed":
        if result == nil {
            return provider.Errored // should not happen, but defensive
        }
        switch *result {
        case "passed":
            return provider.Passed
        case "failed":
            return provider.Failed
        case "error":
            return provider.Errored
        default:
            return provider.Errored
        }
    case "aborted":
        return provider.Aborted
    default:
        return provider.Errored
    }
}
```

### Legacy Numeric Status Codes (for reference only)

The deprecated v2 API uses numeric `run_status` values (from `go.k6.io/k6/cloudapi`). These are NOT used by the v6 client but documented for context:

| Code | Name | Maps To |
|------|------|---------|
| -2 | RunStatusCreated | Running |
| -1 | RunStatusValidated | Running |
| 0 | RunStatusQueued | Running |
| 1 | RunStatusInitializing | Running |
| 2 | RunStatusRunning | Running |
| 3 | RunStatusFinished | Passed/Failed (check thresholds) |
| 4 | RunStatusTimedOut | Errored |
| 5 | RunStatusAbortedUser | Aborted |
| 6 | RunStatusAbortedSystem | Errored |
| 7 | RunStatusAbortedScriptError | Errored |
| 8 | RunStatusAbortedThreshold | Failed |
| 9 | RunStatusAbortedLimit | Errored |

## k6-cloud-openapi-client-go API Surface (Phase 1 Relevant)

### Trigger Test Run
```go
// POST /cloud/v6/load_tests/{id}/start
testRun, httpResp, err := client.LoadTestsAPI.LoadTestsStart(ctx, testID).
    XStackId(stackID).
    Execute()
// Returns: *TestRunApiModel (contains Id, Status, StatusDetails, etc.)
```

### Poll Test Run Status
```go
// GET /cloud/v6/test_runs/{id}
testRun, httpResp, err := client.TestRunsAPI.TestRunsRetrieve(ctx, runID).
    XStackId(stackID).
    Execute()
// Returns: *TestRunApiModel
```

### Abort Test Run
```go
// POST /cloud/v6/test_runs/{id}/abort
httpResp, err := client.TestRunsAPI.TestRunsAbort(ctx, runID).
    XStackId(stackID).
    Execute()
// Returns: no body, just HTTP response
```

### Client Initialization
```go
import k6 "github.com/grafana/k6-cloud-openapi-client-go/k6"

cfg := k6.NewConfiguration()
// Override base URL for testing:
cfg.Servers = k6.ServerConfigurations{{URL: baseURL}}
client := k6.NewAPIClient(cfg)

// Auth via context (Bearer token):
ctx := context.WithValue(ctx, k6.ContextAccessToken, apiToken)
```

### Key Type: TestRunApiModel
```go
type TestRunApiModel struct {
    Id              int32                      // Test run ID
    TestId          int32                      // Parent test ID
    Status          string                     // Legacy status string
    StatusDetails   StatusApiModel             // Current status with type + entered time
    StatusHistory   []StatusApiModel           // All status transitions
    Result          NullableString             // "passed"/"failed"/"error" or null
    ResultDetails   map[string]interface{}     // Extra result info
    Created         time.Time
    Ended           NullableTime
    // ... more fields
}
```

### Metrics/Thresholds Endpoints: NOT Available

The k6-cloud-openapi-client-go does NOT expose:
- Aggregate metric queries (p50/p95/p99 latency, error rate, throughput)
- Threshold status queries

**Phase 2 action:** Hand-roll net/http calls to:
- `GET /cloud/v5/test_runs/{id}/query_aggregate_k6` -- aggregate metrics
- `GET /loadtests/v2/thresholds?test_run_id={id}` -- threshold pass/fail
- (Or discover if v6 has undocumented metric endpoints)

Phase 1 does NOT need these -- the Phase 1 `GetRunResult` implementation returns `RunResult.State` and `RunResult.ThresholdsPassed` from the test run status/result fields only.

## Makefile CGO_ENABLED=0 Build Targets

```makefile
MODULE := github.com/jmichalek132/argo-rollouts-k6-plugin
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := CGO_ENABLED=0

.PHONY: build build-metric build-step test lint clean

build: build-metric build-step

build-metric:
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o bin/metric-plugin ./cmd/metric-plugin

build-step:
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o bin/step-plugin ./cmd/step-plugin

test:
	go test -race -v ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/
```

**Key flags:**
- `CGO_ENABLED=0` -- produces static binary, no libc dependency
- `-ldflags "-s -w"` -- strips debug info and DWARF tables, reduces binary size ~30%
- `-race` on tests only (not builds -- race detector requires CGO)

**Note:** `go test -race` requires CGO. Either run tests with `CGO_ENABLED=1` (default) or skip `-race` in CGO-disabled environments. The Makefile `test` target should NOT set `CGO_ENABLED=0`.

## golangci-lint v2 Configuration

Latest version: v2.11.4 (2026-03-22). Not currently installed on this machine -- needs `go install` or binary download.

```yaml
# .golangci.yml
version: "2"

linters:
  default: standard
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    - typecheck
    - misspell
    - gocyclo
    - goconst
    - gosec
    - unconvert
    - bodyclose      # Catches unclosed HTTP response bodies
    - noctx          # Requires context.Context in HTTP requests
    - exhaustive     # Ensures switch on enum-like types is exhaustive

formatters:
  enable:
    - gofmt
    - goimports

run:
  go: "1.24"
  timeout: 5m

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
```

### Linters for stdout-before-Serve Pitfall

No built-in golangci-lint linter specifically detects `fmt.Print*` or `os.Stdout.Write` calls. To catch this:

1. **Add a Makefile lint target** that greps for stdout usage:
```makefile
lint-stdout:
	@if grep -rn 'fmt\.Print\|os\.Stdout' cmd/ internal/ --include='*.go' | grep -v '_test.go' | grep -v '// stdout-ok'; then \
		echo "ERROR: found stdout usage in non-test code"; exit 1; \
	fi
```

2. **Use `forbidigo` linter** (available in golangci-lint v2):
```yaml
linters:
  enable:
    - forbidigo
  settings:
    forbidigo:
      forbid:
        - pattern: "fmt\\.Print.*"
          msg: "Use slog (writes to stderr) instead of fmt.Print (writes to stdout)"
        - pattern: "os\\.Stdout"
          msg: "stdout is reserved for go-plugin handshake"
```

## Common Pitfalls

### Pitfall 1: stdout Before Serve() Corrupts Handshake
**What goes wrong:** Any output to stdout before `goPlugin.Serve()` corrupts the go-plugin handshake protocol. The controller sees unexpected data, fails to parse the connection info, and kills the plugin.
**Why it happens:** Serve() does `fmt.Printf("%s\n", protocolLine)` to stdout as its first action. Any prior stdout writes interleave with the protocol line.
**How to avoid:** Call `goPlugin.Serve()` as the absolute first action in `main()`. Only set up an slog handler (to stderr) before it. Use `forbidigo` linter to ban `fmt.Print*` and `os.Stdout`.
**Warning signs:** Plugin works in unit tests but fails when loaded by controller. Controller logs: "Unrecognized remote plugin message".

### Pitfall 2: Go Version Mismatch
**What goes wrong:** go.mod declares `go 1.22` but argo-rollouts v1.9.0 requires `go 1.24.9`. The build fails with "go: updates to go.mod needed".
**Why it happens:** The CONTEXT decided on Go 1.22 based on stale research. Argo Rollouts v1.9.0 was released 2026-03-20 with Go 1.24.9.
**How to avoid:** Use `go 1.24` in go.mod. Verified: Go 1.26.1 is installed on this machine.
**Warning signs:** `go mod tidy` errors, build failures.

### Pitfall 3: k6 Client Pseudo-Version
**What goes wrong:** `go get github.com/grafana/k6-cloud-openapi-client-go@v1.7.0-0.1.0` fails because the tag `1.7.0-0.1.0` lacks the `v` prefix required by Go modules.
**Why it happens:** Grafana's release tags are `1.7.0-0.1.0` not `v1.7.0-0.1.0`.
**How to avoid:** Use the pseudo-version: `go get github.com/grafana/k6-cloud-openapi-client-go@1.7.0-0.1.0` (Go resolves to `v0.0.0-20251022100644-dd6cfbb68f85`). Or use the commit hash directly.
**Warning signs:** `go get` returns "invalid version".

### Pitfall 4: int32 Type Mismatch for IDs
**What goes wrong:** The Provider interface uses `string` for TestID, RunID, StackID (D-08). The k6 OpenAPI client uses `int32`. Conversion failures at runtime.
**Why it happens:** The Provider interface is designed to be provider-agnostic (future providers may use UUIDs). The k6 API uses numeric IDs.
**How to avoid:** Parse string-to-int32 with `strconv.ParseInt` in the cloud provider, validate early, return clear errors for non-numeric IDs.
**Warning signs:** Runtime panics from invalid int32 conversion.

### Pitfall 5: CGO_ENABLED=0 on Tests
**What goes wrong:** Running `CGO_ENABLED=0 go test -race ./...` fails because the race detector requires CGO.
**Why it happens:** `-race` uses C code for instrumentation.
**How to avoid:** Only set `CGO_ENABLED=0` for build targets, not test targets. Makefile `test` target uses default CGO (enabled).
**Warning signs:** "race: can't enable race detector: must be compiled with cgo" error.

## Code Examples

### go.mod (Phase 1)
```go
module github.com/jmichalek132/argo-rollouts-k6-plugin

go 1.24

require (
    github.com/argoproj/argo-rollouts v1.9.0
    github.com/grafana/k6-cloud-openapi-client-go v0.0.0-20251022100644-dd6cfbb68f85
    github.com/hashicorp/go-plugin v1.6.3
    github.com/stretchr/testify v1.11.1
)
```

**Note:** `go mod tidy` will add indirect dependencies (k8s client-go, api-machinery, etc. from argo-rollouts). The go.sum will be large. This is expected.

### Provider Interface (internal/provider/provider.go)
```go
package provider

import "context"

// RunState represents the state of a test run from the provider's perspective.
type RunState string

const (
    Running RunState = "Running"
    Passed  RunState = "Passed"
    Failed  RunState = "Failed"
    Errored RunState = "Errored"
    Aborted RunState = "Aborted"
)

// IsTerminal returns true if the run has reached a final state.
func (s RunState) IsTerminal() bool {
    return s != Running
}

// Percentiles holds HTTP request duration percentile values in milliseconds.
type Percentiles struct {
    P50 float64
    P95 float64
    P99 float64
}

// RunResult holds the outcome of a test run query.
type RunResult struct {
    State            RunState
    TestRunURL       string
    ThresholdsPassed bool
    HTTPReqFailed    float64      // 0.0-1.0
    HTTPReqDuration  Percentiles  // milliseconds
    HTTPReqs         float64      // req/s
}

// Provider defines the interface for k6 execution backends.
type Provider interface {
    // TriggerRun starts a new k6 test run. Returns the run ID.
    TriggerRun(ctx context.Context, cfg *PluginConfig) (runID string, err error)

    // GetRunResult returns current status and metrics for a run.
    // Returns partial metrics during active runs (State == Running).
    GetRunResult(ctx context.Context, cfg *PluginConfig, runID string) (*RunResult, error)

    // StopRun requests cancellation of a running test.
    // No-op if the run is already in a terminal state.
    StopRun(ctx context.Context, cfg *PluginConfig, runID string) error

    // Name returns the provider identifier for logging.
    Name() string
}
```

### PluginConfig (internal/provider/config.go)
```go
package provider

// PluginConfig holds configuration parsed from the AnalysisTemplate
// plugin config JSON. Passed to every provider method call.
type PluginConfig struct {
    TestRunID string `json:"testRunId,omitempty"`
    TestID    string `json:"testId"`
    APIToken  string `json:"apiToken"`
    StackID   string `json:"stackId"`
    Timeout   string `json:"timeout,omitempty"`
}
```

### slog Setup Pattern
```go
func setupLogging() {
    levelStr := os.Getenv("LOG_LEVEL")
    var level slog.Level
    switch strings.ToLower(levelStr) {
    case "debug":
        level = slog.LevelDebug
    case "warn", "warning":
        level = slog.LevelWarn
    case "error":
        level = slog.LevelError
    default:
        level = slog.LevelInfo
    }
    handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
    slog.SetDefault(slog.New(handler))
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| k6 API v2 (`/loadtests/v2/`) | k6 API v6 (`/cloud/v6/`) | 2025 | v2 is deprecated; v6 is auto-generated from OpenAPI spec |
| `Token <key>` auth header | `Bearer <key>` auth header | v6 API | Official client uses Bearer; Token was v2-era |
| Numeric run_status codes (-2..10) | String status types (created/running/completed/aborted) | v6 API | Simpler to work with; legacy codes still exist in k6 OSS codebase |
| logrus (Argo Rollouts convention) | log/slog (stdlib) | Go 1.21+ | slog is zero-dependency; locked by D-12 |
| golangci-lint v1 config | golangci-lint v2 config (`version: "2"`) | 2025-03 | Breaking config change: `enable-all`/`disable-all` replaced by `linters.default` |
| go-plugin v1.4.x | go-plugin v1.6.3 (argo-rollouts v1.9.0) | 2025 | API compatible; improved multiplexing support |

## Open Questions

1. **Phase 1 GetRunResult metric fields**
   - What we know: Phase 1 only has lifecycle APIs (status, result). Metrics (p50/p95/p99, http_req_failed, http_reqs) require endpoints not in the v6 client.
   - What's unclear: Should Phase 1 `GetRunResult` return zero-valued metrics and only populate State + ThresholdsPassed? Or should it omit metrics entirely until Phase 2?
   - Recommendation: Return zero-valued metrics in Phase 1. Populate `State` from status/result mapping, `ThresholdsPassed` from `Result == "passed"`. Leave `HTTPReqFailed`, `HTTPReqDuration`, `HTTPReqs` at zero. Document this as Phase 2 enhancement. This lets Phase 2 metric plugin use the provider immediately for threshold-only gates.

2. **k6 API rate limits**
   - What we know: Grafana Cloud has API rate limits.
   - What's unclear: Exact limits for v6 endpoints.
   - Recommendation: Add a `baseURL` config option and test with mock server. Rate limit handling (retry with backoff) can be added in Phase 2 when polling is implemented.

3. **go-plugin v1.6.3 vs v1.7.0**
   - What we know: argo-rollouts v1.9.0 requires v1.6.3. Our module can use v1.7.0 (Go's MVS would upgrade).
   - What's unclear: Whether v1.7.0 has breaking changes affecting the RPC path.
   - Recommendation: Pin v1.6.3 to match argo-rollouts exactly. Less risk, no benefit from v1.7.0 for this project.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | Build, test, all | Yes | 1.26.1 | -- |
| make | Makefile build targets | Yes | (system) | -- |
| golangci-lint | `make lint` | No | -- | `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4` |
| goreleaser | Release builds (Phase 4) | No | -- | Not needed Phase 1 |

**Missing dependencies with no fallback:**
- None for Phase 1.

**Missing dependencies with fallback:**
- golangci-lint: install via `go install` as part of Makefile `lint` target or setup step.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing stdlib + testify v1.11.1 |
| Config file | None needed (go test convention) |
| Quick run command | `go test -race -v -short ./...` |
| Full suite command | `go test -race -v -count=1 ./...` |

### Phase Requirements to Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PLUG-04 | Provider interface compiles, all methods defined | unit (compile) | `go build ./internal/provider/...` | Wave 0 |
| PROV-01 | Auth with Bearer token + X-Stack-Id header | unit | `go test -run TestAuth ./internal/provider/cloud/` | Wave 0 |
| PROV-02 | Trigger test run returns run ID | unit | `go test -run TestTriggerRun ./internal/provider/cloud/` | Wave 0 |
| PROV-03 | Poll status maps to correct RunState | unit | `go test -run TestGetRunResult ./internal/provider/cloud/` | Wave 0 |
| PROV-04 | Stop running test run | unit | `go test -run TestStopRun ./internal/provider/cloud/` | Wave 0 |
| DIST-01 | CGO_ENABLED=0 builds succeed | build | `CGO_ENABLED=0 go build ./cmd/metric-plugin && CGO_ENABLED=0 go build ./cmd/step-plugin` | Wave 0 |
| DIST-04 | No stdout before handshake | lint | `make lint-stdout` | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -race -v -short ./...`
- **Per wave merge:** `go test -race -v -count=1 ./...` + `make build`
- **Phase gate:** Full suite green + both binaries compile with CGO_ENABLED=0

### Wave 0 Gaps
- [ ] `internal/provider/cloud/cloud_test.go` -- covers PROV-01, PROV-02, PROV-03, PROV-04
- [ ] Makefile `lint-stdout` target -- covers DIST-04
- [ ] `.golangci.yml` -- linter configuration

## Sources

### Primary (HIGH confidence)
- [k6-cloud-openapi-client-go source: client.go](https://github.com/grafana/k6-cloud-openapi-client-go) -- Bearer auth verified in `prepareRequest` method
- [k6-cloud-openapi-client-go source: api_test_runs.go](https://github.com/grafana/k6-cloud-openapi-client-go) -- TestRunsRetrieve, TestRunsAbort API surface
- [k6-cloud-openapi-client-go source: api_load_tests.go](https://github.com/grafana/k6-cloud-openapi-client-go) -- LoadTestsStart API surface
- [k6-cloud-openapi-client-go source: model_test_run_api_model.go](https://github.com/grafana/k6-cloud-openapi-client-go) -- TestRunApiModel fields and types
- [k6-cloud-openapi-client-go source: model_status_api_model.go](https://github.com/grafana/k6-cloud-openapi-client-go) -- StatusApiModel.Type values: created/queued/initializing/running/processing_metrics/completed/aborted
- [hashicorp/go-plugin source: server.go](https://github.com/hashicorp/go-plugin) -- Serve() stdout handshake protocol verified
- [argo-rollouts source: metricproviders/plugin/client/client.go](https://github.com/argoproj/argo-rollouts) -- MagicCookieValue "metricprovider" verified
- [argo-rollouts source: rollout/steps/plugin/client/client.go](https://github.com/argoproj/argo-rollouts) -- MagicCookieValue "step" verified
- [go.k6.io/k6/cloudapi pkg.go.dev](https://pkg.go.dev/go.k6.io/k6/cloudapi) -- Legacy numeric RunStatus codes (-2 through 10)
- Go module proxy verification -- argo-rollouts v1.9.0 requires `go 1.24.9`, go-plugin v1.6.3 requires `go 1.21`

### Secondary (MEDIUM confidence)
- [Grafana Cloud k6 REST API docs](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/) -- API overview, auth format
- [golangci-lint v2 configuration](https://golangci-lint.run/docs/configuration/file/) -- v2 YAML structure, linters.default
- [golangci-lint releases](https://github.com/golangci/golangci-lint/releases) -- v2.11.4 latest

### Tertiary (LOW confidence)
- Metric/threshold endpoint availability in v6 -- not verified against live API, inferred from absence in client library

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all versions verified against Go module proxy and source code
- Architecture: HIGH -- patterns verified from hashicorp/go-plugin source and argo-rollouts source
- Auth format: HIGH -- verified in k6-cloud-openapi-client-go client.go source: `"Bearer " + auth`
- RunState mapping: HIGH -- StatusApiModel.Type values verified from source; Result values from TestRunApiModel
- Pitfalls: HIGH -- stdout/handshake verified from go-plugin server.go source
- Go version: HIGH -- verified from go.mod of each dependency via module proxy

**Research date:** 2026-04-09
**Valid until:** 2026-05-09 (stable dependencies, unlikely to change in 30 days)
