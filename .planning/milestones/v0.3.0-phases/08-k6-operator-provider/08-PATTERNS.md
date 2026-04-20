# Phase 8: k6-operator Provider - Pattern Map

**Mapped:** 2026-04-15
**Files analyzed:** 7 new/modified files
**Analogs found:** 7 / 7

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/provider/operator/operator.go` (modify) | service | CRUD | `internal/provider/cloud/cloud.go` | exact |
| `internal/provider/operator/testrun.go` (create) | utility | transform | `internal/provider/cloud/types.go` | role-match |
| `internal/provider/operator/exitcode.go` (create) | utility | transform | `internal/provider/cloud/types.go` | exact |
| `internal/provider/config.go` (modify) | model | transform | `internal/provider/config.go` (self) | exact |
| `internal/provider/operator/operator_test.go` (modify) | test | CRUD | `internal/provider/cloud/cloud_test.go` | exact |
| `internal/provider/operator/testrun_test.go` (create) | test | transform | `internal/provider/cloud/metrics_test.go` | role-match |
| `internal/provider/operator/exitcode_test.go` (create) | test | transform | `internal/provider/cloud/metrics_test.go` | exact |

## Pattern Assignments

### `internal/provider/operator/operator.go` (service, CRUD -- MODIFY)

**Analog:** `internal/provider/cloud/cloud.go`

This is the primary implementation file. The Phase 7 stubs (TriggerRun, GetRunResult, StopRun) are replaced with real k8s API calls. The cloud provider is the exact analog because it implements the same Provider interface with the same three methods.

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

Phase 8 equivalent imports will replace the HTTP/k6-cloud imports with k8s dynamic client and k6-operator types. The internal provider import remains identical.

**Struct + functional options pattern** (cloud.go lines 20-42):
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

Phase 8 extends the existing operator.go pattern (lines 21-47) which already uses this exact convention. Add `dynClient dynamic.Interface` field and `WithDynClient` option alongside the existing `client` / `WithClient`.

**Existing operator struct to extend** (operator.go lines 21-47):
```go
type K6OperatorProvider struct {
	clientOnce sync.Once
	client     kubernetes.Interface
	clientErr  error
}

type Option func(*K6OperatorProvider)

