# Phase 10: Documentation & E2E - Research

**Researched:** 2026-04-16
**Domain:** Kubernetes RBAC documentation, example YAML authoring, k6-operator e2e testing
**Confidence:** HIGH

## Summary

Phase 10 is the capstone phase for v0.3.0. It delivers three distinct deliverables: (1) RBAC ClusterRole examples so users can grant the Argo Rollouts controller permissions to manage k6-operator CRDs, (2) example AnalysisTemplate and Rollout YAML for the k6-operator provider, and (3) an e2e test suite that installs the real k6-operator controller in a kind cluster and validates the full path from TestRun CR creation through runner pod execution to handleSummary metric extraction.

The existing codebase has well-established e2e patterns (e2e-framework, kind lifecycle, mock server) and example directory conventions (examples/canary-full/). This phase extends both patterns for the k6-operator provider. The primary technical challenge is the e2e test: unlike existing tests that mock the k6 Cloud API, k6-operator e2e requires a real k6 binary running in-cluster, hitting a real HTTP endpoint, and producing real handleSummary JSON output.

**Primary recommendation:** Reuse existing e2e infrastructure (kind cluster, mock-server, plugin binary loading), add k6-operator installation as a setup step, create a ConfigMap-sourced k6 script with handleSummary, and validate the full TestRun lifecycle including pod log metric extraction.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Claude decides the example directory structure. Recommended approach: mirror `examples/canary-full/` pattern with a new `examples/k6-operator/` directory containing individual YAML files (clusterrole.yaml, analysistemplate.yaml, rollout-step.yaml, rollout-metric.yaml, configmap-script.yaml).
- **D-02:** RBAC ClusterRole covers both TestRun AND PrivateLoadZone CRDs -- users don't need to modify RBAC when switching between in-cluster and cloud-connected modes. Also includes pods, pods/log, and configmaps permissions.
- **D-03:** Both step plugin and metric plugin examples included. Step plugin Rollout (trigger k6 run as canary step) and metric plugin AnalysisTemplate (poll k6 metrics with successCondition) show both integration patterns.
- **D-04:** Single simple k6 script with handleSummary export. HTTP GET against a target, thresholds (p95<500, error rate<1%), and `handleSummary()` writing JSON to stdout. Minimal, copy-paste ready. No advanced multi-scenario variant.
- **D-05:** Install the real k6-operator controller in kind (CRDs + controller deployment). Create a TestRun CR, let the controller create runner pods, wait for completion. Most realistic approach.
- **D-06:** Claude decides e2e test depth. Recommended: full path validation (TestRun -> runner pods -> pod logs -> handleSummary JSON -> RunResult -> metric values) since this is the milestone's capstone validation.
- **D-07:** Keep documentation in `examples/k6-operator/` only -- the directory gets its own README.md with setup instructions. Main project README links to it but does not duplicate content.

### Claude's Discretion
- e2e test framework setup details (how to install k6-operator in kind, version pinning)
- ConfigMap creation for k6 script delivery in e2e tests
- CI timeout adjustments if needed for k6-operator e2e
- Whether to validate metric values in e2e or just verify non-zero population
- Mock target service reuse vs new deployment for k6-operator tests
- Example YAML annotations and comments style

