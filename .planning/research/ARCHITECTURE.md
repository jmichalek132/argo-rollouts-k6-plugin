# Architecture Patterns: v0.3.0 In-Cluster Execution

**Domain:** Argo Rollouts k6 Load Testing Plugin -- In-Cluster Providers
**Researched:** 2026-04-15
**Overall confidence:** HIGH (existing codebase fully analyzed, k6-operator API types verified, plugin RBAC model confirmed)

## Executive Summary

v0.3.0 introduces three new execution backends (Kubernetes Job, k6-operator TestRun, local binary) and a cross-cutting script sourcing mechanism (ConfigMap). These integrate with the existing `provider.Provider` interface, requiring **interface evolution** (new `ScriptSource` parameter), **a new dependency** (`k8s.io/client-go` for Kubernetes API access), and **four new packages** under `internal/`. The plugin binary runs as a child process of the Argo Rollouts controller and inherits its service account token, so `rest.InClusterConfig()` works without any special setup -- but the controller's RBAC must be extended for each new resource type (ConfigMaps, Jobs, TestRuns).

The build order is dictated by dependency chains: ConfigMap sourcing is a prerequisite for both Job and k6-operator providers (they need scripts from somewhere), the Job provider is simpler and validates the client-go integration patterns, and the k6-operator provider builds on those patterns plus adds CRD watching. Local binary execution is independent but deferred because it requires the k6 binary to exist in the controller pod's filesystem, which is an unusual deployment constraint.

## Existing Architecture (Baseline)

```
cmd/metric-plugin/main.go     -- creates GrafanaCloudProvider, wires to metric.New()
cmd/step-plugin/main.go        -- creates GrafanaCloudProvider, wires to step.New()

internal/
  provider/
    provider.go                -- Provider interface (4 methods + Name())
    config.go                  -- PluginConfig struct (cloud-specific fields)
    cloud/                     -- GrafanaCloudProvider implementation
    providertest/              -- MockProvider for unit tests
  metric/
    metric.go                  -- K6MetricProvider (RpcMetricProviderPlugin)
  step/
    step.go                    -- K6StepPlugin (RpcStepPlugin)
```

**Key architectural properties to preserve:**
1. **Stateless provider pattern** -- credentials/config passed per call, no shared state
2. **Provider selected in main.go** -- metric/step packages accept any `provider.Provider`
3. **Two binaries, one module** -- separate MagicCookieValue handshakes
4. **MockProvider for unit tests** -- function-field pattern in providertest/

## Recommended Architecture for v0.3.0

### Target Directory Structure

```
internal/
  provider/
    provider.go              -- Provider interface (MODIFIED: TriggerRun takes ScriptSource)
    config.go                -- PluginConfig (MODIFIED: new fields for provider type + script ref)
    script.go                -- NEW: ScriptSource type + ConfigMap resolver
    cloud/                   -- UNCHANGED (GrafanaCloudProvider)
    job/                     -- NEW: KubernetesJobProvider
      job.go                 --   Creates batch/v1 Jobs with k6 container
      job_test.go
    operator/                -- NEW: K6OperatorProvider
      operator.go            --   Creates k6.io/v1alpha1 TestRun CRs
      operator_test.go
    binary/                  -- NEW: LocalBinaryProvider (research/experimental)
      binary.go              --   Runs k6 as os/exec subprocess
      binary_test.go
    providertest/            -- MODIFIED: MockProvider gains ScriptSource support
  kube/                      -- NEW: Shared Kubernetes client factory
    client.go                --   InClusterConfig, shared across Job + Operator providers
  metric/                    -- MINOR CHANGES (config parsing)
  step/                      -- MINOR CHANGES (config parsing)
```

### Component Boundaries