func WithClient(c kubernetes.Interface) Option {
	return func(p *K6OperatorProvider) {
		p.client = c
		p.clientOnce.Do(func() {})
	}
}
```

**TriggerRun pattern** (cloud.go lines 63-92):
```go
func (p *GrafanaCloudProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
	// 1. Validate/parse input parameters
	testID, err := strconv.ParseInt(cfg.TestID, 10, 64)
	if err != nil || testID <= 0 || testID > math.MaxInt32 {
		return "", fmt.Errorf("invalid testId %q: must be a positive integer <= %d", cfg.TestID, math.MaxInt32)
	}

	// 2. Initialize client
	ctx, client := p.newK6Client(ctx, cfg)

	// 3. Execute the API call
	testRun, resp, err := client.LoadTestsAPI.LoadTestsStart(ctx, int32(testID)).
		XStackId(int32(stackID)).Execute()

	// 4. Handle response/error
	if err != nil {
		return "", fmt.Errorf("trigger run for test %s: %w", cfg.TestID, err)
	}

	// 5. Extract and return identifier
	runID := strconv.FormatInt(int64(testRun.GetId()), 10)
	slog.Debug("triggered test run", "testId", cfg.TestID, "runId", runID, "provider", p.Name())
	return runID, nil
}
```

Phase 8 TriggerRun follows this same 5-step structure: validate config, get client, build+create TestRun CR, handle error, return CR name as runID.

**GetRunResult pattern** (cloud.go lines 96-143):
```go
func (p *GrafanaCloudProvider) GetRunResult(ctx context.Context, cfg *provider.PluginConfig, runID string) (*provider.RunResult, error) {
	// 1. Parse/validate runID and config
	// 2. Initialize client
	// 3. Fetch current state (single API call per D-04)
	// 4. Map external status to provider.RunState
	state := mapToRunState(statusType, result)
	// 5. Build RunResult
	runResult := &provider.RunResult{
		State:            state,
		TestRunURL:       fmt.Sprintf("https://app.k6.io/runs/%s", runID),
		ThresholdsPassed: isThresholdPassed(result),
	}
	// 6. Conditional enrichment for terminal states
	if state == provider.Passed || state == provider.Failed {
		p.populateAggregateMetrics(ctx, cfg, runID, runResult)
	}
	return runResult, nil
}
```

Phase 8 follows the same structure. Step 3 is a dynamic client GET. Step 4 maps TestRun stage. Step 6 is exit code inspection (only when stage == "finished").

**StopRun pattern** (cloud.go lines 192-219):
```go
func (p *GrafanaCloudProvider) StopRun(ctx context.Context, cfg *provider.PluginConfig, runID string) error {
	// 1. Validate
	// 2. Initialize client
	// 3. Execute delete/abort call
	// 4. Handle error with fmt.Errorf wrapping
	if err != nil {
		return fmt.Errorf("stop run %s: %w", runID, err)
	}
	// 5. Log success
	slog.Info("stopped test run", "runId", runID, "provider", p.Name())
	return nil
}
```

Phase 8 StopRun deletes the TestRun CR via dynamic client. Same error wrapping and logging pattern.

**ensureClient lazy-init pattern** (operator.go lines 68-87):
```go
func (p *K6OperatorProvider) ensureClient() (kubernetes.Interface, error) {
	p.clientOnce.Do(func() {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			p.clientErr = fmt.Errorf("in-cluster config: %w", err)
			slog.Error("kubernetes client initialization failed permanently",
				"error", p.clientErr,
				"provider", p.Name(),
			)
			return
		}
		p.client, p.clientErr = kubernetes.NewForConfig(cfg)
	})
	return p.client, p.clientErr
}
```

Phase 8 extends this to also initialize `p.dynClient` via `dynamic.NewForConfig(cfg)` in the same `clientOnce.Do` block. Returns `(kubernetes.Interface, dynamic.Interface, error)`.

**Error handling pattern** (consistent across all provider methods):
```go
return "", fmt.Errorf("create TestRun %s/%s: %w", ns, name, err)
```

Always wrap errors with `fmt.Errorf` using `%w` for error chaining. Include context (namespace, name, operation) in the message.

**Logging pattern** (consistent across all files):
```go
slog.Info("action description",
	"key1", value1,
	"key2", value2,
	"provider", p.Name(),
)
```

Always include `"provider", p.Name()` in log entries. Use `slog.Debug` for routine operations, `slog.Info` for lifecycle events, `slog.Warn` for non-fatal errors, `slog.Error` for permanent failures.

---

### `internal/provider/operator/testrun.go` (utility, transform -- CREATE)

**Analog:** `internal/provider/cloud/types.go`

This file contains TestRun CR construction helpers. The cloud provider's `types.go` contains status mapping helpers (mapToRunState, isThresholdPassed) -- same role of extracting domain-specific logic into a focused file.

**File structure pattern** (cloud/types.go lines 1-47):
```go
package cloud