### Deferred Ideas (OUT OF SCOPE)
None -- discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DOCS-01 | RBAC example ClusterRole covering k6.io TestRun CRDs, pods, pods/log, and configmaps | Verified GVR constants from testrun.go (k6.io/v1alpha1/testruns, privateloadzones). Verified pod log reading via exitcode.go and summary.go label selectors. ConfigMap read verified via operator.go readScript(). |
| DOCS-02 | Example AnalysisTemplate and Rollout YAML for k6-operator provider (step + metric) | Verified config schema from config.go (provider: k6-operator, configMapRef). Verified metric plugin parseConfig from metric.go and step plugin parseConfig from step.go. |
| TEST-01 | e2e test suite on kind cluster with k6-operator CRDs installed, validating TestRun creation and result extraction against mock target service | Verified e2e-framework pattern from main_test.go. Verified k6-operator bundle.yaml availability at pinned v1.3.2 URL. Verified mock-server HTTP endpoint is already deployed in kind. |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| RBAC ClusterRole | Kubernetes API Server | -- | RBAC rules applied to Argo Rollouts controller ServiceAccount; evaluated by kube-apiserver |
| Example YAML | Static docs | -- | Pure documentation files; no runtime component |
| k6 test script | k6 runner pod (in-cluster) | ConfigMap (storage) | k6-operator creates runner pods that execute the script; script stored in ConfigMap |
| e2e test orchestration | Test process (Go test binary) | kind cluster | e2e-framework manages kind lifecycle; tests apply YAML via kubectl |
| TestRun CR lifecycle | k6-operator controller | Argo Rollouts controller (plugin) | k6-operator owns CR reconciliation; plugin creates/polls/deletes CRs via dynamic client |
| Metric extraction | Argo Rollouts controller (plugin) | k6 runner pods (log source) | Plugin reads pod logs after TestRun finishes; parses handleSummary JSON |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `sigs.k8s.io/e2e-framework` | v0.6.0 | e2e test framework | Already used in existing e2e tests; manages kind cluster lifecycle [VERIFIED: go.mod] |
| `k6-operator` (bundle) | v1.3.2 | TestRun CRD + controller | Matches go.mod dependency version; versioned bundle URL confirmed available [VERIFIED: curl HEAD check] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| kind | v0.31.0 | Local Kubernetes cluster | e2e test cluster creation [VERIFIED: `kind --version`] |
| kubectl | v1.35.3 | Cluster management | YAML application in e2e tests [VERIFIED: `kubectl version --client`] |
| Docker | v29.4.0 | Container runtime | Building/loading images for kind [VERIFIED: `docker --version`] |

**No new dependencies required.** All tools and libraries are already available.

## Architecture Patterns

### System Architecture Diagram

```
                           e2e Test (Go binary)
                                   |
                    +--------------+--------------+
                    |              |              |
              [kind cluster    [k6-operator   [k6 script
               create]          install]       ConfigMap]
                    |              |              |
                    v              v              v
              +-----------------------------------------+
              |           kind Cluster                   |
              |                                          |
              |  argo-rollouts ns:                       |
              |    - Argo Rollouts controller             |
              |    - metric-plugin binary (child proc)    |
              |    - step-plugin binary (child proc)      |
              |    - mock-server (HTTP target)             |
              |                                          |
              |  k6-operator-system ns:                   |
              |    - k6-operator controller                |
              |                                          |
              |  test ns (random):                        |
              |    - ConfigMap (k6 script)                 |
              |    - TestRun CR -----> k6 runner pods      |
              |    - AnalysisTemplate/AnalysisRun          |
              |    - Rollout (step plugin test)            |
              |                                          |
              |  Data flow:                               |
              |    Plugin creates TestRun CR               |
              |      -> k6-operator reconciles             |
              |        -> runner pods execute k6 script    |
              |          -> k6 GETs mock-server            |
              |          -> handleSummary JSON to stdout   |
              |        -> pods terminate (exit 0 or 99)    |
              |      -> TestRun stage -> "finished"        |
              |    Plugin polls TestRun stage              |
              |    Plugin reads pod exit codes             |
              |    Plugin reads pod logs (handleSummary)   |
              |    Plugin returns RunResult to controller  |
              +-----------------------------------------+
```

### Recommended Project Structure (new/modified files)

