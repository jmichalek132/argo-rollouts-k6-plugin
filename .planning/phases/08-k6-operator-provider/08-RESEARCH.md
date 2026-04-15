# Phase 8: k6-operator Provider - Research

**Researched:** 2026-04-15
**Domain:** Kubernetes CRD management (k6-operator TestRun), pod exit code inspection, dynamic client usage
**Confidence:** HIGH

## Summary

Phase 8 replaces the Phase 7 stub methods (TriggerRun, GetRunResult, StopRun) in `K6OperatorProvider` with real implementations that create, poll, and delete k6-operator `TestRun` custom resources. The k6-operator v1.3.2 Go module provides typed API structs (`TestRun`, `TestRunSpec`, `TestRunStatus`, `PrivateLoadZone`) in its `api/v1alpha1` package. These typed structs can be converted to `unstructured.Unstructured` via `runtime.DefaultUnstructuredConverter` and submitted to the Kubernetes API via the dynamic client (`k8s.io/client-go/dynamic`), which is already available in the project's go.mod.

The core challenge is pass/fail detection: k6-operator's `TestRunStatus.Stage` reaches `"finished"` for both passing and failing tests (issue #577). The workaround is to inspect runner pod exit codes after the TestRun reaches a terminal stage. k6 uses exit code 0 for success and exit code 99 (`ThresholdsHaveFailed`) for threshold breaches. Runner pods are discoverable via labels `app=k6`, `k6_cr=<testrun-name>`, `runner=true`.

