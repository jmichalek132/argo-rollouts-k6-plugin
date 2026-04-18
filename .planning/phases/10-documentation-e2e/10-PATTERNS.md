# Phase 10: Documentation & E2E - Pattern Map

**Mapped:** 2026-04-16
**Files analyzed:** 12 (new/modified)
**Analogs found:** 12 / 12

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `examples/k6-operator/README.md` | config (docs) | N/A | (none -- no existing example READMEs) | no-analog |
| `examples/k6-operator/clusterrole.yaml` | config (RBAC) | N/A | `examples/canary-full/secret.yaml` | role-match |
| `examples/k6-operator/analysistemplate.yaml` | config (K8s manifest) | request-response | `examples/canary-full/analysistemplate.yaml` | exact |
| `examples/k6-operator/rollout-step.yaml` | config (K8s manifest) | request-response | `examples/canary-full/rollout.yaml` | exact |
| `examples/k6-operator/rollout-metric.yaml` | config (K8s manifest) | request-response | `examples/canary-full/rollout.yaml` | exact |
| `examples/k6-operator/configmap-script.yaml` | config (K8s manifest) | N/A | `examples/canary-full/configmap-snippet.yaml` | role-match |
| `e2e/k6_operator_test.go` | test (e2e) | event-driven | `e2e/step_plugin_test.go` | exact |
| `e2e/main_test.go` (modified) | test (e2e setup) | event-driven | `e2e/main_test.go` (self -- `installArgoRollouts()`) | exact |
| `e2e/testdata/k6-script-configmap.yaml` | config (test fixture) | N/A | `e2e/testdata/analysistemplate-thresholds.yaml` | role-match |
| `e2e/testdata/analysistemplate-k6op.yaml` | config (test fixture) | request-response | `e2e/testdata/analysistemplate-thresholds.yaml` | exact |
| `e2e/testdata/rollout-step-k6op.yaml` | config (test fixture) | request-response | `e2e/testdata/rollout-step.yaml` | exact |
| `README.md` (modified) | config (docs) | N/A | `README.md` (self -- Examples table) | exact |

## Pattern Assignments

### `examples/k6-operator/clusterrole.yaml` (config, RBAC)

**Analog:** `examples/canary-full/secret.yaml` (closest static K8s resource example)

**YAML comment style** (lines 1-7):
```yaml
# Kubernetes Secret for Grafana Cloud k6 credentials.
# Replace placeholder values before applying.
#
# apiToken: Generate at Grafana Cloud UI > Testing & Synthetics >
#           Performance > Settings > Access > Personal token
# stackId:  Your Grafana Cloud stack ID (visible in the stack URL)
apiVersion: v1
```

**Pattern:** Leading `#` block comment explaining the resource purpose and user instructions, then a blank line before `apiVersion`. No trailing comments after fields. Keep the same indentation style (2-space).

---

### `examples/k6-operator/analysistemplate.yaml` (config, K8s manifest)

**Analog:** `examples/canary-full/analysistemplate.yaml`

**Full file structure** (lines 1-24):
```yaml
# Threshold-check AnalysisTemplate for the canary-full workflow.
# Included here so the example is self-contained (same as threshold-gate example).
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-threshold-check
spec:
  args:
    - name: api-token
    - name: stack-id
    - name: test-id
  metrics:
    - name: k6-thresholds
      interval: 30s
      successCondition: "result == 1"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            testId: "{{args.test-id}}"
            apiToken: "{{args.api-token}}"
            stackId: "{{args.stack-id}}"
            metric: thresholds
```

**Key pattern:** Plugin name is `jmichalek132/k6`. For k6-operator variant, replace grafana-cloud fields (`testId`, `apiToken`, `stackId`) with k6-operator fields (`provider: k6-operator`, `configMapRef`, `namespace`, `metric`). Config field names must match `internal/provider/config.go` JSON tags exactly.

**Config field reference** from `internal/provider/config.go` (lines 17-44):
```go
Provider    string        `json:"provider,omitempty"`    // "k6-operator"
ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"`
Namespace    string        `json:"namespace,omitempty"`
Metric       string        `json:"metric"`
```

---

### `examples/k6-operator/rollout-step.yaml` (config, K8s manifest)

**Analog:** `examples/canary-full/rollout.yaml`

**Rollout structure pattern** (lines 15-70):
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: canary-with-k6
spec:
  replicas: 3
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
        - name: my-app
          image: nginx:1.27
          ports:
            - containerPort: 80
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: jmichalek132/k6-step
            config:
              testId: "<YOUR_TEST_ID>"
              apiToken: "<YOUR_API_TOKEN>"
              stackId: "<YOUR_STACK_ID>"
              timeout: "10m"
```

