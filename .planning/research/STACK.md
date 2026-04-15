# Stack Research: v0.3.0 In-Cluster Execution Additions

**Domain:** Argo Rollouts k6 plugin -- in-cluster execution providers
**Researched:** 2026-04-15
**Confidence:** HIGH (core libraries) / MEDIUM (k6-operator integration approach)

## Scope

This document covers ONLY the new dependencies and stack changes for v0.3.0 features:
1. ConfigMap script sourcing
2. Kubernetes Job provider (`batch/v1`)
3. k6-operator CRD support (`k6.io/v1alpha1 TestRun`)
4. Local binary execution (subprocess)

The existing stack (Go 1.24, argo-rollouts v1.9.0, hashicorp/go-plugin v1.6.3, k6-cloud-openapi-client-go, slog, testify, GoReleaser) is validated and unchanged.

## New Direct Dependencies

### Kubernetes API Access

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `k8s.io/client-go` | v0.34.1 | Create/watch Jobs and ConfigMaps, create/watch k6 TestRun CRs | Plugin runs as child process of the Argo Rollouts controller, inheriting its ServiceAccount and `/var/run/secrets/kubernetes.io/serviceaccount/` mount. `rest.InClusterConfig()` provides zero-config authentication. Already an indirect dep at v0.34.1 -- promote to direct. |
| `k8s.io/api` | v0.34.1 | `batch/v1.Job`, `core/v1.ConfigMap`, `core/v1.Pod` type definitions | Already an indirect dep at v0.34.1 -- promote to direct. Required for typed Job creation and ConfigMap reads. |

**Version rationale:** v0.34.1 is already in go.sum via argo-rollouts and e2e-framework transitive deps. Using the same version avoids diamond dependency conflicts. The k6-operator also uses v0.34.1.

### k6-operator CRD Types

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `github.com/grafana/k6-operator` | v1.3.2 | `api/v1alpha1.TestRun`, `TestRunSpec`, `K6Configmap`, `Stage` types | Typed CRD creation avoids hand-building unstructured JSON. The operator module is the only source for these Go types -- there is no separate API submodule. |

**Dependency cost assessment:** Importing k6-operator v1.3.2 pulls in `sigs.k8s.io/controller-runtime` v0.22.4 and `go.k6.io/k6` v1.7.0 as transitive deps. controller-runtime v0.22.4 is compatible with k8s.io v0.34.1 (tested combination). This adds ~15-20 transitive packages to go.sum but does NOT increase binary size beyond what's actually imported -- Go links only referenced symbols. The actual binary cost is the k6-operator API types package which is ~50KB of code.