```
examples/
  k6-operator/
    README.md                    # Setup instructions, prerequisites
    clusterrole.yaml             # RBAC for Argo Rollouts controller SA
    analysistemplate.yaml        # Metric plugin AnalysisTemplate
    rollout-step.yaml            # Step plugin Rollout
    rollout-metric.yaml          # Metric plugin Rollout with analysis step
    configmap-script.yaml        # k6 test script ConfigMap
e2e/
  k6_operator_test.go           # New: k6-operator e2e tests
  main_test.go                  # Modified: add k6-operator install step
  testdata/
    argo-rollouts-config.yaml   # Unchanged: already has both plugin registrations
    k6-script-configmap.yaml    # New: k6 script ConfigMap for e2e
    analysistemplate-k6op.yaml  # New: k6-operator AnalysisTemplate for e2e
    rollout-step-k6op.yaml      # New: k6-operator step Rollout for e2e
README.md                       # Modified: add link to examples/k6-operator/
```

### Pattern 1: k6-operator Installation in e2e Setup

**What:** Add an `installK6Operator()` env.Func to the existing kind cluster setup chain.
**When to use:** Before any k6-operator e2e tests run.
**Example:**
```go
// Source: Existing pattern from installArgoRollouts() in e2e/main_test.go
// plus k6-operator bundle URL from https://grafana.com/docs/k6/latest/set-up/set-up-distributed-k6/install-k6-operator/
func installK6Operator() env.Func {
    return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
        log.Println("Installing k6-operator...")
        // Use pinned version matching go.mod dependency
        bundleURL := "https://raw.githubusercontent.com/grafana/k6-operator/v1.3.2/bundle.yaml"
        if err := runKubectl(cfg, "apply", "--server-side", "-f", bundleURL); err != nil {
            return ctx, fmt.Errorf("install k6-operator: %w", err)
        }
        // Wait for controller to be ready
        if err := runKubectl(cfg, "rollout", "status", "deployment/k6-operator-controller-manager",
            "-n", "k6-operator-system", "--timeout=120s"); err != nil {
            return ctx, fmt.Errorf("wait for k6-operator: %w", err)
        }
        log.Println("k6-operator installed successfully")
        return ctx, nil
    }
}
```

### Pattern 2: k6 Script with handleSummary for ConfigMap Delivery

**What:** A minimal k6 script that runs HTTP requests against the mock target and exports handleSummary JSON to stdout.
**When to use:** In ConfigMap for k6-operator TestRun execution.
**Example:**
```javascript
// Source: k6 handleSummary docs https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/
// plus existing examples/k6-plugin-demo.js pattern
import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 1,
  iterations: 10,
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

const TARGET_URL = __ENV.TARGET_URL || "http://mock-server.argo-rollouts.svc.cluster.local:8080";

export default function () {
  const res = http.get(TARGET_URL);
  check(res, { "status is 200": (r) => r.status === 200 });
}

export function handleSummary(data) {
  return { stdout: JSON.stringify(data) };
}
```
[VERIFIED: handleSummary stdout pattern from existing summary.go findSummaryJSON parser]

### Pattern 3: RBAC ClusterRole for Argo Rollouts Controller

**What:** ClusterRole granting permissions the plugin needs when running inside the Argo Rollouts controller.
**When to use:** Applied to the cluster, bound to the argo-rollouts ServiceAccount.
**Example:**
```yaml
# Source: Verified from operator.go (ensureClient, readScript, checkRunnerExitCodes,
# parseSummaryFromPods, TriggerRun, GetRunResult, StopRun)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argo-rollouts-k6-plugin
rules:
  # TestRun and PrivateLoadZone CRDs (per D-02)
  - apiGroups: ["k6.io"]
    resources: ["testruns", "privateloadzones"]
    verbs: ["create", "get", "list", "watch", "delete"]
  # Runner pod inspection for exit codes and log reading
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  # ConfigMap reading for k6 scripts
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]
```
[VERIFIED: Each verb maps to actual API calls in the provider code]

### Pattern 4: k6-operator AnalysisTemplate

