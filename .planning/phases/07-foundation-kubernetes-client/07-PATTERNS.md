# Phase 7: Foundation & Kubernetes Client - Pattern Map

**Mapped:** 2026-04-15
**Files analyzed:** 7 new/modified files
**Analogs found:** 7 / 7

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/provider/router.go` | service | request-response | `internal/provider/cloud/cloud.go` | role-match |
| `internal/provider/router_test.go` | test | request-response | `internal/provider/cloud/cloud_test.go` | role-match |
| `internal/provider/operator/operator.go` | service | CRUD | `internal/provider/cloud/cloud.go` | exact |
| `internal/provider/operator/operator_test.go` | test | CRUD | `internal/provider/cloud/cloud_test.go` | exact |
| `internal/provider/config.go` | model | transform | `internal/provider/config.go` (self) | exact |
| `cmd/metric-plugin/main.go` | config | request-response | `cmd/metric-plugin/main.go` (self) | exact |
| `cmd/step-plugin/main.go` | config | request-response | `cmd/step-plugin/main.go` (self) | exact |

## Pattern Assignments

### `internal/provider/router.go` (service, request-response)

**Analog:** `internal/provider/cloud/cloud.go`

**Imports pattern** (lines 1-7):
```go
package provider

import (
	"context"
	"fmt"
	"log/slog"
)
```
Note: Router lives in the `provider` package (same as Provider interface and PluginConfig), not in a sub-package. No external dependencies needed.

**Compile-time interface check pattern** (cloud.go line 17):
```go
// Compile-time interface check.
var _ provider.Provider = (*GrafanaCloudProvider)(nil)
```
Apply as:
```go
// Compile-time interface check.
var _ Provider = (*Router)(nil)
```

**Functional options constructor pattern** (cloud.go lines 25-41):
```go
// Option configures a GrafanaCloudProvider.
type Option func(*GrafanaCloudProvider)

// WithBaseURL overrides the default API base URL (for testing).
func WithBaseURL(url string) Option {
	return func(p *GrafanaCloudProvider) {
		p.baseURL = url
	}
}

// NewGrafanaCloudProvider creates a new Grafana Cloud k6 provider.
func NewGrafanaCloudProvider(opts ...Option) *GrafanaCloudProvider {
	p := &GrafanaCloudProvider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}
```
Apply as: `RouterOption func(*Router)` with `WithProvider(name string, p Provider)` and `NewRouter(opts ...RouterOption)`.

**Provider method delegation pattern** (cloud.go lines 63-92):
```go
func (p *GrafanaCloudProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	// ... validation ...
	// ... core logic ...
	slog.Debug("triggered test run",
		"testId", cfg.TestID,
		"runId", runID,
		"provider", p.Name(),
	)
	return runID, nil
}
```
Router applies this as: resolve provider from `cfg.Provider`, delegate call, log with resolved provider name.

**Error handling pattern** (cloud.go lines 64-67, 82-84):
```go
if err != nil || testID <= 0 || testID > math.MaxInt32 {
	return "", fmt.Errorf("invalid testId %q: must be a positive integer <= %d", cfg.TestID, math.MaxInt32)
}
// ...
if err != nil {
	return "", fmt.Errorf("trigger run for test %s: %w", cfg.TestID, err)
}
```
Router error pattern: `fmt.Errorf("unknown provider %q", name)` for unknown providers, pass-through for delegate errors.

**Name() pattern** (cloud.go lines 44-46):
```go
func (p *GrafanaCloudProvider) Name() string {
	return "grafana-cloud-k6"
}
```
Router: return `"router"` for interface contract.

---

### `internal/provider/router_test.go` (test, request-response)

**Analog:** `internal/provider/cloud/cloud_test.go` + `internal/metric/metric_test.go`

**Imports pattern** (cloud_test.go lines 1-13):
```go
package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```
Router tests use `package provider` (same package), import `providertest` for mock, `testify/assert`, `testify/require`.

**Mock provider usage pattern** (metric_test.go lines 18-19, 88-96):
```go
// mockProvider is a type alias for the shared mock, keeping test callsites concise.
type mockProvider = providertest.MockProvider