**Alternative considered:** Dynamic client (`k8s.io/client-go/dynamic`) with `unstructured.Unstructured` to avoid importing k6-operator entirely. Rejected because:
- Hand-building TestRun JSON is error-prone (22+ fields across spec, runner, script)
- No compile-time validation of CRD structure
- Stage enum constants would need to be redefined as strings
- The k6-operator TestRun API is versioned (v1alpha1 today, v1beta1 planned per issue #623); typed imports let `go mod` catch breaking changes at upgrade time

### No New Dependencies for Subprocess Execution

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `os/exec` | stdlib | Start k6 binary as subprocess | Standard library. `exec.CommandContext` provides context cancellation for graceful shutdown. |
| `encoding/json` | stdlib | Parse k6 JSON summary output | k6's `handleSummary()` outputs JSON to stdout. Already used in the project. |

## Dependencies NOT to Add

| Technology | Why NOT |
|------------|---------|
| `go.k6.io/k6` (direct) | This is the k6 runtime engine. We need to RUN k6 (via Job or subprocess), not embed it. It comes in as a transitive dep of k6-operator but we must not import it directly. |
| `sigs.k8s.io/controller-runtime` (direct) | Transitive from k6-operator. We use raw client-go, not the controller-runtime client. Do not import controller-runtime's `client.Client` -- it adds scheme registration complexity with no benefit for our CRUD-only usage. |
| `github.com/goccy/kubejob` | Third-party Job wrapper. Adds abstraction over what's 30 lines of client-go code. Not worth the dependency. |
| `k8s.io/kubectl` | We create resources programmatically, not via kubectl-style apply. |
| Helm SDK | Out of scope per PROJECT.md. Plugin does not deploy via Helm. |

## Integration Architecture

### How the Plugin Gets Kubernetes API Access

The plugin binary runs as a **child process** of the argo-rollouts-controller pod. It inherits:
- The filesystem, including `/var/run/secrets/kubernetes.io/serviceaccount/token`
- Environment variables `KUBERNETES_SERVICE_HOST` and `KUBERNETES_SERVICE_PORT`
- The controller's ServiceAccount RBAC permissions

`rest.InClusterConfig()` reads these automatically. The plugin needs NO kubeconfig, NO explicit auth configuration.

**RBAC implication:** The argo-rollouts-controller ClusterRole must be extended with:
```yaml
# For Kubernetes Job provider
- apiGroups: ["batch"]
  resources: ["jobs"]
  verbs: ["create", "get", "watch", "delete"]
- apiGroups: [""]
  resources: ["pods", "pods/log"]
  verbs: ["get", "list", "watch"]
# For ConfigMap script sourcing
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get"]
# For k6-operator CRD provider
- apiGroups: ["k6.io"]
  resources: ["testruns"]
  verbs: ["create", "get", "watch", "delete"]
```

### Client Initialization Pattern

```go
import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

func newK8sClient() (kubernetes.Interface, error) {
    config, err := rest.InClusterConfig()
    if err != nil {
        return nil, fmt.Errorf("in-cluster config: %w", err)
    }
    return kubernetes.NewForConfig(config)
}
```

For the k6-operator CRD, use a dynamic client or register the scheme:
```go
import (
    "k8s.io/client-go/dynamic"
    k6v1alpha1 "github.com/grafana/k6-operator/api/v1alpha1"
    "k8s.io/apimachinery/pkg/runtime/schema"
)

var testRunGVR = schema.GroupVersionResource{
    Group:    "k6.io",
    Version:  "v1alpha1",
    Resource: "testruns",
}
```

**Recommendation:** Use **typed client-go** for Jobs/ConfigMaps (built-in types), use **dynamic client** for k6 TestRun CRD but marshal/unmarshal through the typed k6-operator API structs. This gives compile-time safety for CRD fields while avoiding controller-runtime's scheme registration machinery.

## ConfigMap Script Sourcing

### How It Works

The plugin reads a k6 `.js` script from a Kubernetes ConfigMap. The ConfigMap name and key are specified in the provider config (AnalysisTemplate or Rollout step spec).

### API Pattern

```go
cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
script := cm.Data[key] // string content of the .js file
```

### ConfigMap Size Limit

A single ConfigMap has a **1,048,576 byte** (1 MiB) hard limit. This is more than sufficient for k6 scripts (typical scripts are 1-10 KB). For scripts that bundle dependencies (e.g., via `k6 archive`), the limit could be a factor -- but tar archives should use the k6-operator's `volumeClaim` source instead.

### Namespace Resolution

The plugin must know which namespace to read the ConfigMap from. Two options:
1. **Explicit in config:** User specifies `configMapNamespace` in plugin config
2. **From Rollout/AnalysisRun namespace:** The RPC interface passes `*v1alpha1.AnalysisRun` (metric) or `*v1alpha1.Rollout` (step) which contains `.Namespace`

**Recommendation:** Use option 2 (derive from resource namespace) as default, with optional override. This matches how k6-operator expects ConfigMaps in the same namespace as TestRun.

## Kubernetes Job Provider

### Key Types

```go
import (
    batchv1 "k8s.io/api/batch/v1"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

### Job Spec Pattern

```go
job := &batchv1.Job{
    ObjectMeta: metav1.ObjectMeta{
        GenerateName: "k6-run-",
        Namespace:    namespace,
        Labels: map[string]string{
            "app.kubernetes.io/managed-by": "argo-rollouts-k6-plugin",
            "k6-plugin/run-id":             runID,
        },
    },
    Spec: batchv1.JobSpec{
        BackoffLimit: ptr(int32(0)), // No retries -- test failure is meaningful
        Template: corev1.PodTemplateSpec{
            Spec: corev1.PodSpec{
                RestartPolicy: corev1.RestartPolicyNever,
                Containers: []corev1.Container{{
                    Name:    "k6",
                    Image:   "grafana/k6:latest", // configurable
                    Command: []string{"k6", "run", "--out", "json=/tmp/results.json", "/scripts/test.js"},
                    VolumeMounts: []corev1.VolumeMount{{
                        Name:      "script",
                        MountPath: "/scripts",
                    }},
                }},
                Volumes: []corev1.Volume{{
                    Name: "script",
                    VolumeSource: corev1.VolumeSource{
                        ConfigMap: &corev1.ConfigMapVolumeSource{
                            LocalObjectReference: corev1.LocalObjectReference{
                                Name: configMapName,
                            },
                        },
                    },
                }},
            },
        },
    },
}
```

### Job Status Polling

```go
job, err := clientset.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{})
// Job completed: job.Status.Succeeded > 0
// Job failed: job.Status.Failed > 0
// Still running: neither
```

### Result Extraction from Job

Two approaches for getting k6 results from a completed Job:

1. **Exit code based:** k6 exits with code 99 when thresholds fail, 0 on success. Check `pod.Status.ContainerStatuses[0].State.Terminated.ExitCode`. Simple but loses metric detail.

2. **JSON summary via pod logs:** k6 script uses `handleSummary()` to write JSON to stdout. Read pod logs after completion:
   ```go
   req := clientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{})
   stream, _ := req.Stream(ctx)
   ```
   Parse the JSON for threshold results and metric values.

**Recommendation:** Use approach 2 (JSON summary via pod logs) because the plugin needs metric values (http_req_duration, http_req_failed, etc.) for the metric plugin, not just pass/fail.

### k6 Exit Codes Reference

| Exit Code | Constant | Meaning |
|-----------|----------|---------|
| 0 | (success) | Test completed, all thresholds passed |
| 99 | ThresholdsHaveFailed | One or more thresholds breached |
| 105 | ExternalAbort | Aborted by signal (SIGINT/SIGTERM) |
| 107 | ScriptException | Script runtime error |
| 108 | ScriptAborted | Script called `test.abort()` |

## k6-operator CRD Provider

### TestRun CRD Structure

```go
import k6v1alpha1 "github.com/grafana/k6-operator/api/v1alpha1"