**Key pattern:** Step plugin name is `jmichalek132/k6-step`. Config is a JSON-in-YAML block under `config:`. For k6-operator, replace grafana-cloud fields with: `provider: k6-operator`, `configMapRef.name`, `configMapRef.key`, `timeout`. No `apiToken`/`stackId`/`testId` needed.

**Comment style** (lines 1-14):
```yaml
# Complete canary workflow with k6 step plugin trigger + metric analysis gate.
#
# Flow:
#   1. Route 20% traffic to canary
#   2. Step plugin triggers a k6 Cloud test run and waits for pass/fail
#   ...
```

---

### `examples/k6-operator/rollout-metric.yaml` (config, K8s manifest)

**Analog:** `examples/canary-full/rollout.yaml` (analysis step portion)

**Analysis step pattern** (lines 48-65):
```yaml
        - analysis:
            templates:
              - templateName: k6-threshold-check
            args:
              - name: api-token
                valueFrom:
                  secretKeyRef:
                    name: k6-cloud-credentials
                    key: apiToken
              - name: stack-id
                valueFrom:
                  secretKeyRef:
                    name: k6-cloud-credentials
                    key: stackId
              - name: test-id
                value: "<YOUR_TEST_ID>"
```

**Key pattern:** For k6-operator, the analysis step references the k6-operator AnalysisTemplate. Args change to `script-configmap`, `script-key`, `namespace` instead of secret-sourced credentials.

---

### `examples/k6-operator/configmap-script.yaml` (config, K8s manifest)

**Analog:** `examples/canary-full/configmap-snippet.yaml`

**ConfigMap pattern** (lines 1-17):
```yaml
# Argo Rollouts ConfigMap snippet to register BOTH plugins (metric + step).
# Merge this into your existing argo-rollouts-config ConfigMap.
# Replace <SHA256_FROM_CHECKSUMS_TXT> with the checksums from the release.
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  metricProviderPlugins: |
    - name: "jmichalek132/k6"
      location: "https://github.com/..."
```

**Key pattern:** This new file is a k6 *script* ConfigMap (not a plugin registration ConfigMap). Different data content but same YAML structure. The k6 script goes under `data.test.js: |` as a multi-line string.

**k6 script reference** from `examples/k6-plugin-demo.js` (lines 1-21):
```javascript
import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  cloud: {
    projectID: "<YOUR_PROJECT_ID>",
    name: "k6-plugin-demo",
  },
  vus: 1,
  duration: "10s",
  thresholds: {
    http_req_failed: ["rate<0.05"],
    http_req_duration: ["p(95)<2000"],
  },
};

export default function () {
  const res = http.get("https://test.k6.io");
  check(res, { "status is 200": (r) => r.status === 200 });
  sleep(1);
}
```

**Key difference:** k6-operator script uses `iterations` (not `duration`), adds `handleSummary()`, removes `cloud` block, targets cluster-internal service URL.

---

### `e2e/k6_operator_test.go` (test, e2e)

**Analog:** `e2e/step_plugin_test.go`

**Imports pattern** (lines 1-16):
```go
//go:build e2e

package e2e

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "testing"
    "time"

    "sigs.k8s.io/e2e-framework/pkg/envconf"
    "sigs.k8s.io/e2e-framework/pkg/features"
)
```