// In test:
mock := &mockProvider{
	TriggerRunFn: func(_ context.Context, cfg *provider.PluginConfig) (string, error) {
		triggered = true
		assert.Equal(t, "42", cfg.TestID)
		return "run-999", nil
	},
}
```
Router tests create multiple named mock providers and register them via `WithProvider`.

**Test helper pattern** (cloud_test.go lines 15-21):
```go
func defaultConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		TestID:   "42",
		APIToken: "test-token",
		StackID:  "12345",
	}
}
```
Router tests need a helper that sets the `Provider` field on PluginConfig.

**Test naming convention** (cloud_test.go):
```
TestTriggerRun_Success
TestTriggerRun_InvalidTestID
TestTriggerRun_APIError
TestGetRunResult_Running
TestStopRun_Success
```
Router tests follow: `TestRouter_DispatchToCloud`, `TestRouter_DispatchToOperator`, `TestRouter_DefaultProvider`, `TestRouter_UnknownProvider`.

---

### `internal/provider/operator/operator.go` (service, CRUD)

**Analog:** `internal/provider/cloud/cloud.go`

**Imports pattern** (cloud.go lines 1-14):
```go
package cloud

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	k6 "github.com/grafana/k6-cloud-openapi-client-go/k6"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)
```
Operator imports: `context`, `fmt`, `log/slog`, `sync`, `k8s.io/client-go/kubernetes`, `k8s.io/client-go/rest`, `k8s.io/apimachinery/pkg/apis/meta/v1`, `github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider`.

**Compile-time interface check** (cloud.go line 17):
```go
var _ provider.Provider = (*GrafanaCloudProvider)(nil)
```
Apply as:
```go
var _ provider.Provider = (*K6OperatorProvider)(nil)
```

**Struct + functional options pattern** (cloud.go lines 19-41):
```go
type GrafanaCloudProvider struct {
	baseURL string
}

type Option func(*GrafanaCloudProvider)

func WithBaseURL(url string) Option {
	return func(p *GrafanaCloudProvider) {
		p.baseURL = url
	}
}

func NewGrafanaCloudProvider(opts ...Option) *GrafanaCloudProvider {
	p := &GrafanaCloudProvider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}
```
Apply as: `K6OperatorProvider` struct with `clientOnce sync.Once`, `client kubernetes.Interface`, `clientErr error`. Options: `WithClient(kubernetes.Interface)`.

**Name() pattern** (cloud.go lines 44-46):
```go
func (p *GrafanaCloudProvider) Name() string {
	return "grafana-cloud-k6"
}
```
Apply as: `return "k6-operator"`.

**Error formatting pattern** (cloud.go lines 64-67, 82-84):
```go
return "", fmt.Errorf("invalid testId %q: must be a positive integer <= %d", cfg.TestID, math.MaxInt32)
return "", fmt.Errorf("trigger run for test %s: %w", cfg.TestID, err)
```
Apply for ConfigMap errors: `fmt.Errorf("get configmap %s/%s: %w", ns, name, err)`.

**slog structured logging pattern** (cloud.go lines 86-91):
```go
slog.Debug("triggered test run",
	"testId", cfg.TestID,
	"runId", runID,
	"provider", p.Name(),
)
```
Apply for ConfigMap reading: `slog.Info("k6 script loaded from configmap", "configmap", name, "key", key, "scriptLen", len(script))`.

**Stub method pattern** -- TriggerRun, GetRunResult, StopRun return "not yet implemented" errors in Phase 7 after validating config and reading script.

---

### `internal/provider/operator/operator_test.go` (test, CRUD)

**Analog:** `internal/provider/cloud/cloud_test.go`

**Imports pattern** (cloud_test.go lines 1-13):
```go
package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```
Operator tests use `package operator`, import:
- `context`, `testing`
- `k8s.io/client-go/kubernetes/fake`
- `corev1 "k8s.io/api/core/v1"`
- `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`
- `github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider`
- `github.com/stretchr/testify/assert`
- `github.com/stretchr/testify/require`

**Test helper pattern** (cloud_test.go lines 15-21):
```go
func defaultConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		TestID:   "42",
		APIToken: "test-token",
		StackID:  "12345",
	}
}
```
Apply as helper that returns a PluginConfig with `ConfigMapRef`, `Namespace` set for k6-operator.

**httptest server pattern** (cloud_test.go lines 29-66) -- not applicable for operator tests. Instead, use fake clientset pattern from RESEARCH.md:
```go
cm := &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "k6-scripts",
		Namespace: "test-ns",
	},
	Data: map[string]string{
		"load-test.js": "import http from 'k6/http'; ...",
	},
}
fakeClient := fake.NewSimpleClientset(cm)
p := NewK6OperatorProvider(WithClient(fakeClient))
```

**Test naming convention** (cloud_test.go):
```
TestTriggerRun_Success
TestTriggerRun_InvalidTestID
TestTriggerRun_APIError
```
Apply as: `TestReadScript_Success`, `TestReadScript_NotFound`, `TestReadScript_MissingKey`, `TestReadScript_EmptyContent`, `TestEnsureClient_WithInjectedClient`, `TestName`.

**require vs assert pattern** (cloud_test.go lines 69-70):
```go
require.NoError(t, err)       // stops test immediately on failure
assert.Equal(t, "99", runID)  // continues to check remaining assertions
```

---

### `internal/provider/config.go` (model, transform) -- MODIFIED

**Analog:** `internal/provider/config.go` (self, current state)

**Current file** (lines 1-13):
```go
package provider