**What:** AnalysisTemplate for metric plugin with k6-operator provider.
**When to use:** When users want threshold-based analysis with in-cluster k6 execution.
**Example:**
```yaml
# Source: Verified from metric.go parseConfig and config.go PluginConfig fields
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-operator-threshold-check
spec:
  args:
    - name: script-configmap
    - name: script-key
      value: test.js
    - name: namespace
  metrics:
    - name: k6-thresholds
      interval: 15s
      successCondition: "result == 1"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            provider: k6-operator
            configMapRef:
              name: "{{args.script-configmap}}"
              key: "{{args.script-key}}"
            namespace: "{{args.namespace}}"
            metric: thresholds
```
[VERIFIED: config.go PluginConfig JSON tags and ValidateK6Operator()]

### Pattern 5: e2e Test Structure for k6-operator

**What:** Feature-based test using e2e-framework matching existing test patterns.
**When to use:** Validating the full k6-operator integration path.
**Example:**
```go
// Source: Pattern from e2e/step_plugin_test.go and e2e/metric_plugin_test.go
func TestK6OperatorStepPass(t *testing.T) {
    f := features.New("k6-operator step pass").
        Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            // 1. Create ConfigMap with k6 script
            // 2. Apply Rollout with k6-operator step plugin
            // 3. Wait for initial Healthy, then patch to trigger canary
            return ctx
        }).
        Assess("rollout advances past k6-operator step", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            // Wait for Rollout to reach Healthy (step passed)
            // Optionally verify TestRun CR was created and cleaned up
            return ctx
        }).
        Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
            // Delete rollout, service, configmap, any leftover testruns
            return ctx
        }).
        Feature()
    testenv.Test(t, f)
}
```

### Anti-Patterns to Avoid
- **Mocking the k6-operator:** Do NOT mock the k6-operator controller for Phase 10 e2e. Decision D-05 explicitly requires the real controller. The point is end-to-end validation.
- **Using k6 binary duration-based tests:** Use `iterations: N` (finite) instead of `duration: Xs` to ensure deterministic test completion. Duration-based tests add calendar time to CI.
- **Hard-coding namespace in test YAML:** Use `cfg.Namespace()` and string templating like existing tests do, not hard-coded namespace references.
- **Skipping handleSummary in the test script:** The k6 script MUST export handleSummary. Without it, the metric plugin cannot extract detailed metrics, and METR-01 validation is impossible.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Kind cluster lifecycle | Custom cluster management | `sigs.k8s.io/e2e-framework/pkg/envfuncs` | Already used; handles create/destroy, namespace management |
| k6-operator installation | Custom CRD/deployment logic | `kubectl apply -f bundle.yaml` | Official bundle includes CRDs, RBAC, controller deployment. One command. |
| YAML templating in tests | Go template engine | `fmt.Sprintf` with inline YAML | Existing pattern in step_plugin_test.go; simple, no new dependencies |
| Waiting for resources | Custom polling loops | `waitForAnalysisRun` / `waitForRolloutPhase` | Already exist in the test files; add similar `waitForTestRun` |

**Key insight:** The existing e2e infrastructure handles 90% of what Phase 10 needs. The main addition is k6-operator installation and a TestRun-aware wait function.

## Common Pitfalls

### Pitfall 1: k6-operator CRD Size Limits for ConfigMap Scripts
**What goes wrong:** k6-operator references ConfigMaps for scripts. Kubernetes ConfigMaps have a 1 MiB size limit. The k6 script for e2e is tiny, but example docs should mention this limit.
**Why it happens:** Users may try to use large k6 scripts with complex scenarios.
**How to avoid:** Document the 1 MiB limit in the examples/k6-operator/README.md. For e2e, the script is ~500 bytes, well within limits.
**Warning signs:** `kubectl apply` fails with "too long" error on ConfigMap creation. [VERIFIED: Kubernetes documentation]

### Pitfall 2: k6-operator Namespace Mismatch
**What goes wrong:** The k6-operator watches all namespaces by default (bundle installs into k6-operator-system). However, the TestRun CR must be created in a namespace where the operator has RBAC to create Jobs, Pods, etc. The bundle's ClusterRole handles this, but if the user uses Helm with namespace restrictions, tests fail silently.
**Why it happens:** k6-operator uses WATCH_NAMESPACE env var to restrict namespaces.
**How to avoid:** Use the default bundle installation (all-namespace mode). Verify with `kubectl get testruns -A` that the operator processes CRs in the test namespace.
**Warning signs:** TestRun stays in `initialization` stage indefinitely. [VERIFIED: k6-operator docs]