**Primary recommendation:** Import k6-operator v1.3.2 types directly, use the dynamic client for CRD CRUD operations, and the existing typed client (from Phase 7) for pod listing and exit code inspection.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Import k6-operator Go types directly from `github.com/grafana/k6-operator`. Typed TestRun and PrivateLoadZone structs with compile-time safety. Adds a direct dependency on the k6-operator module.
- **D-02:** Target both k6.io/v1alpha1 TestRun (pure in-cluster) and PrivateLoadZone (Grafana Cloud-connected in-cluster). Auto-detect which CRD to use: if Grafana Cloud fields (apiToken, stackId) are present alongside `provider: "k6-operator"`, use PrivateLoadZone; otherwise use TestRun. No explicit override field.
- **D-03:** CRD client uses the same lazy-init Kubernetes client from Phase 7 (sync.Once pattern). No additional client initialization needed.
- **D-04:** No internal polling loop. The plugin piggybacks on the Argo Rollouts controller's existing polling cadence. Each GetRunResult/Run call does a single GET to check TestRun status. No goroutines, no watches, no reconnection logic.
- **D-05:** Pass/fail detection via runner pod exit codes (k6-operator issue #577 workaround). After TestRun reaches 'finished' stage, list runner pods by label, check exit codes. Exit 0 = all k6 thresholds passed, non-zero = failed. Log parsing for detailed metrics is deferred to Phase 9.
- **D-06:** Essential fields only. Expose: parallelism (int), resource requests/limits (corev1.ResourceRequirements), runner image (string), environment variables ([]corev1.EnvVar), k6 arguments ([]string). Covers 90% of use cases without bloating PluginConfig.
- **D-07:** Namespace auto-injected from rollout. Plugin reads namespace from the AnalysisRun/Rollout ObjectMeta passed via RPC. User can override via the existing `namespace` config field from Phase 7. TestRun CR created in the resolved namespace.
- **D-08:** Primary cleanup: explicit Delete on TestRun/PLZ CR when StopRun is called (rollout abort/terminate). Always works, predictable.
- **D-09:** Safety net: set owner reference on the TestRun CR pointing to the AnalysisRun (if UID is available via RPC context). Kubernetes garbage collection auto-deletes orphaned TestRuns when the AnalysisRun is cleaned up. If AnalysisRun UID is not available, fall back to label-based identification for manual/script cleanup.
- **D-10:** Consistent naming: `k6-<rollout-name>-<hash>` for TestRun CRs. Labels: `app.kubernetes.io/managed-by: argo-rollouts-k6-plugin`, `k6-plugin/rollout: <rollout-name>`.

### Claude's Discretion
- TestRun spec construction helper functions (builder pattern or direct struct)
- Pod label selector construction for runner pod discovery
- Error message wording for CRD creation failures and pod exit code interpretation
- Internal state management for tracking created TestRun names between TriggerRun/GetRunResult/StopRun calls

### Deferred Ideas (OUT OF SCOPE)
None -- discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| K6OP-01 | Plugin creates a TestRun CR (k6.io/v1alpha1) from plugin config and polls stage until terminal state | k6-operator v1.3.2 API types provide TestRun struct; dynamic client creates the CR; Stage enum has terminal values "finished" and "error" |
| K6OP-02 | Plugin extracts pass/fail result from runner pod exit codes after TestRun reaches finished stage | Runner pods labeled `app=k6,k6_cr=<name>,runner=true`; k6 exit code 0 = pass, 99 = thresholds failed; pod.Status.ContainerStatuses[].State.Terminated.ExitCode |
| K6OP-03 | Plugin supports namespace targeting for TestRun CR creation (defaults to rollout namespace) | Existing cfg.Namespace field + D-07 rollout namespace injection; dynamic client is namespace-scoped via .Namespace(ns) |
| K6OP-04 | Plugin supports parallelism configuration for distributed k6 execution | TestRunSpec.Parallelism int32 field maps directly from PluginConfig |
| K6OP-05 | Plugin supports resource requests/limits on k6 runner pods | TestRunSpec.Runner.Resources (corev1.ResourceRequirements) maps directly from PluginConfig |
| K6OP-06 | Plugin applies consistent naming and labels to TestRun CRs | D-10: `k6-<rollout>-<hash>` naming; ObjectMeta.Labels with managed-by and rollout labels |
| K6OP-07 | Plugin supports custom k6 runner image and environment variable injection | TestRunSpec.Runner.Image (string) and TestRunSpec.Runner.Env ([]corev1.EnvVar) map from PluginConfig; TestRunSpec.Arguments (string) for k6 CLI args |
| K6OP-08 | Plugin deletes TestRun CR on rollout abort/terminate to stop running k6 pods | D-08: dynamic client Delete on the TestRun CR by name; k6-operator cascades deletion to runner pods |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| TestRun CR creation | API / Backend (plugin process) | -- | Plugin constructs and submits CRD to k8s API server |
| TestRun status polling | API / Backend (plugin process) | -- | Plugin GETs the TestRun status on each controller reconcile |
| Runner pod exit code inspection | API / Backend (plugin process) | -- | Plugin lists pods via k8s API and reads container statuses |
| Pass/fail determination | API / Backend (plugin process) | -- | Business logic mapping exit codes to RunState |
| TestRun cleanup | API / Backend (plugin process) | Kubernetes GC (owner references) | Explicit delete + safety net via owner refs |
| Config parsing / validation | API / Backend (plugin process) | -- | JSON unmarshal from AnalysisTemplate/Rollout config |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/grafana/k6-operator` | v1.3.2 | Typed TestRun/PrivateLoadZone CRD structs | [VERIFIED: go list -m] Official Go types for k6-operator CRDs. Released 2026-04-02. Provides compile-time safety per D-01 |
| `k8s.io/client-go/dynamic` | v0.34.1 | Dynamic Kubernetes client for CRD CRUD | [VERIFIED: go.mod] Already in go.mod. Creates/Gets/Deletes unstructured CRDs without needing a generated typed client |
| `k8s.io/client-go/kubernetes` | v0.34.1 | Typed client for pod listing | [VERIFIED: go.mod] Already in go.mod. Used by Phase 7 for ConfigMap reads; Phase 8 reuses for pod listing |
| `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured` | v0.34.1 | Unstructured CRD conversion | [VERIFIED: go.mod] Runtime converter bridges typed structs to unstructured format for dynamic client |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `k8s.io/apimachinery/pkg/runtime` | v0.34.1 | `DefaultUnstructuredConverter` | Converting k6-operator typed structs to/from unstructured maps |
| `k8s.io/apimachinery/pkg/runtime/schema` | v0.34.1 | `GroupVersionResource` definition | Defining GVR for dynamic client resource targeting |
| `crypto/sha256` | stdlib | Hash generation for CR naming | Generating deterministic short hash for `k6-<rollout>-<hash>` naming pattern |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Dynamic client + typed structs | Pure unstructured (map construction) | Loses compile-time safety; error-prone nested map construction. D-01 locks typed approach |
| Dynamic client | controller-runtime client | controller-runtime client is more ergonomic but requires scheme registration and adds coupling. Dynamic client is simpler for CRUD-only needs |
| k6-operator module import | Vendored type definitions | Avoids transitive deps but drifts from upstream. D-01 locks direct import |

**Dependency impact of k6-operator v1.3.2 import:**
- Bumps `go` directive from 1.24.9 to 1.25.0 (Go 1.26.2 toolchain handles this) [VERIFIED: go get dry run]
- Upgrades `sigs.k8s.io/controller-runtime` from v0.20.0 (indirect) to v0.22.4 (indirect) [VERIFIED: go get dry run]
- k8s.io module versions remain at v0.34.1 (no conflict) [VERIFIED: go get dry run]
- Pulls in transitive dependencies: k6 runtime, otel exporters, ginkgo/gomega [VERIFIED: go get dry run]
- Binary size impact is minimal -- only the `api/v1alpha1` package symbols are linked [ASSUMED]

**Installation:**
```bash
go get github.com/grafana/k6-operator@v1.3.2
```

## Architecture Patterns

### System Architecture Diagram

```
AnalysisTemplate / Rollout YAML
         |
         | (JSON config via RPC)
         v
+------------------+
| K6StepPlugin /   |  parseConfig() -> PluginConfig
| K6MetricProvider |
+--------+---------+
         |
         | TriggerRun / GetRunResult / StopRun
         v
+------------------+
|     Router       |  resolve() -> K6OperatorProvider
+--------+---------+
         |
         v
+------------------------+
|  K6OperatorProvider    |
|                        |
|  TriggerRun:           |
|    1. readScript(CM)   |-----> k8s API: GET ConfigMap
|    2. buildTestRun()   |
|    3. dynamic.Create() |-----> k8s API: CREATE TestRun CR
|    4. return CR name   |
|                        |
|  GetRunResult:         |
|    1. dynamic.Get()    |-----> k8s API: GET TestRun CR
|    2. check Stage      |
|    3. if finished:     |
|       listRunnerPods() |-----> k8s API: LIST Pods (label selector)
|       checkExitCodes() |
|    4. return RunResult |
|                        |
|  StopRun:              |
|    1. dynamic.Delete() |-----> k8s API: DELETE TestRun CR
+------------------------+
         |
         v
+------------------------+
|  k6-operator           |  (cluster controller -- not our code)
|  Watches TestRun CRs   |
|  Creates runner pods   |
|  Manages lifecycle     |
+------------------------+
```

### Recommended Project Structure
```
internal/provider/operator/
  operator.go          # K6OperatorProvider (Phase 7 + Phase 8 implementations)
  testrun.go           # TestRun CR construction helpers (buildTestRun, buildPLZ)
  exitcode.go          # Runner pod exit code inspection logic
  operator_test.go     # Phase 7 tests (existing) + Phase 8 tests
  testrun_test.go      # TestRun construction tests
  exitcode_test.go     # Exit code inspection tests
internal/provider/
  config.go            # PluginConfig (extend with Phase 8 fields)
```

### Pattern 1: Dynamic Client CRD Creation
**What:** Convert typed k6-operator struct to unstructured, submit via dynamic client
**When to use:** Every TriggerRun call
**Example:**
```go
// Source: k8s.io/client-go/dynamic + k8s.io/apimachinery/pkg/runtime
import (
    k6v1alpha1 "github.com/grafana/k6-operator/api/v1alpha1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/client-go/dynamic"
)

var testRunGVR = schema.GroupVersionResource{
    Group:    "k6.io",
    Version:  "v1alpha1",
    Resource: "testruns",
}

func createTestRun(ctx context.Context, dynClient dynamic.Interface, tr *k6v1alpha1.TestRun) (string, error) {
    // Set TypeMeta (required for unstructured conversion)
    tr.APIVersion = "k6.io/v1alpha1"
    tr.Kind = "TestRun"

    // Convert typed struct to unstructured
    obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tr)
    if err != nil {
        return "", fmt.Errorf("convert TestRun to unstructured: %w", err)
    }
    u := &unstructured.Unstructured{Object: obj}

    // Create via dynamic client
    created, err := dynClient.Resource(testRunGVR).Namespace(tr.Namespace).Create(ctx, u, metav1.CreateOptions{})
    if err != nil {
        return "", fmt.Errorf("create TestRun %s/%s: %w", tr.Namespace, tr.Name, err)
    }
    return created.GetName(), nil
}
```

### Pattern 2: TestRun Status Polling via Dynamic Client
**What:** GET the TestRun CR, extract stage from unstructured response
**When to use:** Every GetRunResult call
**Example:**
```go
// Source: k8s.io/client-go/dynamic
func getTestRunStage(ctx context.Context, dynClient dynamic.Interface, ns, name string) (string, error) {
    u, err := dynClient.Resource(testRunGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return "", fmt.Errorf("get TestRun %s/%s: %w", ns, name, err)
    }
    stage, _, err := unstructured.NestedString(u.Object, "status", "stage")
    if err != nil {
        return "", fmt.Errorf("extract stage from TestRun %s/%s: %w", ns, name, err)
    }
    return stage, nil
}
```

### Pattern 3: Runner Pod Exit Code Inspection
**What:** List runner pods by label, check container terminated exit codes
**When to use:** After TestRun stage reaches "finished"
**Example:**
```go
// Source: k8s.io/client-go/kubernetes, k6-operator runner pod labels
func checkRunnerExitCodes(ctx context.Context, client kubernetes.Interface, ns, testRunName string) (bool, error) {
    // Labels set by k6-operator on runner pods
    selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)

    pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
        LabelSelector: selector,
    })
    if err != nil {
        return false, fmt.Errorf("list runner pods: %w", err)
    }
    if len(pods.Items) == 0 {
        return false, fmt.Errorf("no runner pods found for TestRun %s", testRunName)
    }

    for _, pod := range pods.Items {
        for _, cs := range pod.Status.ContainerStatuses {
            if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
                return false, nil // at least one runner failed
            }
        }
    }
    return true, nil // all runners exited 0
}
```

### Pattern 4: Dynamic Client Initialization (Lazy, Alongside Typed Client)
**What:** Create a dynamic client from the same `rest.Config` used for the typed client
**When to use:** During `ensureClient()` initialization (extend Phase 7 pattern)
**Example:**
```go
// Source: k8s.io/client-go/dynamic, k8s.io/client-go/rest
type K6OperatorProvider struct {
    clientOnce sync.Once
    client     kubernetes.Interface  // typed client (Phase 7)
    dynClient  dynamic.Interface     // dynamic client (Phase 8)
    clientErr  error
}