import (
	"log/slog"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Constants for domain values
const (
	resultPassed = "passed"
	resultFailed = "failed"
	resultError  = "error"
)

// Pure mapping function: external status -> internal RunState
func mapToRunState(statusType string, result *string) provider.RunState {
	switch statusType {
	case "created", "queued", "initializing", "running", "processing_metrics":
		return provider.Running
	// ...
	}
}
```

Phase 8 testrun.go follows this pattern: package-level constants (GVR definitions, label keys), pure construction functions (buildTestRun, buildPrivateLoadZone), and CR name generation. No side effects -- pure struct construction.

**Constants pattern:**
```go
var testRunGVR = schema.GroupVersionResource{
	Group:    "k6.io",
	Version:  "v1alpha1",
	Resource: "testruns",
}

const (
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelRollout   = "k6-plugin/rollout"
	managedByValue = "argo-rollouts-k6-plugin"
)
```

**Builder function pattern** (from research, aligned with types.go convention of pure functions):
```go
func buildTestRun(cfg *provider.PluginConfig, scriptCMName, scriptKey, namespace, crName string) *k6v1alpha1.TestRun {
	// Pure struct construction, no I/O, no side effects
	tr := &k6v1alpha1.TestRun{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k6.io/v1alpha1",
			Kind:       "TestRun",
		},
		ObjectMeta: metav1.ObjectMeta{...},
		Spec:       k6v1alpha1.TestRunSpec{...},
	}
	return tr
}
```

---

### `internal/provider/operator/exitcode.go` (utility, transform -- CREATE)

**Analog:** `internal/provider/cloud/types.go`

This file handles runner pod exit code inspection and mapping to RunState. Directly parallel to `types.go` which maps cloud API status to RunState.

**Status mapping pattern** (cloud/types.go lines 19-43):
```go
func mapToRunState(statusType string, result *string) provider.RunState {
	switch statusType {
	case "created", "queued", "initializing", "running", "processing_metrics":
		return provider.Running
	case "completed":
		if result == nil {
			return provider.Errored
		}
		switch *result {
		case resultPassed:
			return provider.Passed
		case resultFailed:
			return provider.Failed
		case resultError:
			return provider.Errored
		default:
			return provider.Errored
		}
	case "aborted":
		return provider.Aborted
	default:
		slog.Warn("unknown k6 status type, treating as error", "statusType", statusType)
		return provider.Errored
	}
}
```

Phase 8 equivalent maps k6 exit codes (0, 99, other) to RunState, and maps TestRun stage strings to RunState. Same switch-based pure function pattern.

**Exit code mapping (from research):**
```go
const (
	exitCodeSuccess          = 0
	exitCodeThresholdsFailed = 99
)

func exitCodeToRunState(code int32) provider.RunState {
	switch code {
	case 0:
		return provider.Passed
	case 99:
		return provider.Failed
	default:
		return provider.Errored
	}
}
```

**Stage mapping (from research):**
```go
func stageToRunState(stage string) provider.RunState {
	switch stage {
	case "initialization", "initialized", "created", "started":
		return provider.Running
	case "stopped":
		return provider.Running
	case "finished":
		return provider.Running // need exit code check
	case "error":
		return provider.Errored
	default:
		return provider.Running
	}
}
```

**Pod listing function** (uses typed client, same client pattern as readScript in operator.go lines 103-135):
```go
func checkRunnerExitCodes(ctx context.Context, client kubernetes.Interface, ns, testRunName string) (provider.RunState, error) {
	selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return provider.Errored, fmt.Errorf("list runner pods: %w", err)
	}
	// ... check exit codes
}
```

---

### `internal/provider/config.go` (model, transform -- MODIFY)

**Analog:** `internal/provider/config.go` (self -- extend existing file)

Phase 8 adds new fields to `PluginConfig` for k6-operator configuration: parallelism, resources, runnerImage, env, arguments.

**Existing field pattern** (config.go lines 13-28):
```go
type PluginConfig struct {
	TestRunID   string `json:"testRunId,omitempty"`
	TestID      string `json:"testId"`
	APIToken    string `json:"apiToken"`
	StackID     string `json:"stackId"`
	Timeout     string `json:"timeout,omitempty"`
	Metric      string `json:"metric"`
	Aggregation string `json:"aggregation,omitempty"`

	// Provider routing (per D-04)
	Provider string `json:"provider,omitempty"`

	// k6-operator fields (per D-04, D-09)
	ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"`
	Namespace    string        `json:"namespace,omitempty"`
}
```

New fields follow the same conventions: `json:"fieldName,omitempty"` tags, comment referencing decision number (D-06), grouped under a `// Phase 8 k6-operator fields` comment.

**New fields to add:**
```go
	// Phase 8 k6-operator fields (per D-06)
	Parallelism int              `json:"parallelism,omitempty"`
	Resources   *corev1.ResourceRequirements `json:"resources,omitempty"`
	RunnerImage string           `json:"runnerImage,omitempty"`
	Env         []corev1.EnvVar  `json:"env,omitempty"`
	Arguments   []string         `json:"arguments,omitempty"`
```

**Validation extension pattern** (config.go lines 61-72):
```go
func (c *PluginConfig) ValidateK6Operator() error {
	if c.ConfigMapRef == nil {
		return fmt.Errorf("configMapRef is required for k6-operator provider")
	}
	if c.ConfigMapRef.Name == "" {
		return fmt.Errorf("configMapRef.name is required")
	}
	if c.ConfigMapRef.Key == "" {
		return fmt.Errorf("configMapRef.key is required")
	}
	return nil
}
```