testRun := &k6v1alpha1.TestRun{
    ObjectMeta: metav1.ObjectMeta{
        GenerateName: "k6-run-",
        Namespace:    namespace,
    },
    Spec: k6v1alpha1.TestRunSpec{
        Parallelism: 1, // configurable
        Script: k6v1alpha1.K6Script{
            ConfigMap: k6v1alpha1.K6Configmap{
                Name: configMapName,
                File: "test.js",
            },
        },
        Runner: k6v1alpha1.Pod{
            Image: "grafana/k6:latest", // configurable
        },
        Cleanup: k6v1alpha1.Cleanup("post"), // auto-cleanup after completion
    },
}
```

### TestRun Stage Lifecycle

```
initialization -> initialized -> created -> started -> stopped -> finished
                                                    \-> error
```

Terminal stages: `finished`, `error`, `stopped`.

| Stage | Meaning |
|-------|---------|
| `initialization` | Initializer job is running, validating script |
| `initialized` | Script validated, ready to create runners |
| `created` | Runner jobs and services created |
| `started` | Test execution active on all runners |
| `stopped` | Test execution stopped (may be normal or forced) |
| `finished` | All runner jobs completed |
| `error` | Error during any phase |

### CRD Operations via Dynamic Client

```go
import (
    "k8s.io/client-go/dynamic"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
)