| Component | Responsibility | Communicates With |
|-----------|---------------|-------------------|
| `provider/` (interface) | Contract for all k6 backends | metric/, step/ (via interface) |
| `provider/config.go` | Parse JSON config, determine provider + script source | All providers, metric/, step/ |
| `provider/script.go` | Resolve ScriptSource (ConfigMap -> raw JS) | kube/ (for ConfigMap reads) |
| `provider/cloud/` | Grafana Cloud k6 API calls | External: api.k6.io |
| `provider/job/` | Create/watch/delete batch/v1 Jobs | kube/ (for K8s API) |
| `provider/operator/` | Create/watch k6.io/v1alpha1 TestRun CRs | kube/ (for K8s API) |
| `provider/binary/` | Run k6 subprocess, parse exit code + summary | Local filesystem (k6 binary) |
| `kube/` | Kubernetes client singleton, shared RBAC | K8s API server (via service account) |
| `metric/` | Argo Rollouts MetricProviderPlugin RPC | provider/ (via interface) |
| `step/` | Argo Rollouts StepPlugin RPC | provider/ (via interface) |

## Integration Point 1: Provider Interface Evolution

### Current Interface

```go
type Provider interface {
    TriggerRun(ctx context.Context, cfg *PluginConfig) (runID string, err error)
    GetRunResult(ctx context.Context, cfg *PluginConfig, runID string) (*RunResult, error)
    StopRun(ctx context.Context, cfg *PluginConfig, runID string) error
    Name() string
}
```

### Proposed Interface (v0.3.0)

The interface itself does NOT need to change. The key insight: `PluginConfig` already carries all per-call state, and expanding PluginConfig is cleaner than changing the interface signature.

```go
// provider.go -- interface stays the same
type Provider interface {
    TriggerRun(ctx context.Context, cfg *PluginConfig) (runID string, err error)
    GetRunResult(ctx context.Context, cfg *PluginConfig, runID string) (*RunResult, error)
    StopRun(ctx context.Context, cfg *PluginConfig, runID string) error
    Name() string
}
```