func (p *K6OperatorProvider) ensureClient() (kubernetes.Interface, dynamic.Interface, error) {
    p.clientOnce.Do(func() {
        cfg, err := rest.InClusterConfig()
        if err != nil {
            p.clientErr = fmt.Errorf("in-cluster config: %w", err)
            return
        }
        p.client, p.clientErr = kubernetes.NewForConfig(cfg)
        if p.clientErr != nil {
            return
        }
        p.dynClient, p.clientErr = dynamic.NewForConfig(cfg)
    })
    return p.client, p.dynClient, p.clientErr
}
```

### Anti-Patterns to Avoid
- **Internal polling loop in GetRunResult:** The Argo Rollouts controller already requeues at intervals. Adding a goroutine or sleep loop inside GetRunResult would block the RPC call and could cause timeouts. Each call must be a single API roundtrip (D-04).
- **Using controller-runtime client directly:** While it's being pulled in as a transitive dep, using it directly couples the plugin to controller-runtime's lifecycle management. The dynamic client is simpler for CRUD-only operations.
- **Constructing unstructured maps manually:** Building nested `map[string]interface{}` by hand is error-prone and loses compile-time safety. Use typed k6-operator structs and `runtime.DefaultUnstructuredConverter`.
- **Watching TestRun resources:** The plugin process is a child of the controller. Starting watches would require reconnection logic, goroutines, and adds complexity with no benefit over polling (D-04).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| TestRun CRD struct | Manual `map[string]interface{}` construction | `k6v1alpha1.TestRun` from k6-operator | Compile-time safety, upstream maintained, deep copy support |
| CRD CRUD operations | Raw HTTP calls to k8s API | `k8s.io/client-go/dynamic` | Handles auth, TLS, content negotiation, retries |
| Label selectors | String formatting for label queries | `k8s.io/apimachinery/pkg/labels.SelectorFromSet` | Type-safe, handles escaping edge cases |
| Unstructured conversion | Manual JSON marshal/unmarshal cycle | `runtime.DefaultUnstructuredConverter` | Purpose-built, handles TypeMeta correctly |
| CR name hashing | Custom hash function | `crypto/sha256` + truncation | Standard, deterministic, collision-resistant |

**Key insight:** The k6-operator module exports exactly the types we need. The overhead of importing it is justified by compile-time safety and automatic tracking of upstream CRD schema changes.

## Common Pitfalls

### Pitfall 1: Missing TypeMeta on Unstructured Conversion
**What goes wrong:** `runtime.DefaultUnstructuredConverter.ToUnstructured()` produces an object without `apiVersion` and `kind` if the source struct's TypeMeta is empty. The dynamic client then creates a resource with no type information, causing a 400 Bad Request.
**Why it happens:** Go zero-values for embedded `metav1.TypeMeta` are empty strings.
**How to avoid:** Always set `tr.APIVersion = "k6.io/v1alpha1"` and `tr.Kind = "TestRun"` before conversion.
**Warning signs:** "the server could not find the requested resource" or 400 errors on Create.

### Pitfall 2: Runner Pods Not Yet Terminated When Stage is "finished"
**What goes wrong:** The TestRun stage transitions to "finished" but runner pods may still be in `Terminating` state. Checking exit codes at this moment may find pods with nil `Terminated` state.
**Why it happens:** Kubernetes pod termination is asynchronous. The k6-operator sets the stage before all pods have fully terminated.
**How to avoid:** When listing runner pods, check that `ContainerStatuses[].State.Terminated` is non-nil. If any pod is still running, return `Running` state and let the next poll cycle retry.
**Warning signs:** Nil pointer dereference or "no terminated status" errors during exit code check.

### Pitfall 3: Dynamic Client GVR Must Use Plural Resource Name
**What goes wrong:** Using `"testrun"` instead of `"testruns"` as the resource name in `schema.GroupVersionResource` causes 404 Not Found.
**Why it happens:** Kubernetes API convention uses plural resource names in REST paths.
**How to avoid:** Always use plural: `testruns`, `privateloadzones`.
**Warning signs:** 404 on Create/Get/Delete operations.

### Pitfall 4: Exit Code 99 vs Other Non-Zero Codes
**What goes wrong:** Treating all non-zero exit codes as "thresholds failed" when some indicate infrastructure errors.
**Why it happens:** k6 has many exit codes (97-110), each with different meaning.
**How to avoid:** Exit code 0 = Passed, exit code 99 = Failed (thresholds), all other non-zero = Errored. Map to the correct RunState. [CITED: https://github.com/grafana/k6/blob/master/errext/exitcodes/codes.go]
**Warning signs:** Script errors being reported as "threshold failures" to the rollout.

### Pitfall 5: PluginConfig Field Conflicts with k6-operator Types
**What goes wrong:** Adding `corev1.ResourceRequirements` or `[]corev1.EnvVar` to PluginConfig breaks JSON backward compatibility if the field names collide with existing fields.
**Why it happens:** New Phase 8 fields in PluginConfig must coexist with Phase 1-7 Grafana Cloud fields.
**How to avoid:** Use distinct field names with `omitempty` JSON tags: `resources`, `runnerImage`, `env`, `arguments`, `parallelism`. All new fields are ignored by Grafana Cloud provider (it only reads its own fields).
**Warning signs:** Existing AnalysisTemplate YAML failing to parse after Phase 8 code changes.

### Pitfall 6: Owner Reference Requires GVK of the Owner
**What goes wrong:** Setting owner reference on the TestRun CR pointing to the AnalysisRun requires the AnalysisRun's UID, Name, APIVersion, and Kind. If any are missing, the owner reference is invalid.
**Why it happens:** The RPC context may not carry the AnalysisRun's UID. The step plugin receives a `*v1alpha1.Rollout` but not the AnalysisRun object directly.
**How to avoid:** Per D-09, attempt to set owner reference if UID is available. If not, fall back to label-based identification. Do not fail the operation if owner ref cannot be set.
**Warning signs:** "invalid owner reference" errors on Create.

## Code Examples

### TestRun CR Construction
```go
// Source: github.com/grafana/k6-operator/api/v1alpha1 (verified from source)
func buildTestRun(cfg *provider.PluginConfig, scriptCMName, scriptKey, namespace, crName string) *k6v1alpha1.TestRun {
    tr := &k6v1alpha1.TestRun{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "k6.io/v1alpha1",
            Kind:       "TestRun",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      crName,
            Namespace: namespace,
            Labels: map[string]string{
                "app.kubernetes.io/managed-by": "argo-rollouts-k6-plugin",
                "k6-plugin/rollout":            cfg.RolloutName, // from config or context
            },
        },
        Spec: k6v1alpha1.TestRunSpec{
            Script: k6v1alpha1.K6Script{
                ConfigMap: k6v1alpha1.K6Configmap{
                    Name: scriptCMName,
                    File: scriptKey,
                },
            },
            Parallelism: int32(cfg.Parallelism),
            Runner: k6v1alpha1.Pod{
                Image:     cfg.RunnerImage,
                Env:       cfg.Env,
                Resources: cfg.Resources,
            },
            Arguments: strings.Join(cfg.Arguments, " "),
            Cleanup:   "post", // auto-cleanup runner pods after test
        },
    }
    return tr
}
```

### Stage to RunState Mapping
```go
// Source: k6-operator testrun_types.go Stage constants (verified from source)
func stageToRunState(stage string) provider.RunState {
    switch stage {
    case "initialization", "initialized", "created", "started":
        return provider.Running
    case "stopped":
        return provider.Running // stopping but not yet finished
    case "finished":
        return provider.Running // need exit code check to determine pass/fail
    case "error":
        return provider.Errored
    default:
        return provider.Running // unknown stage, keep polling
    }
}
```

Note: `"finished"` returns Running because we need a second step (exit code check) to determine Passed vs Failed. The actual terminal state is set after exit code inspection.

### k6 Exit Code Constants
```go
// Source: https://github.com/grafana/k6/blob/master/errext/exitcodes/codes.go [CITED]
const (
    exitCodeSuccess           = 0
    exitCodeThresholdsFailed  = 99
    // All other non-zero codes indicate infrastructure/script errors
)