// Create TestRun
unstructuredObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRun)
result, err := dynamicClient.Resource(testRunGVR).Namespace(ns).Create(ctx,
    &unstructured.Unstructured{Object: unstructuredObj},
    metav1.CreateOptions{},
)

// Get TestRun status
result, err := dynamicClient.Resource(testRunGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
var status k6v1alpha1.TestRunStatus
runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object["status"].(map[string]interface{}), &status)
stage := status.Stage // "finished", "error", etc.
```

### Result Extraction from TestRun

The k6-operator TestRun status does NOT expose k6 metric values (http_req_duration, etc.). To get metrics:
1. **Pod logs approach:** Same as Job provider -- read runner pod logs for handleSummary JSON output
2. **Prometheus remote-write:** k6 can write metrics to Prometheus, then query Prometheus. Out of scope for v0.3.0.

**Recommendation:** Use pod logs approach. The runner pods are named `<testrun-name>-<index>` and created in the same namespace.

## Local Binary Execution (Subprocess)

### Research Scope

This is a **research** item for v0.3.0, not necessarily a full implementation. Key findings:

### Feasibility: YES with caveats

The plugin can execute `k6 run script.js` as a subprocess using `os/exec`. The k6 binary must be present in the controller pod's filesystem (via init container or baked into a custom image).

### Execution Pattern

```go
import "os/exec"

ctx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()