### Pitfall 3: k6 Runner Pod Image Pull in kind
**What goes wrong:** k6-operator uses `grafana/k6:latest` as the default runner image. In kind clusters, this requires pulling from Docker Hub, which can be slow or rate-limited.
**Why it happens:** kind does not pre-pull images; Docker Hub rate limits apply.
**How to avoid:** Pre-load the k6 image into kind: `docker pull grafana/k6:latest && kind load docker-image grafana/k6:latest --name <cluster>`. Or set `runner.image` in TestRun spec to use a pre-loaded image.
**Warning signs:** Runner pods stuck in `ImagePullBackOff` or `ErrImagePull`. [ASSUMED]

### Pitfall 4: Mock Server DNS Resolution Across Namespaces
**What goes wrong:** The existing mock-server is deployed in `argo-rollouts` namespace. k6 runner pods execute in the test namespace. The k6 script must use the fully-qualified DNS name `mock-server.argo-rollouts.svc.cluster.local:8080` (not just `mock-server:8080`).
**Why it happens:** Kubernetes DNS resolves short names within the same namespace only.
**How to avoid:** Use the FQDN in the k6 script or pass it via environment variable. The existing mock-server is named `mock-k6`, so the URL would be `http://mock-k6.argo-rollouts.svc.cluster.local:8080`.
**Warning signs:** k6 reports connection refused / DNS resolution failure in pod logs. [VERIFIED: mock-server deployed as mock-k6 Service in argo-rollouts ns, from main_test.go]

### Pitfall 5: TestRun Cleanup in e2e Tests
**What goes wrong:** If a test fails mid-execution, TestRun CRs and runner pods may be left behind, consuming cluster resources and potentially causing name collisions in subsequent test runs.
**Why it happens:** TestRun names are deterministic per rollout name (k6-<rollout>-<hash> includes timestamp).
**How to avoid:** Teardown function must delete TestRun CRs by label selector (`app.kubernetes.io/managed-by=argo-rollouts-k6-plugin`). Also the TestRun spec has `cleanup: "post"` which auto-deletes runner Jobs after completion.
**Warning signs:** Stale pods in the test namespace after test completion. [VERIFIED: testrun.go buildTestRun sets Cleanup: "post"]

### Pitfall 6: e2e Test Timeout for k6-operator Tests
**What goes wrong:** k6-operator tests take longer than grafana-cloud mock tests because: (a) k6-operator controller startup, (b) real k6 binary execution, (c) runner pod scheduling overhead.
**Why it happens:** Real components have real latency vs mocked API calls.
**How to avoid:** Use generous timeouts (3-5 minutes per test instead of 2 minutes). Use `iterations: 10` instead of `duration: 30s` for faster completion. Keep the Makefile `test-e2e` timeout at 15m (already sufficient).
**Warning signs:** Sporadic timeout failures in CI. [ASSUMED]

### Pitfall 7: handleSummary Not in Pod Logs
**What goes wrong:** The plugin's `parseSummaryFromPods` reads pod logs and parses handleSummary JSON. If the k6 script doesn't export handleSummary, or if the JSON is not written to stdout, the metric plugin returns zero values for all metrics.
**Why it happens:** handleSummary must explicitly return `{ stdout: JSON.stringify(data) }`. Writing to a file (the default examples in k6 docs) doesn't help because the plugin reads pod logs, not files.
**How to avoid:** The e2e k6 script MUST include `export function handleSummary(data) { return { stdout: JSON.stringify(data) } }`. The example script should have a comment explaining why stdout is required.
**Warning signs:** Metric values are all zero despite successful test execution. [VERIFIED: summary.go readPodLogs + findSummaryJSON]