**Test function pattern** (lines 18-80 of step_plugin_test.go):
```go
func TestStepPluginPass(t *testing.T) {
    if os.Getenv("K6_LIVE_TEST") == "true" {
        t.Skip("mock test skipped in live mode")
    }
    f := features.New("step plugin pass").
        Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            // 1. Create Service
            svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-step-e2e
  namespace: %s
...`, cfg.Namespace())
            if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
                t.Fatalf("create Service: %v", err)
            }
            // 2. Apply Rollout from testdata
            if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
                "-f", "testdata/rollout-step.yaml"); err != nil {
                t.Fatalf("apply Rollout: %v", err)
            }
            // 3. Wait for initial Healthy
            if _, err := waitForRolloutPhase(cfg, "k6-step-e2e", cfg.Namespace(), "Healthy", 2*time.Minute); err != nil {
                t.Fatalf("initial rollout did not become Healthy: %v", err)
            }
            // 4. Trigger canary via annotation patch
            if err := runKubectl(cfg, "patch", "rollout", "k6-step-e2e",
                "-n", cfg.Namespace(), "--type=merge",
                "-p", `{"spec":{"template":{"metadata":{"annotations":{"test/run":"2"}}}}}`); err != nil {
                t.Fatalf("patch rollout to trigger update: %v", err)
            }
            return ctx
        }).
        Assess("rollout advances past step plugin", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            phase, err := waitForRolloutPhase(cfg, "k6-step-e2e", cfg.Namespace(), "Healthy", 3*time.Minute)
            if err != nil {
                t.Fatalf("wait for Rollout: %v", err)
            }
            if phase != "Healthy" {
                t.Errorf("expected Rollout phase Healthy, got %s", phase)
            }
            return ctx
        }).
        Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            _ = runKubectl(cfg, "delete", "rollout", "k6-step-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
            _ = runKubectl(cfg, "delete", "service", "k6-step-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
            return ctx
        }).
        Feature()
    testenv.Test(t, f)
}
```

**Key patterns:**
- `//go:build e2e` build tag
- `K6_LIVE_TEST` skip guard (but k6-operator tests should NOT skip in live mode since they don't use mock-k6 API)
- `features.New("name").Setup().Assess().Teardown().Feature()` structure
- Inline YAML via `fmt.Sprintf` with `cfg.Namespace()`
- File-based YAML via `runKubectl(cfg, "apply", "-n", cfg.Namespace(), "-f", "testdata/...")`
- `waitForRolloutPhase` / `waitForAnalysisRun` for polling terminal state
- Teardown uses `--ignore-not-found` on all deletes
- `testenv.Test(t, f)` at the end

**Metric plugin test pattern** from `e2e/metric_plugin_test.go` (lines 18-76):
```go
func TestMetricPluginPass(t *testing.T) {
    // ... skip guard ...
    f := features.New("metric plugin pass").
        Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
                "-f", "testdata/analysistemplate-thresholds.yaml"); err != nil {
                t.Fatalf("apply AnalysisTemplate: %v", err)
            }
            arYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: metric-pass-test
  namespace: %s
...`, cfg.Namespace())
            if err := kubectlApplyStdin(cfg, arYAML); err != nil {
                t.Fatalf("create AnalysisRun: %v", err)
            }
            return ctx
        }).
        Assess("analysisrun succeeds", func(...) context.Context {
            phase, err := waitForAnalysisRun(cfg, "metric-pass-test", cfg.Namespace(), 2*time.Minute)
            // ...
        }).
        Teardown(func(...) context.Context {
            _ = runKubectl(cfg, "delete", "analysisrun", "metric-pass-test", "-n", cfg.Namespace(), "--ignore-not-found")
            _ = runKubectl(cfg, "delete", "analysistemplate", "k6-threshold-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
            return ctx
        }).Feature()
    testenv.Test(t, f)
}
```

**Wait helpers** from `e2e/metric_plugin_test.go` (lines 141-164):
```go
func waitForAnalysisRun(cfg *envconf.Config, name, namespace string, timeout time.Duration) (string, error) {
    deadline := time.Now().Add(timeout)
    var lastPhase string
    for time.Now().Before(deadline) {
        phase, err := getAnalysisRunPhase(cfg, name, namespace)
        if err != nil {
            time.Sleep(2 * time.Second)
            continue
        }
        lastPhase = phase
        switch phase {
        case "Successful", "Failed", "Error", "Inconclusive":
            return phase, nil
        }
        time.Sleep(3 * time.Second)
    }
    // Dump full JSON for diagnostics on timeout
    if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
        "get", "analysisrun", name, "-n", namespace, "-o", "json").Output(); err == nil {
        fmt.Printf("=== AnalysisRun %s/%s (timeout dump) ===\n%s\n", namespace, name, string(out))
    }
    return lastPhase, fmt.Errorf("timed out waiting for AnalysisRun %s/%s (last phase: %s)", namespace, name, lastPhase)
}
```

**New wait helper needed:** `waitForTestRunStage` -- follow same pattern as `waitForAnalysisRun` but poll `testrun` resource via `kubectl get testrun -o jsonpath={.status.stage}`.

---

### `e2e/main_test.go` (modified -- add `installK6Operator()`)

**Analog:** `e2e/main_test.go` self -- `installArgoRollouts()` function

**Install function pattern** (lines 50-71):
```go
func installArgoRollouts() env.Func {
    return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
        log.Println("Installing Argo Rollouts...")

        if err := runKubectl(cfg, "create", "namespace", "argo-rollouts"); err != nil {
            return ctx, fmt.Errorf("create argo-rollouts namespace: %w", err)
        }

        if err := runKubectl(cfg, "apply", "-n", "argo-rollouts", "-f",
            "https://github.com/argoproj/argo-rollouts/releases/download/v1.9.0/install.yaml"); err != nil {
            return ctx, fmt.Errorf("install argo rollouts: %w", err)
        }

        if err := runKubectl(cfg, "rollout", "status", "deployment/argo-rollouts",
            "-n", "argo-rollouts", "--timeout=120s"); err != nil {
            return ctx, fmt.Errorf("wait for argo-rollouts controller: %w", err)
        }

        log.Println("Argo Rollouts installed successfully")
        return ctx, nil
    }
}
```

**Key pattern:** Returns `env.Func`, uses `runKubectl` helper, applies remote URL, waits for deployment rollout status, wraps errors with `fmt.Errorf("context: %w", err)`. New `installK6Operator()` follows identical structure with k6-operator bundle URL and `k6-operator-system` namespace.

**Setup chain pattern** (lines 35-44):
```go
testenv.Setup(
    envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
    envfuncs.CreateNamespace(namespace),
    installArgoRollouts(),
    setupFn,
)
```

**Modification:** Insert `installK6Operator()` after `installArgoRollouts()`.

---

### `e2e/testdata/analysistemplate-k6op.yaml` (config, test fixture)

**Analog:** `e2e/testdata/analysistemplate-thresholds.yaml`

**Full file** (lines 1-19):
```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-threshold-e2e
spec:
  metrics:
    - name: k6-thresholds
      interval: 10s
      count: 3
      successCondition: "result == 1"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            testId: "100"
            apiToken: "test-token"
            stackId: "1"
            metric: thresholds