// PluginConfig holds configuration parsed from the AnalysisTemplate
// plugin config JSON. Passed to every provider method call (stateless pattern per D-06).
type PluginConfig struct {
	TestRunID   string `json:"testRunId,omitempty"`
	TestID      string `json:"testId"`
	APIToken    string `json:"apiToken"`
	StackID     string `json:"stackId"`
	Timeout     string `json:"timeout,omitempty"`
	Metric      string `json:"metric"`                // thresholds|http_req_failed|http_req_duration|http_reqs
	Aggregation string `json:"aggregation,omitempty"` // p50|p95|p99 (for http_req_duration only)
}
```

**Extension pattern** -- add new fields with `omitempty` tags, grouped by comment blocks. Add `ConfigMapRef` struct in same file:
```go
// Provider routing
Provider string `json:"provider,omitempty"` // "grafana-cloud" (default) or "k6-operator"

// k6-operator fields
ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"`
Namespace    string        `json:"namespace,omitempty"`
```

**JSON tag convention** -- all fields use `json:"camelCase"` with `omitempty` for optional fields. Existing Grafana Cloud fields (`testId`, `apiToken`, `stackId`) do NOT have `omitempty` but are conceptually Grafana-Cloud-specific. Per D-04, leave them as-is for backward compat; new fields use `omitempty`.

---

### `cmd/metric-plugin/main.go` (config, request-response) -- MODIFIED

**Analog:** `cmd/metric-plugin/main.go` (self, current state)

**Current wiring** (lines 31-37):
```go
// Create provider and metric plugin implementation.
var opts []cloud.Option
if baseURL := os.Getenv("K6_BASE_URL"); baseURL != "" {
	opts = append(opts, cloud.WithBaseURL(baseURL))
}
p := cloud.NewGrafanaCloudProvider(opts...)
impl := metric.New(p)
```

**New wiring pattern** per D-03:
```go
// Create providers.
var cloudOpts []cloud.Option
if baseURL := os.Getenv("K6_BASE_URL"); baseURL != "" {
	cloudOpts = append(cloudOpts, cloud.WithBaseURL(baseURL))
}
cloudProvider := cloud.NewGrafanaCloudProvider(cloudOpts...)
operatorProvider := operator.NewK6OperatorProvider()