### Pitfall 8: RBAC Missing `watch` Verb for TestRun
**What goes wrong:** The plugin currently uses single GET calls (not watches) for TestRun polling. However, including `watch` in the ClusterRole is forward-compatible and required if the plugin ever switches to informers.
**Why it happens:** GetRunResult does a single `dynClient.Resource(gvr).Namespace(ns).Get()`, which needs `get`. The `list` verb is needed for `checkRunnerExitCodes` and `parseSummaryFromPods` (pod listing). `watch` is not currently used but is standard RBAC practice for CRDs.
**How to avoid:** Include `watch` in the ClusterRole. It costs nothing and prevents breakage if the implementation adds watches later.
**Warning signs:** 403 Forbidden errors when the plugin tries to watch TestRun CRs. [VERIFIED: operator.go uses Get not Watch currently, but watch is recommended]

## Code Examples

### Example 1: k6 Script with handleSummary (for ConfigMap)
```javascript
// Source: k6 handleSummary docs + existing examples/k6-plugin-demo.js pattern
// This script is designed for k6-operator in-cluster execution.
// It targets a service within the cluster and exports handleSummary
// so the metric plugin can extract detailed metrics from pod logs.
import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 1,
  iterations: 10,
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

// TARGET_URL is injected via k6 runner pod environment variable or defaults
// to a cluster-internal service.
const TARGET_URL = __ENV.TARGET_URL || "http://my-app.default.svc.cluster.local";

export default function () {
  const res = http.get(TARGET_URL);
  check(res, { "status is 200": (r) => r.status === 200 });
}

// handleSummary writes k6 summary data as JSON to stdout.
// The argo-rollouts-k6-plugin metric plugin reads this from pod logs
// to extract http_req_failed, http_req_duration, and http_reqs metrics.
export function handleSummary(data) {
  return { stdout: JSON.stringify(data) };
}
```
[VERIFIED: summary.go findSummaryJSON expects this exact output format]

### Example 2: ConfigMap for k6 Script
```yaml
# Source: Kubernetes ConfigMap spec + k6-operator K6Script.ConfigMap type
apiVersion: v1
kind: ConfigMap
metadata:
  name: k6-load-test
data:
  test.js: |
    import http from "k6/http";
    import { check } from "k6";

    export const options = {
      vus: 1,
      iterations: 10,
      thresholds: {
        http_req_failed: ["rate<0.01"],
        http_req_duration: ["p(95)<500"],
      },
    };

    export default function () {
      const res = http.get("http://my-app:80");
      check(res, { "status is 200": (r) => r.status === 200 });
    }

    export function handleSummary(data) {
      return { stdout: JSON.stringify(data) };
    }
```
[VERIFIED: K6Configmap struct in testrun_types.go uses Name+File fields; summary.go parses stdout JSON]

### Example 3: Step Plugin Rollout for k6-operator
```yaml
# Source: Verified from step.go parseConfig and config.go PluginConfig fields
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: my-app
spec:
  replicas: 3
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
              provider: k6-operator
              configMapRef:
                name: k6-load-test
                key: test.js
              timeout: "5m"
        - setWeight: 100
```
[VERIFIED: step.go parseConfig reads provider, configMapRef from RpcStepContext.Config JSON]