**Rationale:** Adding ScriptSource to PluginConfig (not to TriggerRun's signature) preserves backward compatibility. Cloud provider ignores script source (it uses testId). Job and operator providers read it. The interface contract remains unchanged -- no breaking change to metric/ or step/ packages.

### PluginConfig Evolution

```go
// config.go -- MODIFIED
type PluginConfig struct {
    // --- Existing (Grafana Cloud) ---
    TestRunID   string `json:"testRunId,omitempty"`
    TestID      string `json:"testId,omitempty"`      // NOW optional (was required)
    APIToken    string `json:"apiToken,omitempty"`     // NOW optional (not needed for job/operator)
    StackID     string `json:"stackId,omitempty"`      // NOW optional
    Timeout     string `json:"timeout,omitempty"`
    Metric      string `json:"metric"`
    Aggregation string `json:"aggregation,omitempty"`

    // --- NEW: Provider selection ---
    ProviderType string `json:"provider,omitempty"`    // "cloud" (default), "job", "operator", "binary"

    // --- NEW: Script sourcing ---
    Script *ScriptSource `json:"script,omitempty"`     // ConfigMap ref for job/operator/binary

    // --- NEW: Job/Operator-specific ---
    Namespace    string `json:"namespace,omitempty"`   // K8s namespace for Job/TestRun (defaults to controller ns)
    Image        string `json:"image,omitempty"`       // k6 container image (default: "grafana/k6:latest")
    Parallelism  int32  `json:"parallelism,omitempty"` // k6-operator parallelism (default: 1)

    // --- NEW: Binary-specific ---
    BinaryPath   string `json:"binaryPath,omitempty"`  // Path to k6 binary (default: "k6")
}

// ScriptSource identifies where the k6 test script comes from.
type ScriptSource struct {
    ConfigMap    *ConfigMapRef `json:"configMap,omitempty"`
    Inline       string        `json:"inline,omitempty"`    // Future: inline script
}

type ConfigMapRef struct {
    Name string `json:"name"`
    Key  string `json:"key,omitempty"` // defaults to "test.js"
}
```

### Validation Changes

Config validation moves from hardcoded cloud-only checks to provider-aware validation:

| Provider | Required Fields | Optional Fields |
|----------|----------------|-----------------|
| `cloud` (default) | testId OR testRunId, apiToken, stackId, metric | timeout, aggregation |
| `job` | script.configMap, metric | namespace, image, timeout |
| `operator` | script.configMap, metric | namespace, image, parallelism, timeout |
| `binary` | script.configMap OR script.inline, metric | binaryPath, timeout |

## Integration Point 2: Provider Factory Pattern

### Current Wiring (main.go)

Both binaries currently hardcode `GrafanaCloudProvider`:

```go
p := cloud.NewGrafanaCloudProvider(opts...)
impl := metric.New(p)
```

### Proposed: Provider Factory

Add a factory that reads provider type from config at first call. Two approaches:

**Option A: Static selection at startup (simpler, recommended)**

The plugin reads the provider type from an environment variable or the first config it receives and creates one provider for its lifetime. This matches the current pattern where the cloud provider is created once in main.go.

```go
// internal/provider/factory.go
func NewProvider(providerType string, kubeClient kube.Client) (Provider, error) {
    switch providerType {
    case "cloud", "":
        return cloud.NewGrafanaCloudProvider(), nil
    case "job":
        return job.NewKubernetesJobProvider(kubeClient), nil
    case "operator":
        return operator.NewK6OperatorProvider(kubeClient), nil
    case "binary":
        return binary.NewLocalBinaryProvider(), nil
    default:
        return nil, fmt.Errorf("unknown provider type: %s", providerType)
    }
}
```

**Option B: Dynamic dispatch per call (more flexible, more complex)**

A multiplexing provider that reads `cfg.ProviderType` on each call and delegates to the right backend. This allows a single binary to serve multiple provider types.

**Recommendation: Option A.** The AnalysisTemplate/Rollout config is static per analysis run. Users do not mix providers within a single template. Static selection avoids runtime dispatch complexity. The provider type can be set via environment variable (`K6_PROVIDER`) in main.go, falling back to "cloud" for backward compatibility.

```go
// cmd/metric-plugin/main.go
func main() {
    setupLogging()

    providerType := os.Getenv("K6_PROVIDER") // "cloud", "job", "operator", "binary"
    var kubeClient kube.Client
    if providerType == "job" || providerType == "operator" {
        var err error
        kubeClient, err = kube.NewInClusterClient()
        if err != nil {
            slog.Error("failed to create kube client", "error", err)
            os.Exit(1)
        }
    }

    p, err := provider.NewProvider(providerType, kubeClient)
    if err != nil {
        slog.Error("failed to create provider", "error", err)
        os.Exit(1)
    }

    impl := metric.New(p)
    goPlugin.Serve(...)
}
```

## Integration Point 3: Kubernetes API Access

### How the Plugin Gets K8s Access

The plugin binary runs as a **child process** of the Argo Rollouts controller pod. It inherits:
- The controller's filesystem (including `/var/run/secrets/kubernetes.io/serviceaccount/token`)
- The controller's environment variables (`KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT`)

`rest.InClusterConfig()` reads these automatically. No special configuration needed.

**Confidence: HIGH** -- verified from Kubernetes docs and Argo Rollouts plugin docs that plugins "inherit the same RBAC permissions as the controller."

### Required RBAC Extensions

The Argo Rollouts controller's ClusterRole/Role must be extended:

```yaml
# For ConfigMap script sourcing (all in-cluster providers)
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get"]

# For Kubernetes Job provider
- apiGroups: ["batch"]
  resources: ["jobs"]
  verbs: ["create", "get", "watch", "delete"]

# For k6-operator provider
- apiGroups: ["k6.io"]
  resources: ["testruns"]
  verbs: ["create", "get", "watch", "delete"]
- apiGroups: ["k6.io"]
  resources: ["testruns/status"]
  verbs: ["get"]
```

### Shared Kubernetes Client

```go
// internal/kube/client.go
package kube

import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/dynamic"
)

type Client struct {
    Typed   kubernetes.Interface   // For ConfigMaps, Jobs
    Dynamic dynamic.Interface      // For k6.io CRDs (unstructured)
    Config  *rest.Config
}

func NewInClusterClient() (*Client, error) {
    cfg, err := rest.InClusterConfig()
    if err != nil {
        return nil, fmt.Errorf("in-cluster config: %w (plugin must run inside K8s pod)", err)
    }
    typed, err := kubernetes.NewForConfig(cfg)
    if err != nil {
        return nil, fmt.Errorf("typed client: %w", err)
    }
    dyn, err := dynamic.NewForConfig(cfg)
    if err != nil {
        return nil, fmt.Errorf("dynamic client: %w", err)
    }
    return &Client{Typed: typed, Dynamic: dyn, Config: cfg}, nil
}
```

**Why dynamic client for k6-operator:** Importing `github.com/grafana/k6-operator/api/v1alpha1` pulls in the entire k6-operator dependency tree (controller-runtime, etc.). Using `dynamic.Interface` with `unstructured.Unstructured` objects avoids this -- we only need to create a TestRun CR and watch its status.stage field. The CRD schema is simple enough that hand-building the unstructured object is cleaner than importing the full operator module.

**Alternative considered:** Import k6-operator types directly. Rejected because:
- k6-operator depends on controller-runtime, which pulls in ~50 transitive deps
- Version coupling: k6-operator releases independently, pinning creates maintenance burden
- The TestRun spec we create is 4 fields; unstructured is simpler

## Integration Point 4: ConfigMap Script Sourcing

### Data Flow

```
AnalysisTemplate JSON config
  -> PluginConfig.Script.ConfigMap{Name: "my-k6-script", Key: "test.js"}
  -> ScriptResolver.Resolve(ctx, namespace, scriptSource)
  -> kube.Client.Typed.CoreV1().ConfigMaps(ns).Get(ctx, name, ...)
  -> configMap.Data[key]  (string: the k6 JavaScript)
  -> passed to Job spec / TestRun CR / binary stdin
```

### ScriptResolver Component

```go
// internal/provider/script.go
package provider

import (
    "context"
    "fmt"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

const defaultScriptKey = "test.js"

type ScriptResolver struct {
    client kubernetes.Interface
}

func NewScriptResolver(client kubernetes.Interface) *ScriptResolver {
    return &ScriptResolver{client: client}
}

func (r *ScriptResolver) Resolve(ctx context.Context, namespace string, src *ScriptSource) (string, error) {
    if src == nil {
        return "", fmt.Errorf("script source is nil")
    }
    if src.ConfigMap != nil {
        return r.resolveConfigMap(ctx, namespace, src.ConfigMap)
    }
    if src.Inline != "" {
        return src.Inline, nil
    }
    return "", fmt.Errorf("no script source specified (configMap or inline required)")
}

func (r *ScriptResolver) resolveConfigMap(ctx context.Context, ns string, ref *ConfigMapRef) (string, error) {
    key := ref.Key
    if key == "" {
        key = defaultScriptKey
    }
    cm, err := r.client.CoreV1().ConfigMaps(ns).Get(ctx, ref.Name, metav1.GetOptions{})
    if err != nil {
        return "", fmt.Errorf("get configmap %s/%s: %w", ns, ref.Name, err)
    }
    script, ok := cm.Data[key]
    if !ok {
        return "", fmt.Errorf("key %q not found in configmap %s/%s", key, ns, ref.Name)
    }
    return script, nil
}
```

### How Each Provider Uses Scripts

| Provider | Script Usage | Notes |
|----------|-------------|-------|
| Cloud | Ignores `script` field; uses `testId` to reference pre-uploaded script | No change needed |
| Job | Script mounted as ConfigMap volume in Job Pod spec | ConfigMap name from config, mounted at `/scripts/test.js` |
| Operator | Script referenced in TestRun CR `.spec.script.configMap` | Native k6-operator support -- just pass the ConfigMap reference through |
| Binary | Script written to temp file, passed as arg to `k6 run /tmp/script.js` | Or piped via stdin: `k6 run -` |

### Key Insight: Operator Provider Gets ConfigMap Sourcing for Free

The k6-operator TestRun CRD already has a `.spec.script.configMap` field:

```go
type K6Script struct {
    ConfigMap K6Configmap `json:"configMap,omitempty"`
    // ...
}
type K6Configmap struct {
    Name string `json:"name"`
    File string `json:"file,omitempty"`
}
```

The operator provider does not need to read the ConfigMap contents -- it just passes the ConfigMap name through to the TestRun CR. The k6-operator handles mounting it into the runner pods. This means the ScriptResolver is only needed by the Job provider and Binary provider.

## Integration Point 5: Kubernetes Job Provider

### Data Flow

```
TriggerRun(ctx, cfg) ->
  1. Resolve script (ScriptResolver or ConfigMap volume mount)
  2. Build batch/v1 Job spec:
     - Container: cfg.Image (default "grafana/k6:latest")
     - Command: ["k6", "run", "/scripts/test.js"]
     - Volume: ConfigMap mounted at /scripts/
     - RestartPolicy: Never
     - TTL: automatic cleanup
  3. Create Job via client-go
  4. Return Job name as runID

GetRunResult(ctx, cfg, runID) ->
  1. Get Job by name
  2. Map Job conditions to RunState:
     - Active > 0              -> Running
     - Succeeded > 0           -> check exit code -> Passed (exit 0) or Failed (exit 99)
     - Failed > 0              -> Errored
  3. Parse k6 summary from Pod logs (optional, for metrics)
  4. Return RunResult

StopRun(ctx, cfg, runID) ->
  1. Delete Job with propagation policy (delete pods)
```

### Job Naming Convention

`k6-{rollout-name}-{short-hash}` where short-hash is derived from the analysis run context. This prevents collisions across concurrent rollouts.

### Exit Code Mapping

| k6 Exit Code | RunState | Meaning |
|--------------|----------|---------|
| 0 | Passed | Test completed, all thresholds passed |
| 99 | Failed | One or more thresholds breached |
| 97-98, 100-110 | Errored | Script error, timeout, abort, etc. |

### Metrics from Jobs

The Job provider has two options for metrics:
1. **handleSummary() to stdout** -- k6 prints JSON summary to stdout, captured from Pod logs
2. **Threshold-only mode** -- just check exit code, report thresholds as passed/failed

Recommendation: Start with threshold-only mode (exit code). Add log-based metric parsing later if needed. This keeps the MVP simple and avoids parsing complexity.

```go
// RunResult for Job provider (threshold-only mode)
&provider.RunResult{
    State:            provider.Passed, // or Failed based on exit code
    ThresholdsPassed: exitCode == 0,
    // HTTPReqFailed, HTTPReqDuration, HTTPReqs: all zero (not available without log parsing)
}
```

## Integration Point 6: k6-Operator Provider

### Data Flow

```
TriggerRun(ctx, cfg) ->
  1. Build TestRun CR (unstructured):
     apiVersion: k6.io/v1alpha1
     kind: TestRun
     spec:
       script:
         configMap:
           name: cfg.Script.ConfigMap.Name
           file: cfg.Script.ConfigMap.Key
       parallelism: cfg.Parallelism (default 1)
       runner:
         image: cfg.Image (optional override)
  2. Create via dynamic client
  3. Return TestRun name as runID

GetRunResult(ctx, cfg, runID) ->
  1. Get TestRun CR via dynamic client
  2. Read .status.stage:
     - "initialization", "created", "started" -> Running
     - "finished"                              -> check conditions -> Passed or Failed
     - "error"                                 -> Errored
     - "stopped"                               -> Aborted
  3. Return RunResult

StopRun(ctx, cfg, runID) ->
  1. Delete TestRun CR (operator handles cleanup of runner Jobs)
```

### TestRun Status Stage Mapping

| status.stage | RunState | Notes |
|-------------|----------|-------|
| `""` (empty) | Running | Just created, not yet reconciled |
| `"initialization"` | Running | Initializer job validating script |
| `"initialized"` | Running | Script validated, ready to create runners |
| `"created"` | Running | Runner jobs created |
| `"started"` | Running | Test execution in progress |
| `"stopped"` | Aborted | Test was stopped |
| `"finished"` | Passed or Failed | Must check conditions/runner exit codes |
| `"error"` | Errored | Operator-level error |

**Confidence: HIGH** -- stage values from k6-operator API v1alpha1 types and DeepWiki analysis.

### Detecting Pass vs Fail at "finished"

The k6-operator issue #577 confirms a known limitation: the TestRun status conditions do not currently differentiate between successful and failed test runs when thresholds are breached. The `TestRunRunning` condition transitions from True to False when finished, but there is no `TestRunFailed` condition.

**Workaround:** After stage becomes "finished", inspect the runner Job exit codes:
1. List Jobs with label selector matching the TestRun
2. Check Pod exit codes: 0 = passed, 99 = thresholds failed

This is more complex but reliable. Alternatively, wait for k6-operator to add pass/fail conditions (issue #577 is open and tracked).

**Recommendation:** Implement the Job exit code check. Do not depend on upstream fixing issue #577 on our timeline.

## Integration Point 7: Local Binary Provider (Research/Experimental)

### Concept

Run k6 as a subprocess of the plugin process using `os/exec`. The k6 binary must be present in the controller pod's filesystem.

### Data Flow

```
TriggerRun(ctx, cfg) ->
  1. Resolve script to temp file
  2. Start: exec.CommandContext(ctx, cfg.BinaryPath, "run", "--summary-export=/tmp/summary.json", tmpScript)
  3. Store PID/process handle
  4. Return PID as runID

GetRunResult(ctx, cfg, runID) ->
  1. Check if process is still running
  2. If exited: parse exit code and summary JSON
  3. Return RunResult

StopRun(ctx, cfg, runID) ->
  1. Send SIGTERM to process
  2. Wait with timeout, SIGKILL if needed
```

### Major Constraints

1. **k6 binary must be in controller pod** -- requires custom controller image or init container
2. **Process lifecycle tied to plugin process** -- if plugin crashes, orphan k6 process
3. **Resource contention** -- k6 load generation competes with controller for CPU/memory
4. **No parallelism** -- single process, no distributed execution

### Recommendation

Mark as **experimental/research** in v0.3.0. Document the constraints. Do not make it a first-class provider unless there is clear user demand for "no external dependencies" execution mode.

## Data Flow: Complete Request Path

### Step Plugin -- Job Provider Example

```
Argo Rollouts Controller
  |
  | (net/rpc via go-plugin)
  v
Step Plugin (cmd/step-plugin/main.go)
  |
  | Run() called with RpcStepContext
  v
step.K6StepPlugin.Run()
  |
  | Parse config: provider="job", script.configMap.name="my-k6-test"
  v
provider.KubernetesJobProvider.TriggerRun(ctx, cfg)
  |
  | 1. Build Job spec with ConfigMap volume mount
  | 2. client-go: batchV1.Jobs(ns).Create(ctx, job, ...)
  v
Kubernetes API Server
  |
  | Job controller creates Pod
  v
k6 Pod: runs test, exits with code 0 or 99
  |
  | (subsequent Run() calls poll)
  v
provider.KubernetesJobProvider.GetRunResult(ctx, cfg, jobName)
  |
  | client-go: batchV1.Jobs(ns).Get(ctx, jobName, ...)
  | Map Job.Status to RunState
  v
step.K6StepPlugin: maps RunState to StepPhase
  |
  | (net/rpc response)
  v
Argo Rollouts Controller: proceeds or rolls back
```

## Patterns to Follow

### Pattern 1: Namespace Resolution

The plugin does not receive namespace information through the Argo Rollouts RPC interface directly. The namespace must come from the plugin config JSON.

```go
func resolveNamespace(cfg *PluginConfig) string {
    if cfg.Namespace != "" {
        return cfg.Namespace
    }
    // Fall back to controller's namespace (from downward API or service account)
    if ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
        return strings.TrimSpace(string(ns))
    }
    return "default"
}
```

### Pattern 2: Owner References for Cleanup

Jobs and TestRuns created by the plugin should have owner references or labels that allow cleanup if the analysis is abandoned. Since the plugin cannot set owner references to the AnalysisRun (it is not a K8s resource owner), use labels instead:

```go
labels := map[string]string{
    "app.kubernetes.io/managed-by": "argo-rollouts-k6-plugin",
    "k6.argo-rollouts/analysis":    analysisRunName,  // if available from context
}
```

### Pattern 3: Job Cleanup with TTL

```go
ttl := int32(3600) // 1 hour
job.Spec.TTLSecondsAfterFinished = &ttl
```

This ensures completed Jobs are garbage-collected even if the plugin crashes before calling StopRun.

### Pattern 4: Unstructured TestRun Creation

```go
func buildTestRunCR(name, namespace string, cfg *PluginConfig) *unstructured.Unstructured {
    tr := &unstructured.Unstructured{}
    tr.SetAPIVersion("k6.io/v1alpha1")
    tr.SetKind("TestRun")
    tr.SetName(name)
    tr.SetNamespace(namespace)
    tr.SetLabels(map[string]string{
        "app.kubernetes.io/managed-by": "argo-rollouts-k6-plugin",
    })

    spec := map[string]interface{}{
        "parallelism": int64(cfg.Parallelism),
        "script": map[string]interface{}{
            "configMap": map[string]interface{}{
                "name": cfg.Script.ConfigMap.Name,
            },
        },
    }
    if cfg.Script.ConfigMap.Key != "" {
        spec["script"].(map[string]interface{})["configMap"].(map[string]interface{})["file"] = cfg.Script.ConfigMap.Key
    }
    if cfg.Image != "" {
        spec["runner"] = map[string]interface{}{
            "image": cfg.Image,
        }
    }

    tr.Object["spec"] = spec
    return tr
}
```

## Anti-Patterns to Avoid

### Anti-Pattern 1: Importing k6-operator Module

**What:** `import "github.com/grafana/k6-operator/api/v1alpha1"` for typed TestRun creation.
**Why bad:** Pulls in controller-runtime (~50 transitive deps), creates version coupling, bloats binary from ~15MB to ~40MB+.
**Instead:** Use `k8s.io/client-go/dynamic` with `unstructured.Unstructured`. The TestRun spec is simple (5 fields). Build it as a map.

### Anti-Pattern 2: Shared Mutable State Between Provider Calls

**What:** Storing k8s client or Job references in provider struct fields and reusing across calls.
**Why bad:** The stateless pattern exists for good reason -- concurrent AnalysisRuns would conflict. The k8s client itself is safe to share (it is thread-safe), but per-run state must not leak between calls.
**Instead:** Keep the kube.Client as an immutable field. All per-run state flows through PluginConfig + runID.

### Anti-Pattern 3: Polling Job Status via Pod Log Streaming

**What:** Streaming Pod logs to detect k6 progress.
**Why bad:** Expensive, fragile (log buffer limits), and unnecessary. Job status + exit code is sufficient for pass/fail.
**Instead:** Use Job.Status.Conditions for state, Pod exit code for result. Reserve log parsing for optional metric extraction later.

### Anti-Pattern 4: Creating Provider Per Request

**What:** Instantiating a new KubernetesJobProvider on each Run()/Resume() call.
**Why bad:** Each creation would call InClusterConfig() and create a new HTTP/2 connection to the API server.
**Instead:** Create one kube.Client in main.go, inject into provider once.

## New Dependencies

| Dependency | Purpose | Impact |
|-----------|---------|--------|
| `k8s.io/client-go` | Create Jobs, get ConfigMaps, manage TestRun CRs | Already an **indirect** dependency (via argo-rollouts). Promoting to direct. |
| `k8s.io/api` | batch/v1 Job types, core/v1 ConfigMap types | Already an **indirect** dependency. Promoting to direct. |

No new external dependencies needed. `client-go` and `k8s.io/api` are already in go.sum via `argo-rollouts` and `e2e-framework`. Promoting them to direct dependencies adds zero new modules.

## Build Order (Suggested Phase Structure)

### Phase 1: ConfigMap Script Sourcing + PluginConfig Evolution

**Components:** `provider/config.go` (modified), `provider/script.go` (new), `kube/client.go` (new)
**Why first:**
- Foundation for all in-cluster providers
- Validates client-go integration in isolation
- PluginConfig changes affect all downstream phases
- Can be tested without any new provider -- just verify ConfigMap reads work
- Backward compatible: cloud provider ignores new fields

**RBAC needed:** `configmaps: get`

### Phase 2: Kubernetes Job Provider

**Components:** `provider/job/job.go` (new), `cmd/*/main.go` (modified for factory)
**Why second:**
- Simplest in-cluster provider (no CRD, just batch/v1 Jobs)
- Validates the full data flow: config -> script resolution -> k6 execution -> result mapping
- Job API is stable (batch/v1 GA since Kubernetes 1.2)
- Patterns established here (Job creation, exit code parsing, cleanup) reuse in operator provider

**RBAC needed:** `jobs: create, get, watch, delete`

### Phase 3: k6-Operator Provider

**Components:** `provider/operator/operator.go` (new)
**Why third:**
- Depends on patterns from Phase 2 (K8s resource creation, status polling)
- Requires k6-operator to be installed in the cluster (external dependency)
- More complex status mapping (TestRun stages vs Job conditions)
- Dynamic client usage needs careful testing

**RBAC needed:** `testruns: create, get, watch, delete; testruns/status: get`

### Phase 4: Local Binary Provider (Research)

**Components:** `provider/binary/binary.go` (new)
**Why last:**
- Independent of Phases 1-3 (no K8s dependency)
- Most constrained (requires k6 in controller image)
- Lowest priority per PROJECT.md ("research" scope)
- Can be deferred to v0.4.0 if v0.3.0 timeline is tight

**RBAC needed:** None (no K8s API calls)

## Scalability Considerations

| Concern | At 1 rollout | At 10 concurrent | At 100 concurrent |
|---------|-------------|-------------------|-------------------|
| K8s API calls (Job provider) | 3 calls per poll (Job + Pod) | 30 calls/poll cycle | Rate limiting risk; add client-side caching |
| K8s API calls (Operator provider) | 2 calls per poll (TestRun) | 20 calls/poll cycle | Lower than Job; operator manages sub-resources |
| ConfigMap reads | 1 per TriggerRun | 10 per trigger batch | Cache ConfigMap content per analysis run |
| Job cleanup | TTL handles it | TTL handles it | Ensure TTL is set; audit orphaned Jobs |
| Binary provider memory | 1 k6 process (~100MB) | 10 processes (~1GB) | Not viable; use Job/Operator |

## Sources

- [Argo Rollouts Plugin Docs](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- HIGH confidence (RBAC inheritance, plugin lifecycle)
- [k6-operator API v1alpha1 types](https://pkg.go.dev/github.com/grafana/k6-operator/api/v1alpha1) -- HIGH confidence (TestRunSpec, TestRunStatus, Stage enum)
- [k6-operator TestRun CRD configuration](https://grafana.com/docs/k6/latest/set-up/set-up-distributed-k6/usage/configure-testrun-crd/) -- HIGH confidence (spec.script.configMap)
- [k6-operator test execution flow](https://deepwiki.com/grafana/k6-operator/2.3-test-execution-flow) -- MEDIUM confidence (stage transitions)
- [k6-operator issue #577: TestRun pass/fail in conditions](https://github.com/grafana/k6-operator/issues/577) -- HIGH confidence (known limitation)
- [k6-operator conditions.go](https://github.com/grafana/k6-operator/blob/main/pkg/types/conditions.go) -- HIGH confidence (condition types)
- [k6 exit codes](https://github.com/grafana/k6/blob/master/errext/exitcodes/codes.go) -- HIGH confidence (99 = ThresholdsHaveFailed)
- [k6 handleSummary()](https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/) -- HIGH confidence (JSON summary format)
- [Kubernetes client-go InClusterConfig](https://github.com/kubernetes/client-go/blob/master/examples/in-cluster-client-configuration/README.md) -- HIGH confidence
- [Kubernetes Pod API access](https://kubernetes.io/docs/tasks/run-application/access-api-from-pod/) -- HIGH confidence (service account token mount)
- Existing codebase analysis (provider.go, config.go, cloud.go, step.go, metric.go, main.go) -- HIGH confidence