Phase 8 extends this with bounds checking for parallelism (e.g., > 0 if set). Same `fmt.Errorf` pattern for validation errors.

---

### `internal/provider/operator/operator_test.go` (test, CRUD -- MODIFY)

**Analog:** `internal/provider/cloud/cloud_test.go`

Extends the existing test file with tests for the real TriggerRun, GetRunResult, StopRun implementations.

**Test helper pattern** (operator_test.go lines 16-35):
```go
func defaultOperatorConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		Provider: "k6-operator",
		ConfigMapRef: &provider.ConfigMapRef{
			Name: "k6-scripts",
			Key:  "load-test.js",
		},
		Namespace: "test-ns",
	}
}

func testConfigMap(ns, name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: data,
	}
}
```

Phase 8 extends `defaultOperatorConfig()` to include Phase 8 fields (parallelism, etc.) and adds helper functions for creating fake dynamic client objects.

**Fake client injection pattern** (operator_test.go lines 46-53):
```go
func TestEnsureClient_WithInjectedClient(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient))

	client, err := p.ensureClient()
	require.NoError(t, err)
	assert.Equal(t, fakeClient, client)
}
```

Phase 8 adds `WithDynClient(fakeDyn)` following the same pattern. Tests inject both a typed fake and a dynamic fake.

**Test structure pattern** (cloud_test.go lines 29-71 -- TriggerRun success):
```go
func TestTriggerRun_Success(t *testing.T) {
	// 1. Set up mock/fake (httptest server for cloud, fake clientset for operator)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud/v6/load_tests/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		// Return response
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{...})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	// 2. Create provider with injected fake
	p := NewGrafanaCloudProvider(WithBaseURL(server.URL))
	cfg := defaultConfig()

	// 3. Execute
	runID, err := p.TriggerRun(context.Background(), cfg)

	// 4. Assert
	require.NoError(t, err)
	assert.Equal(t, "99", runID)
}
```

Phase 8 operator tests follow this same 4-step structure. Instead of httptest, they use `fake.NewSimpleClientset()` and `dynamicfake.NewSimpleDynamicClient()`.

**Dynamic fake client test pattern** (from research, adapted to project conventions):
```go
func TestTriggerRun_CreatesTestRun(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := fake.NewSimpleClientset(cm)
	scheme := runtime.NewScheme()
	fakeDyn := dynamicfake.NewSimpleDynamicClient(scheme)

	p := NewK6OperatorProvider(
		WithClient(fakeClient),
		WithDynClient(fakeDyn),
	)

	cfg := defaultOperatorConfig()
	runID, err := p.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, runID)

	// Verify the TestRun CR was created
	created, err := fakeDyn.Resource(testRunGVR).Namespace("test-ns").Get(
		context.Background(), runID, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "TestRun", created.GetKind())
}
```

---

### `internal/provider/operator/testrun_test.go` (test, transform -- CREATE)

**Analog:** `internal/provider/cloud/metrics_test.go`

Tests for pure TestRun construction functions. The cloud provider's `metrics_test.go` tests pure parsing/mapping functions (parseAggregateValue) -- same role.