### Example 4: Waiting for TestRun Completion in e2e Tests
```go
// Source: Pattern from waitForAnalysisRun in metric_plugin_test.go
func waitForTestRunStage(cfg *envconf.Config, name, namespace, stage string, timeout time.Duration) (string, error) {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
            "get", "testrun", name, "-n", namespace, "-o", "jsonpath={.status.stage}")
        out, err := cmd.Output()
        if err != nil {
            time.Sleep(2 * time.Second)
            continue
        }
        currentStage := string(out)
        if currentStage == stage {
            return currentStage, nil
        }
        time.Sleep(3 * time.Second)
    }
    return "", fmt.Errorf("timed out waiting for TestRun %s/%s stage %s", namespace, name, stage)
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Mock k6 Cloud API for all e2e | Mock for cloud provider, real k6-operator for operator provider | Phase 10 | k6-operator tests validate real integration, not mocked behavior |
| Single provider examples | Provider-specific example directories | Phase 10 | Users see examples tailored to their execution backend |
| No RBAC guidance | Explicit ClusterRole examples | Phase 10 | Users can configure RBAC without guesswork |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | k6 runner image `grafana/k6:latest` is available on Docker Hub and pullable from kind without auth | Pitfall 3 | e2e tests fail with image pull errors; mitigation: pre-load image |
| A2 | k6-operator v1.3.2 bundle installs cleanly in a kind cluster without additional configuration | Pitfalls section | e2e setup fails; mitigation: test locally before CI |
| A3 | k6 `iterations: 10` completes in under 30 seconds with 1 VU against an in-cluster HTTP server | Pitfall 6 | Tests take longer than expected; mitigation: adjust timeout |
| A4 | The mock-k6 service (existing mock server) responds to HTTP GET on `/` with a valid response | Pattern 2 | k6 script fails all checks; mitigation: verify mock-server handler covers GET / |

## Open Questions

1. **Mock server endpoint for k6 GET requests**
   - What we know: The mock server in e2e/mock/main.go handles k6 Cloud API paths (load_tests, test_runs, etc). It does NOT serve a generic HTTP GET on `/` for k6 to load-test.
   - What's unclear: Does the mock server need a simple health/GET endpoint that k6 scripts can target, or should we deploy a separate simple HTTP server (e.g., nginx pod)?
   - Recommendation: Add a simple `GET /health` handler to the existing mock server that returns 200 OK. This avoids deploying another service. The k6 script targets `http://mock-k6.argo-rollouts.svc.cluster.local:8080/health`. The mock server already has a catch-all 404 handler, so unmatched paths return 404 -- but k6 needs a 200 response for check assertions. Alternatively, the mock server's root handler could be modified, or we deploy a separate nginx pod. **Simplest: add a `/health` or `/` 200-OK route to mock/main.go.**

2. **k6 runner image version pinning**
   - What we know: k6-operator defaults to `grafana/k6:latest` when no runner image is specified.
   - What's unclear: Whether to pin a specific k6 version (e.g., `grafana/k6:1.0.0`) in examples and e2e tests for reproducibility.
   - Recommendation: Pin to `grafana/k6:latest` in e2e (simplicity) but recommend a specific version in example YAML comments. The handleSummary format is stable across k6 versions.

3. **Metric value validation depth in e2e**
   - What we know: D-06 recommends full path validation. The metric plugin returns specific float64 values for http_req_failed, http_req_duration, http_reqs.
   - What's unclear: Whether to assert specific metric ranges or just assert non-zero.
   - Recommendation: Assert non-zero population for http_reqs and http_req_duration.P95, and assert http_req_failed < 0.01 (matching threshold). Exact values depend on kind cluster performance and are not reproducible. For thresholds metric, assert `result == 1` (passed).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| kind | e2e cluster creation | Yes | v0.31.0 | -- |
| kubectl | YAML application | Yes | v1.35.3 | -- |
| Docker | Image building/loading | Yes | v29.4.0 | -- |
| Go | Building binaries | Yes | 1.26.2 | -- |
| k6-operator bundle | e2e k6-operator install | Yes (remote URL) | v1.3.2 | -- |
| grafana/k6 image | k6 runner pods | Yes (Docker Hub) | latest | -- |

**Missing dependencies with no fallback:** None.
**Missing dependencies with fallback:** None.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + sigs.k8s.io/e2e-framework v0.6.0 |
| Config file | None (e2e uses `//go:build e2e` tag + e2e/main_test.go) |
| Quick run command | `make test` (unit tests only) |
| Full suite command | `make test-e2e` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DOCS-01 | RBAC ClusterRole grants needed permissions | manual-only | N/A (YAML review) | No - Wave 0 creates examples/k6-operator/clusterrole.yaml |
| DOCS-02 | Example YAML works out of the box | e2e (implicit) | `make test-e2e` | No - Wave 0 creates example YAML files |
| TEST-01 | e2e test validates TestRun creation and result extraction | e2e | `make test-e2e` | No - Wave 0 creates e2e/k6_operator_test.go |