func exitCodeToRunState(code int32) provider.RunState {
    switch code {
    case 0:
        return provider.Passed
    case 99:
        return provider.Failed // thresholds breached
    default:
        return provider.Errored // script error, infra issue, etc.
    }
}
```

### CR Name Generation
```go
// Source: deterministic naming per D-10
import (
    "crypto/sha256"
    "fmt"
)

func testRunName(rolloutName, namespace string) string {
    // Hash includes namespace to avoid collisions across namespaces
    // when rollout names are the same.
    h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", namespace, rolloutName, time.Now().UnixNano())))
    short := fmt.Sprintf("%x", h[:4]) // 8 hex chars
    name := fmt.Sprintf("k6-%s-%s", rolloutName, short)
    // Kubernetes name max is 253 chars; truncate rollout name if needed
    if len(name) > 253 {
        name = name[:253]
    }
    return name
}
```

### Dynamic Client Fake for Testing
```go
// Source: k8s.io/client-go/dynamic/fake [CITED: https://pkg.go.dev/k8s.io/client-go/dynamic/fake]
import (
    dynamicfake "k8s.io/client-go/dynamic/fake"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
)

func TestTriggerRun_CreatesTestRun(t *testing.T) {
    scheme := runtime.NewScheme()
    fakeDyn := dynamicfake.NewSimpleDynamicClient(scheme)

    p := NewK6OperatorProvider(
        WithClient(fake.NewSimpleClientset(cm)),
        WithDynClient(fakeDyn),
    )
    // ... test TriggerRun creates the expected TestRun CR
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| k6-operator v0.x (untagged, alpha) | k6-operator v1.3.2 (stable) | v1.0.0 released ~2025 | Stable API, semantic versioning, no breaking changes expected in v1.x |
| TestRun only | TestRun + PrivateLoadZone | k6-operator v0.0.15+ | PLZ enables Grafana Cloud-connected in-cluster execution |
| No cleanup mechanism | `cleanup: "post"` field | k6-operator v0.0.12+ | Auto-cleanup of runner pods after test completion |
| Issue #577 open (no exit code in status) | Still open as of 2026-04 | -- | Exit code workaround remains necessary; upstream has not merged a fix [VERIFIED: gh issue view] |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Binary size impact of k6-operator import is minimal (only api/v1alpha1 symbols linked) | Standard Stack | If the entire k6 runtime gets linked, binary size could increase significantly. Verify with `go build` after import |
| A2 | k6-operator sets TestRun stage to "finished" before all runner pods terminate | Pitfall 2 | If stage is set after pod termination, the exit code check race condition doesn't exist. Verify with integration test |
| A3 | The step plugin receives enough context to extract rollout name for CR naming | D-10 / Code Examples | If rollout name is not available via RPC context, need alternative naming strategy |
| A4 | PrivateLoadZone CRD resource plural is "privateloadzones" | Architecture Patterns | If plural is different, GVR definition will fail. Verify from CRD YAML or API server |

## Open Questions

1. **AnalysisRun UID availability in RPC context**
   - What we know: Step plugin receives `*v1alpha1.Rollout`, metric plugin receives `*v1alpha1.AnalysisRun`. Both carry ObjectMeta with UID.
   - What's unclear: Whether the UID field is populated when passed via RPC (go-plugin gob encoding may zero out certain fields).
   - Recommendation: Attempt to set owner ref; log a warning and skip if UID is empty. Test with actual RPC round-trip.

2. **PrivateLoadZone GVR plural name**
   - What we know: TestRun plural is "testruns" (from CRD YAML). [VERIFIED: github.com/grafana/k6-operator/config/crd/bases/k6.io_testruns.yaml]
   - What's unclear: PrivateLoadZone plural (likely "privateloadzones" but not verified).
   - Recommendation: Check CRD YAML at `config/crd/bases/k6.io_privateloadzones.yaml` or use API discovery.

3. **TestRun cleanup: "post" behavior with abort**
   - What we know: `cleanup: "post"` tells k6-operator to clean up runner pods after test completion.
   - What's unclear: Whether "post" cleanup triggers when we DELETE the TestRun CR mid-execution (abort scenario) or only on natural completion.
   - Recommendation: Always explicitly DELETE the CR (D-08). Don't rely on "post" cleanup for abort scenarios.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Build | Yes | 1.26.2 | -- |
| k8s.io/client-go | CRD operations | Yes | v0.34.1 | -- |
| k8s.io/client-go/dynamic | Dynamic CRD client | Yes | v0.34.1 (in module) | -- |
| github.com/grafana/k6-operator | CRD types | No (not yet in go.mod) | v1.3.2 | `go get` needed |
| k6-operator in cluster | E2E testing | N/A (not local) | -- | kind + CRDs for e2e |

**Missing dependencies with no fallback:**
- `github.com/grafana/k6-operator` must be added to go.mod (Wave 0 task)

**Missing dependencies with fallback:**
- None

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify v1.11.1 |
| Config file | None (standard `go test`) |
| Quick run command | `go test -race -v -count=1 ./internal/provider/operator/...` |
| Full suite command | `make test` |

### Phase Requirements to Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| K6OP-01 | TriggerRun creates TestRun CR via dynamic client | unit | `go test -run TestTriggerRun_Creates -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-01 | GetRunResult reads TestRun stage | unit | `go test -run TestGetRunResult_Stage -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-02 | Exit code 0 maps to Passed | unit | `go test -run TestExitCode -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-02 | Exit code 99 maps to Failed | unit | `go test -run TestExitCode -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-02 | Other exit codes map to Errored | unit | `go test -run TestExitCode -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-03 | Namespace from config used for CR creation | unit | `go test -run TestNamespace -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-04 | Parallelism propagated to TestRun spec | unit | `go test -run TestParallelism -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-05 | Resources propagated to TestRun runner | unit | `go test -run TestResources -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-06 | CR name follows `k6-<rollout>-<hash>` pattern | unit | `go test -run TestCRName -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-06 | Labels include managed-by and rollout | unit | `go test -run TestLabels -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-07 | Runner image and env vars propagated | unit | `go test -run TestRunnerImage -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-07 | Arguments propagated to TestRun spec | unit | `go test -run TestArguments -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-08 | StopRun deletes TestRun CR | unit | `go test -run TestStopRun -v ./internal/provider/operator/` | No -- Wave 0 |
| K6OP-02 | Runner pods not yet terminated returns Running | unit | `go test -run TestRunnerPodsNotTerminated -v ./internal/provider/operator/` | No -- Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -race -v -count=1 ./internal/provider/operator/...`
- **Per wave merge:** `make test`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/provider/operator/testrun_test.go` -- covers K6OP-01, K6OP-04, K6OP-05, K6OP-06, K6OP-07
- [ ] `internal/provider/operator/exitcode_test.go` -- covers K6OP-02
- [ ] Add `github.com/grafana/k6-operator@v1.3.2` to go.mod
- [ ] Add `WithDynClient` functional option to K6OperatorProvider for test injection

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | Plugin inherits controller service account; no auth logic in plugin |
| V3 Session Management | No | Stateless provider; no sessions |
| V4 Access Control | Yes | RBAC on controller service account must include k6.io CRDs, pods, pods/log |
| V5 Input Validation | Yes | PluginConfig validation (ValidateK6Operator); parallelism bounds; resource limits validation |
| V6 Cryptography | No | No crypto operations (SHA256 for naming is not security-critical) |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Excessive parallelism causing resource exhaustion | Denial of Service | Validate parallelism has an upper bound; resource limits are required |
| Arbitrary container image in runner config | Elevation of Privilege | Document that runner image should come from trusted registry; no enforcement in plugin (user responsibility) |
| ConfigMap injection via crafted script content | Tampering | ConfigMap content is user-provided by design; k6-operator runs it in isolated pods with resource limits |
| Label selector collision across namespaces | Information Disclosure | CR naming includes namespace-derived hash; labels include rollout name for disambiguation |

## Sources

### Primary (HIGH confidence)
- [k6-operator v1.3.2 source: api/v1alpha1/testrun_types.go](https://github.com/grafana/k6-operator/blob/v1.3.2/api/v1alpha1/testrun_types.go) -- TestRun struct, Stage enum, ListOptions labels
- [k6-operator v1.3.2 source: api/v1alpha1/privateloadzone_types.go](https://github.com/grafana/k6-operator/blob/v1.3.2/api/v1alpha1/privateloadzone_types.go) -- PrivateLoadZone struct
- [k6-operator v1.3.2 source: api/v1alpha1/groupversion_info.go](https://github.com/grafana/k6-operator/blob/v1.3.2/api/v1alpha1/groupversion_info.go) -- GroupVersion, SchemeBuilder, AddToScheme
- [k6-operator v1.3.2 source: pkg/resources/jobs/helpers.go](https://github.com/grafana/k6-operator/blob/main/pkg/resources/jobs/helpers.go) -- Runner pod labels: `app=k6`, `k6_cr=<name>`
- [k6-operator v1.3.2 source: pkg/resources/jobs/runner.go](https://github.com/grafana/k6-operator/blob/main/pkg/resources/jobs/runner.go) -- Runner pod label `runner=true`
- [k6 exit codes](https://github.com/grafana/k6/blob/master/errext/exitcodes/codes.go) -- Exit code 0 (success), 99 (thresholds failed)
- [k8s.io/client-go/dynamic](https://pkg.go.dev/k8s.io/client-go@v0.34.1/dynamic) -- Dynamic client interface, Create/Get/Delete/List methods
- [k8s.io/client-go/dynamic/fake](https://pkg.go.dev/k8s.io/client-go@v0.34.1/dynamic/fake) -- Fake dynamic client for testing

### Secondary (MEDIUM confidence)
- [k6-operator issue #577](https://github.com/grafana/k6-operator/issues/577) -- TestRun status doesn't differentiate pass/fail; exit code workaround confirmed by maintainer
- [k6-operator issue #75](https://github.com/grafana/k6-operator/issues/75) -- k6 exit code handling; backoff limit set to 0 (no restarts)
- [Argo Rollouts plugin docs](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- Plugin runs as child process, inherits controller RBAC

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- k6-operator types verified from module cache, dynamic client API verified from pkg.go.dev, dependency resolution tested
- Architecture: HIGH -- patterns verified against k6-operator source code and existing Phase 7 codebase
- Pitfalls: HIGH -- issue #577 confirmed open, exit codes verified from k6 source, TypeMeta requirement verified from k8s.io docs

**Research date:** 2026-04-15
**Valid until:** 2026-05-15 (k6-operator v1.x is stable; k8s client-go v0.34.x is LTS)