**Pure function test pattern** (cloud/metrics_test.go lines 18-33):
```go
func TestParseAggregateValue_ValidFloat(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"__name__": "http_req_duration"},
					"values": [[1684950639, 14207.5]]
				}
			]
		}
	}`)
	val, err := parseAggregateValue(body)
	require.NoError(t, err)
	assert.InDelta(t, 14207.5, val, 0.001)
}
```

Phase 8 tests follow the same pattern: construct input, call pure function, assert output. No mocks needed for pure construction functions.

**Table-driven test pattern** (cloud/metrics_test.go lines 337-368):
```go
func TestMapToRunState_KnownStatuses(t *testing.T) {
	passed := "passed"
	failed := "failed"
	errored := "error"

	tests := []struct {
		statusType string
		result     *string
		want       provider.RunState
	}{
		{"created", nil, provider.Running},
		{"completed", &passed, provider.Passed},
		{"completed", &failed, provider.Failed},
		// ...
	}
	for _, tc := range tests {
		t.Run(tc.statusType, func(t *testing.T) {
			assert.Equal(t, tc.want, mapToRunState(tc.statusType, tc.result))
		})
	}
}
```

Phase 8 uses this table-driven pattern for testing buildTestRun with various config permutations and testRunName generation.

---

### `internal/provider/operator/exitcode_test.go` (test, transform -- CREATE)

**Analog:** `internal/provider/cloud/metrics_test.go`

Tests for exit code mapping and runner pod inspection functions.

**Same patterns as testrun_test.go above.** Table-driven tests for exitCodeToRunState and stageToRunState. Fake clientset tests for checkRunnerExitCodes.

**Pod fixture construction** (follows testConfigMap helper pattern from operator_test.go):
```go
func testRunnerPod(ns, testRunName string, exitCode int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("k6-%s-runner-0", testRunName),
			Namespace: ns,
			Labels: map[string]string{
				"app":    "k6",
				"k6_cr":  testRunName,
				"runner": "true",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "k6",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: exitCode,
						},
					},
				},
			},
		},
	}
}
```

---

## Shared Patterns

### Compile-Time Interface Check
**Source:** `internal/provider/operator/operator.go` line 17
**Apply to:** operator.go (already present, maintain it)
```go
var _ provider.Provider = (*K6OperatorProvider)(nil)
```

### Error Wrapping
**Source:** All provider files
**Apply to:** All new/modified files in `internal/provider/operator/`
```go
return "", fmt.Errorf("create TestRun %s/%s: %w", ns, name, err)
```
Always use `%w` for wrapped errors. Include operation, namespace, and name in the message.

### Structured Logging
**Source:** `internal/provider/operator/operator.go` lines 127-133, `internal/provider/cloud/cloud.go` lines 86-90
**Apply to:** All new/modified operator files
```go
slog.Info("k6 test run created",
	"testRunName", crName,
	"namespace", ns,
	"provider", p.Name(),
)
```
Always include `"provider", p.Name()`. Use `slog` from stdlib (log/slog). Never use logrus, fmt.Println, or os.Stdout.

### Functional Options
**Source:** `internal/provider/operator/operator.go` lines 28-38
**Apply to:** operator.go (extend with WithDynClient)
```go
type Option func(*K6OperatorProvider)

func WithClient(c kubernetes.Interface) Option {
	return func(p *K6OperatorProvider) {
		p.client = c
		p.clientOnce.Do(func() {})
	}
}
```

### Test Helper Functions
**Source:** `internal/provider/operator/operator_test.go` lines 16-35
**Apply to:** All test files in `internal/provider/operator/`
```go
func defaultOperatorConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		Provider: "k6-operator",
		ConfigMapRef: &provider.ConfigMapRef{
			Name: "k6-scripts",
			Key:  "load-test.js",
		},
		Namespace: "test-ns",
	}
}
```

### Test Assertions
**Source:** All test files
**Apply to:** All new test files
```go
import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// require for must-succeed operations (setup, preconditions)
require.NoError(t, err)
// assert for the actual assertions under test
assert.Equal(t, expected, actual)
assert.Contains(t, err.Error(), "substring")
```

### Config Validation Pattern
**Source:** `internal/provider/config.go` lines 61-72
**Apply to:** config.go (extend ValidateK6Operator)
```go
func (c *PluginConfig) ValidateK6Operator() error {
	if c.ConfigMapRef == nil {
		return fmt.Errorf("configMapRef is required for k6-operator provider")
	}
	// ... field-level checks returning fmt.Errorf
	return nil
}
```

### Context Timeout on Provider Calls
**Source:** `internal/metric/metric.go` lines 79-80, `internal/step/step.go` line 108
**Apply to:** Callers already handle this; provider methods should respect the context passed in
```go
ctx, cancel := context.WithTimeout(context.Background(), providerCallTimeout)
defer cancel()
```
Provider methods receive this context and must pass it to all k8s API calls.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | -- | -- | All Phase 8 files have close analogs in the existing codebase |

## Metadata

**Analog search scope:** `internal/provider/`, `internal/metric/`, `internal/step/`
**Files scanned:** 15 Go source files across provider, metric, and step packages
**Pattern extraction date:** 2026-04-15