cmd := exec.CommandContext(ctx, "k6", "run",
    "--quiet",
    "--out", "json=-",  // JSON metrics to stdout
    scriptPath,
)
cmd.Env = append(os.Environ(),
    "K6_NO_USAGE_REPORT=true",
)
output, err := cmd.CombinedOutput()
// Parse exit code: 0 = pass, 99 = thresholds failed
```

### Caveats

1. **k6 binary availability:** Must be present in the controller pod. This means either a custom controller image or an init container that downloads k6. Neither is clean.
2. **Resource consumption:** k6 load generation runs IN the controller pod, competing for CPU/memory with the Argo Rollouts controller itself. This is dangerous for production.
3. **No parallelism:** Single k6 process, no distributed execution.
4. **Script delivery:** Script must be on the filesystem. ConfigMap mount or inline are the only options.

### Recommendation

Local binary execution is viable for **testing and development** but should carry strong warnings against production use. The Kubernetes Job provider is the correct production path for in-cluster execution. Document local binary as an experimental/dev-only provider.

## go.mod Changes

### Promote from indirect to direct

```
k8s.io/api v0.34.1
k8s.io/client-go v0.34.1
```

### Add new direct dependency

```
github.com/grafana/k6-operator v1.3.2
```

### Resulting go.mod require block

```
require (
    github.com/argoproj/argo-rollouts v1.9.0
    github.com/grafana/k6-cloud-openapi-client-go v0.0.0-20251022100644-dd6cfbb68f85
    github.com/grafana/k6-operator v1.3.2
    github.com/hashicorp/go-plugin v1.6.3
    github.com/stretchr/testify v1.11.1
    k8s.io/api v0.34.1
    k8s.io/apimachinery v0.34.1
    k8s.io/client-go v0.34.1
    sigs.k8s.io/e2e-framework v0.6.0
)
```

## Version Compatibility Matrix

| Package | Version | Compatible With | Notes |
|---------|---------|-----------------|-------|
| k8s.io/client-go v0.34.1 | k8s.io/api v0.34.1 | Must match minor version. Both already in go.sum. |
| k8s.io/client-go v0.34.1 | sigs.k8s.io/controller-runtime v0.22.4 | Tested combination. controller-runtime v0.22 targets client-go v0.34. |
| github.com/grafana/k6-operator v1.3.2 | k8s.io/api v0.34.1 | k6-operator's go.mod uses v0.34.1 -- perfect alignment. |
| github.com/grafana/k6-operator v1.3.2 | Go 1.24+ | k6-operator requires Go 1.25; our go.mod says `go 1.24.9` which satisfies (toolchain auto-download). |
| os/exec (stdlib) | Go 1.24+ | Always available. `CommandContext` added in Go 1.7. |

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| K8s client | client-go v0.34.1 (typed + dynamic) | controller-runtime client | controller-runtime's client requires scheme registration, runtime.Scheme, and manager setup -- overkill for CRUD operations. client-go is lower-level but we need only Create/Get/Watch/Delete. |
| CRD interaction | Dynamic client + k6-operator types | Pure dynamic client (no k6-operator import) | Hand-building unstructured TestRun JSON is error-prone. 22+ fields. No compile-time safety. |
| CRD interaction | Dynamic client + k6-operator types | controller-runtime typed client | Requires scheme registration and a full manager. We're not building a controller -- we just create CRs. |
| Job result extraction | Pod logs (JSON summary) | Exit code only | Exit code tells pass/fail but not metric values. The metric plugin needs http_req_duration, http_req_failed, etc. |
| Job result extraction | Pod logs (JSON summary) | Shared volume + JSON file | More complex (emptyDir volume, sidecar or post-completion read), and pod logs are simpler since k6 can output JSON to stdout via handleSummary(). |
| k6 image | `grafana/k6:latest` (configurable) | Custom image with extensions | Default to official image. Allow override via config for users who need xk6 extensions. |
| Local binary | os/exec (stdlib) | Embed k6 as Go library | go.k6.io/k6 is the runtime, not a library API. Embedding it couples the plugin to k6 internals and adds ~50MB to binary. |

## Sources

- [k8s.io/client-go rest.InClusterConfig](https://github.com/kubernetes/client-go/blob/master/examples/in-cluster-client-configuration/main.go) -- HIGH confidence
- [k8s.io/client-go batch/v1 Job interface](https://pkg.go.dev/k8s.io/client-go/kubernetes/typed/batch/v1) -- HIGH confidence
- [k8s.io/api batch/v1 types](https://pkg.go.dev/k8s.io/api/batch/v1) -- HIGH confidence
- [grafana/k6-operator v1.3.2 releases](https://github.com/grafana/k6-operator/releases) -- HIGH confidence
- [grafana/k6-operator api/v1alpha1 types](https://pkg.go.dev/github.com/grafana/k6-operator/api/v1alpha1) -- HIGH confidence
- [k6-operator go.mod (confirms k8s v0.34.1)](https://github.com/grafana/k6-operator/blob/main/go.mod) -- HIGH confidence
- [k6-operator TestRun CRD docs](https://grafana.com/docs/k6/latest/set-up/set-up-distributed-k6/usage/executing-k6-scripts-with-testrun-crd/) -- HIGH confidence
- [k6-operator execution stages](https://deepwiki.com/grafana/k6-operator/2.3-test-execution-flow) -- MEDIUM confidence
- [k6 exit codes](https://github.com/grafana/k6/blob/master/errext/exitcodes/codes.go) -- HIGH confidence
- [k6 handleSummary JSON output](https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/) -- HIGH confidence
- [k6 subprocess behavior (issue #3744)](https://github.com/grafana/k6/issues/3744) -- MEDIUM confidence
- [Argo Rollouts plugin RBAC inheritance](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- HIGH confidence
- [controller-runtime v0.22 compatibility](https://github.com/kubernetes-sigs/controller-runtime/releases) -- HIGH confidence

---
*Stack research for: argo-rollouts-k6-plugin v0.3.0 in-cluster execution*
*Researched: 2026-04-15*