```

**Key pattern:** No `args` in e2e templates (hard-coded values for testing). Plugin name `jmichalek132/k6`. For k6-operator variant: replace `testId/apiToken/stackId` with `provider: k6-operator`, `configMapRef`, `metric: thresholds`. Namespace not needed in the template itself (the plugin resolves it from the AnalysisRun namespace).

---

### `e2e/testdata/rollout-step-k6op.yaml` (config, test fixture)

**Analog:** `e2e/testdata/rollout-step.yaml`

**Full file** (lines 1-36):
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: k6-step-e2e
spec:
  replicas: 1
  revisionHistoryLimit: 1
  selector:
    matchLabels:
      app: k6-step-e2e
  template:
    metadata:
      labels:
        app: k6-step-e2e
    spec:
      containers:
        - name: app
          image: k6-plugin-binaries:e2e
          imagePullPolicy: Never
          command: ["sh", "-c", "sleep 3600"]
          securityContext:
            runAsUser: 65534
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: jmichalek132/k6-step
            config:
              testId: "200"
              apiToken: "test-token"
              stackId: "1"
              timeout: "2m"
        - setWeight: 100
```

**Key patterns:**
- Uses `k6-plugin-binaries:e2e` image (already loaded into kind)
- `imagePullPolicy: Never`
- `securityContext.runAsUser: 65534`
- `sleep 3600` as the dummy app
- `replicas: 1`, `revisionHistoryLimit: 1` (minimal for tests)
- Step plugin config under `strategy.canary.steps[].plugin.config`
- For k6-operator variant: replace config with `provider: k6-operator`, `configMapRef`, `timeout`

---

### `e2e/testdata/k6-script-configmap.yaml` (config, test fixture)

**Analog:** `e2e/testdata/analysistemplate-thresholds.yaml` (same directory, test fixture role)