// Create router and wire to metric plugin.
router := provider.NewRouter(
	provider.WithProvider("grafana-cloud", cloudProvider),
	provider.WithProvider("k6-operator", operatorProvider),
)
impl := metric.New(router)
```

**Import additions:**
```go
"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/operator"
```

**Logging pattern** (lines 28-29):
```go
setupLogging()
slog.Info("starting metric plugin", "version", version)
```
No change needed -- Router is transparent to the plugin binary's logging.

---

### `cmd/step-plugin/main.go` (config, request-response) -- MODIFIED

**Analog:** `cmd/step-plugin/main.go` (self, current state)

**Current wiring** (lines 31-37) -- identical pattern to metric-plugin:
```go
var opts []cloud.Option
if baseURL := os.Getenv("K6_BASE_URL"); baseURL != "" {
	opts = append(opts, cloud.WithBaseURL(baseURL))
}
p := cloud.NewGrafanaCloudProvider(opts...)
impl := step.New(p)
```

**New wiring** -- same Router pattern as metric-plugin, replacing `step.New(p)` with `step.New(router)`.

---

## Shared Patterns

### Compile-time Interface Check
**Source:** `internal/provider/cloud/cloud.go` line 17
**Apply to:** `router.go`, `operator/operator.go`
```go
var _ provider.Provider = (*GrafanaCloudProvider)(nil)
```

### Functional Options Constructor
**Source:** `internal/provider/cloud/cloud.go` lines 25-41
**Apply to:** `router.go` (RouterOption + WithProvider), `operator/operator.go` (Option + WithClient)
```go
type Option func(*GrafanaCloudProvider)

func WithBaseURL(url string) Option {
	return func(p *GrafanaCloudProvider) {
		p.baseURL = url
	}
}

func NewGrafanaCloudProvider(opts ...Option) *GrafanaCloudProvider {
	p := &GrafanaCloudProvider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}
```

### Structured Logging
**Source:** `internal/provider/cloud/cloud.go` lines 86-91, 124-129, 213-217
**Apply to:** `router.go` (dispatch logging), `operator/operator.go` (client init, ConfigMap read)
```go
slog.Debug("triggered test run",
	"testId", cfg.TestID,
	"runId", runID,
	"provider", p.Name(),
)
slog.Info("polled test run status",
	"runId", runID,
	"statusType", statusType,
	"state", state,
	"provider", p.Name(),
)
slog.Warn("failed to stop run during terminate",
	"runId", state.RunID,
	"error", err,
)
```
Convention: Debug for routine operations, Info for state transitions, Warn for non-fatal errors. Always include `"provider"` field.

### Error Wrapping
**Source:** `internal/provider/cloud/cloud.go` lines 82-84
**Apply to:** All new files
```go
return "", fmt.Errorf("trigger run for test %s: %w", cfg.TestID, err)
```
Convention: `fmt.Errorf("action descriptor: %w", err)` -- context prefix + wrapped error.

### Test Mock Pattern
**Source:** `internal/provider/providertest/mock.go` lines 14-41
**Apply to:** `router_test.go`
```go
type MockProvider struct {
	TriggerRunFn   func(ctx context.Context, cfg *provider.PluginConfig) (string, error)
	GetRunResultFn func(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error)
	StopRunFn      func(ctx context.Context, cfg *provider.PluginConfig, runID string) error
}

func (m *MockProvider) Name() string { return "mock" }

func (m *MockProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	if m.TriggerRunFn != nil {
		return m.TriggerRunFn(ctx, cfg)
	}
	return "mock-run-123", nil
}
```
Router tests register multiple mocks with different names via `WithProvider`.

### Test Assertion Pattern
**Source:** `internal/provider/cloud/cloud_test.go` lines 69-71
**Apply to:** All test files
```go
require.NoError(t, err)       // fail-fast on setup errors
assert.Equal(t, "99", runID)  // soft assertion on expected values
assert.Contains(t, err.Error(), "invalid testId")  // error message checks
```

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | -- | -- | All files have close analogs in the existing codebase |

## Metadata

**Analog search scope:** `internal/provider/`, `internal/metric/`, `internal/step/`, `cmd/*/`, `internal/provider/providertest/`
**Files scanned:** 12 Go source files + 5 Go test files
**Pattern extraction date:** 2026-04-15