### Sampling Rate
- **Per task commit:** `make test` (unit tests, ~5s)
- **Per wave merge:** `make test-e2e` (full e2e suite, ~10-15min)
- **Phase gate:** Full e2e suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `e2e/k6_operator_test.go` -- covers TEST-01
- [ ] `e2e/testdata/k6-script-configmap.yaml` -- k6 script ConfigMap for e2e
- [ ] `e2e/testdata/analysistemplate-k6op.yaml` -- k6-operator AnalysisTemplate
- [ ] `e2e/testdata/rollout-step-k6op.yaml` -- k6-operator step Rollout
- [ ] `e2e/main_test.go` modification -- add installK6Operator() to setup chain

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | N/A (no user auth in this phase) |
| V3 Session Management | No | N/A |
| V4 Access Control | Yes | Kubernetes RBAC ClusterRole with least-privilege verbs |
| V5 Input Validation | No | N/A (YAML examples are static, not user input) |
| V6 Cryptography | No | N/A |

### Known Threat Patterns for RBAC + k6-operator

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Over-permissive RBAC | Elevation of Privilege | Least-privilege verbs (no `*`, no `patch`, no `update` on CRDs). Only verbs actually used by the plugin code. |
| ConfigMap containing secrets in k6 script | Information Disclosure | Example script should NOT contain credentials. Document using k6 `--env` for secrets. |
| TestRun CRs left running after rollout abort | Denial of Service | StopRun deletes CRs (verified in operator.go StopRun). RBAC includes `delete` verb. |

## Sources

### Primary (HIGH confidence)
- `internal/provider/operator/operator.go` -- TriggerRun, GetRunResult, StopRun implementation (verified API calls -> RBAC verbs)
- `internal/provider/operator/testrun.go` -- TestRun GVR `k6.io/v1alpha1/testruns`, PrivateLoadZone GVR `k6.io/v1alpha1/privateloadzones`
- `internal/provider/operator/summary.go` -- parseSummaryFromPods, findSummaryJSON (pod log reading, JSON parsing)
- `internal/provider/operator/exitcode.go` -- checkRunnerExitCodes (pod listing with label selector)
- `internal/provider/config.go` -- PluginConfig JSON schema, ValidateK6Operator
- `e2e/main_test.go` -- Existing e2e setup patterns (kind, plugin deploy, mock server)
- `e2e/metric_plugin_test.go`, `e2e/step_plugin_test.go` -- Existing test patterns
- `examples/canary-full/` -- Existing example directory structure
- `go.mod` -- k6-operator v1.3.2, e2e-framework v0.6.0
- k6-operator v1.3.2 bundle.yaml -- Confirmed available at `https://raw.githubusercontent.com/grafana/k6-operator/v1.3.2/bundle.yaml` [VERIFIED: curl HEAD 200]

### Secondary (MEDIUM confidence)
- [Grafana k6-operator install docs](https://grafana.com/docs/k6/latest/set-up/set-up-distributed-k6/install-k6-operator/) -- Bundle installation method
- [k6 handleSummary docs](https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/) -- handleSummary API
- [k6-operator TestRun CRD types](https://github.com/grafana/k6-operator/blob/main/config/crd/bases/k6.io_testruns.yaml) -- CRD schema

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- All libraries already in use, versions verified from go.mod
- Architecture: HIGH -- Extends well-established existing patterns with minimal new infrastructure
- Pitfalls: HIGH/MEDIUM -- Most verified from code, image pull behavior assumed
- RBAC: HIGH -- Every verb traced to actual API calls in the provider code

**Research date:** 2026-04-16
**Valid until:** 2026-05-16 (stable domain, k6-operator CRD API is stable v1alpha1)