**Pattern:** Simple K8s manifest YAML, no comments (test fixtures don't have header comments in this codebase). Contains a ConfigMap with inline k6 script under `data.test.js: |`.

---

### `README.md` (modified -- add examples table entry)

**Analog:** `README.md` self -- Examples table

**Examples table pattern** (lines 139-147):
```markdown
## Examples

| Pattern | Directory | Description |
|---------|-----------|-------------|
| Threshold gate | [`examples/threshold-gate/`](examples/threshold-gate/) | Simplest: pass/fail based on k6 thresholds |
| Error rate + latency | [`examples/error-rate-latency/`](examples/error-rate-latency/) | Combined HTTP error rate and p95 latency gate |
| Full canary | [`examples/canary-full/`](examples/canary-full/) | Step plugin trigger + metric analysis gate |
```

**Modification:** Add a new row for k6-operator examples. Follow the same column format: Pattern name, linked directory, short description.

---

### `examples/k6-operator/README.md` (config, docs)

**No analog found.** No existing example directories contain README files. This is a new pattern.

**Closest reference:** The main `README.md` file's structure (headings, code blocks, YAML examples) can serve as a style guide. Key sections based on CONTEXT.md D-07: Prerequisites, Setup (RBAC), Usage (step plugin, metric plugin), k6 Script, Troubleshooting.

---

## Shared Patterns

### YAML Comment Style
**Source:** All files in `examples/canary-full/`
**Apply to:** All new `examples/k6-operator/*.yaml` files
```yaml
# Brief description of the resource.
# Multi-line context with specific instructions.
# Replace <PLACEHOLDER> before applying.
apiVersion: ...
```
Leading comment block, blank line optional, then the manifest. Comments use `#` with a space after. Placeholders use `<ANGLE_BRACKETS>`.

### e2e Test Lifecycle (features.New)
**Source:** `e2e/step_plugin_test.go` (lines 18-80), `e2e/metric_plugin_test.go` (lines 18-76)
**Apply to:** `e2e/k6_operator_test.go`

Structure: `features.New("name").Setup().Assess().Teardown().Feature()` with `testenv.Test(t, f)`.

Setup creates resources. Assess polls for terminal state. Teardown deletes with `--ignore-not-found`. Inline YAML uses `fmt.Sprintf` with `cfg.Namespace()`.

### e2e Wait/Poll Helpers
**Source:** `e2e/metric_plugin_test.go` (lines 141-184), `e2e/step_plugin_test.go` (lines 182-221)
**Apply to:** `e2e/k6_operator_test.go` (new `waitForTestRunStage` helper)

Pattern: deadline loop, `time.Sleep(2s)` on error, `time.Sleep(3s)` between polls, timeout dump for diagnostics, return last known state in error message.

### e2e Install Function
**Source:** `e2e/main_test.go` (lines 50-71, `installArgoRollouts()`)
**Apply to:** `e2e/main_test.go` (new `installK6Operator()`)

Pattern: Returns `env.Func`. Uses `runKubectl` for apply + rollout status wait. Error wrapping: `fmt.Errorf("verb noun: %w", err)`. Log at start and end.

### Plugin Config JSON Tags
**Source:** `internal/provider/config.go` (lines 17-44)
**Apply to:** All example YAML and e2e testdata YAML that contains plugin config

k6-operator config fields:
```json
{
  "provider": "k6-operator",
  "configMapRef": {"name": "...", "key": "..."},
  "namespace": "...",
  "metric": "thresholds",
  "timeout": "5m"
}
```

Metric plugin name: `jmichalek132/k6`. Step plugin name: `jmichalek132/k6-step`.

### kubectl Helpers
**Source:** `e2e/main_test.go` (lines 339-360)
**Apply to:** `e2e/k6_operator_test.go`

`runKubectl(cfg, args...)` for fire-and-forget commands. `kubectlApplyStdin(cfg, yamlContent)` for inline YAML application. Both use `cfg.KubeconfigFile()`.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `examples/k6-operator/README.md` | config (docs) | N/A | No existing example directories contain README files; this is a new documentation pattern for the project |

The planner should use the main `README.md` structure as a loose style guide and the RESEARCH.md Pattern 3 (RBAC) and Pattern 4 (AnalysisTemplate) for content.

## Metadata

**Analog search scope:** `examples/`, `e2e/`, `internal/provider/`, `internal/metric/`, `internal/step/`, `README.md`
**Files scanned:** 18 analog candidates read
**Pattern extraction date:** 2026-04-16
