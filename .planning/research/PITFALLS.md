# Pitfalls Research: v0.3.0 In-Cluster Execution

**Domain:** Adding Kubernetes Job execution, k6-operator CRD support, ConfigMap script sourcing, and local binary execution to an Argo Rollouts k6 plugin
**Researched:** 2026-04-15
**Confidence:** HIGH for Kubernetes Job/RBAC pitfalls, MEDIUM for k6-operator CRD integration (status reporting gap is documented but workarounds are community-sourced), HIGH for ConfigMap limits, MEDIUM for subprocess execution

## Critical Pitfalls

### Pitfall 1: Plugin Has No Kubernetes API Access Without Adding client-go

**What goes wrong:**
The plugin currently runs as a child process of the Argo Rollouts controller via hashicorp/go-plugin. It communicates exclusively through net/rpc. The plugin binary has zero Kubernetes API access today -- no client-go dependency, no in-cluster config, no service account token reading. Adding the Kubernetes Job provider or k6-operator CRD provider requires importing `k8s.io/client-go` and calling `rest.InClusterConfig()` to build a Kubernetes client. This is a fundamental architectural change: the plugin goes from "pure HTTP API caller" to "Kubernetes API participant."

**Why it happens:**
The v1.0 design intentionally avoided client-go because Grafana Cloud k6 only needs HTTP REST calls. Developers may assume the plugin "already has K8s access" because it runs inside the controller pod -- but it does not. The plugin inherits the controller's service account token (mounted at `/var/run/secrets/kubernetes.io/serviceaccount/token`), but it must explicitly use client-go to read and use that token.

**How to avoid:**
- Add `k8s.io/client-go` as a direct dependency. Pin to the same minor version used by `github.com/argoproj/argo-rollouts v1.9.0` (which uses k8s.io/client-go v0.34.1 -- already an indirect dependency in go.mod).
- Use `rest.InClusterConfig()` to create the REST config. This reads the mounted service account token and CA cert automatically.
- Create the Kubernetes clientset once in `InitPlugin()` (not per-call) since the in-cluster config is static for the process lifetime. This differs from the stateless per-call pattern used for GrafanaCloudProvider, and requires a clear design decision.
- Test that binary size increase is acceptable. Importing client-go typically adds 15-25MB to a Go binary due to transitive dependencies (protobuf, OpenAPI schemas, etc.).

**Warning signs:**
- `rest.InClusterConfig()` returns error "unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined" -- plugin is not running inside a pod, or env vars are not propagated to child process.
- Binary size doubles or triples after adding client-go.
- go.mod version conflicts between the argo-rollouts module's k8s dependencies and the client-go version you import.

**Phase to address:**
Phase 1 (Kubernetes Job provider) -- this is the first feature that needs K8s API access. Must be resolved before any in-cluster feature work begins.

---

### Pitfall 2: RBAC Permissions Not Granted for Job/CRD Operations

**What goes wrong:**
The plugin inherits the Argo Rollouts controller's service account RBAC permissions. The default `argo-rollouts` ClusterRole does NOT include permissions for `batch/v1 Jobs` (create, get, watch, delete) or `k6.io TestRun` CRDs (create, get, watch, delete). Every API call to create a Job or TestRun will return 403 Forbidden. Users will see cryptic "forbidden" errors in AnalysisRun status, and the rollout will fail.

**Why it happens:**
Plugin authors assume RBAC "just works" because the plugin runs inside the controller. But the controller's service account is scoped to Argo Rollouts resources (Rollouts, AnalysisRuns, ReplicaSets, Services, etc.). It has no reason to have batch/v1 or k6.io permissions by default. Users must manually extend the RBAC rules, and this is easy to forget or misconfigure.

**How to avoid:**
- Ship example RBAC patches (ClusterRole additions) in the documentation and in a `deploy/rbac/` directory.
- For the Job provider: grant `create`, `get`, `list`, `watch`, `delete` on `batch/v1` `jobs` in the target namespace(s).
- For the k6-operator provider: grant `create`, `get`, `list`, `watch`, `delete` on `k6.io` `testruns` in the target namespace(s).
- Validate RBAC at startup in `InitPlugin()` by performing a dry-run SelfSubjectAccessReview or simply attempting a list operation. Log a clear error if permissions are missing.
- Document that users must choose between ClusterRole (all namespaces) or Role (per-namespace) bindings.

**Warning signs:**
- AnalysisRun or Rollout fails immediately with "forbidden" or "Unauthorized" in the message field.
- Controller logs from the plugin show "the server does not allow access to the requested resource."
- Works in dev (where you granted cluster-admin) but fails in staging/production.

**Phase to address:**
Phase 1 (Kubernetes Job provider) -- RBAC documentation and validation must ship with the first in-cluster provider.

---

### Pitfall 3: k6-operator TestRun CRD Cannot Distinguish Pass vs. Fail

**What goes wrong:**
The k6-operator's TestRun CRD status has a `stage` field that transitions through: `initialization` -> `initialized` -> `created` -> `started` -> `finished` -> (or `error`). When a test finishes, `stage` becomes `"finished"` regardless of whether k6 thresholds passed (exit 0) or failed (exit 99). There is NO status field or condition that indicates whether the test passed or failed. This is a documented upstream limitation (grafana/k6-operator#577, grafana/k6-operator#75).

The plugin cannot simply watch for `stage == "finished"` and report success. It must independently determine pass/fail by inspecting the underlying runner Job's pod exit codes or container statuses.

**Why it happens:**
The k6-operator was designed primarily for manual and CI/CD workflows where users inspect logs for results. The operator tracks execution lifecycle (did the test run?) not test outcome (did the test pass?). The enhancement to add pass/fail conditions has been requested since 2023 but remains unresolved.

**How to avoid:**
- Do NOT rely on TestRun `.status.stage == "finished"` as a success signal. `"finished"` only means "execution completed."
- After the TestRun reaches `"finished"`, query the runner Job's pods to inspect container exit codes:
  - Exit code 0 = thresholds passed
  - Exit code 99 = one or more thresholds failed
  - Exit code 107 = script exception
  - Exit code 108 = script aborted by test.abort()
  - Any other non-zero = execution error
- Name the runner Jobs predictably using the TestRun name so you can find them: k6-operator creates Jobs named `{testrun-name}-{N}` (where N is the runner index).
- Consider watching the Job directly (via informer) in addition to the TestRun CRD for faster pass/fail detection.
- Document this limitation clearly so users understand why the plugin needs extra RBAC for pod/get.

**Warning signs:**
- Plugin reports "Passed" for every k6 run that completes, even when thresholds failed.
- Users see successful rollout promotions despite k6 threshold failures.
- Test works correctly with the Grafana Cloud provider (which has explicit pass/fail in API response) but not with k6-operator.

**Phase to address:**
k6-operator CRD support phase -- this is THE hardest problem in that feature. Must be solved before claiming k6-operator support works.

---

### Pitfall 4: Kubernetes Job Provider Leaves Orphaned Jobs on Rollout Abort

**What goes wrong:**
When a rollout is aborted or the AnalysisRun is terminated, the plugin's `Terminate()` / `Abort()` method is called. For the Grafana Cloud provider, this calls a simple HTTP API to stop the run. For a Kubernetes Job, the plugin must actively delete the Job (and its pods) from the cluster. If `Terminate()` fails silently (the current pattern logs warnings but doesn't return errors), orphaned k6 Jobs continue running, consuming cluster resources and potentially generating load against production services.

**Why it happens:**
The current StopRun pattern is fire-and-forget with error logging. This is fine for an HTTP API call that takes milliseconds. But deleting a Kubernetes Job has cascading effects (pod termination, grace periods) and can fail for RBAC reasons, API server unavailability, or namespace termination races. The "log and swallow" pattern masks these failures.

**How to avoid:**
- Implement Job cleanup with propagation policy `Background` (delete Job, let controller clean up pods) as the default, with `Foreground` as a configurable option for users who need synchronous cleanup.
- Set `ttlSecondsAfterFinished` on created Jobs (e.g., 300 seconds) as a safety net -- even if explicit cleanup fails, Kubernetes will eventually garbage collect.
- Add finalizer-like tracking: store the Job name and namespace in the step state (persisted in `RpcStepContext.Status`) so cleanup can be retried on subsequent calls.
- In StopRun, attempt deletion and return a meaningful error if it fails, rather than swallowing it.
- For the metric plugin, GarbageCollect is already a no-op -- for Job-backed providers, it should delete completed Jobs.

**Warning signs:**
- `kubectl get jobs` shows many completed/failed k6 jobs accumulating over time.
- Cluster resource usage increases after many rollback events.
- Pod count in the namespace grows unexpectedly.

**Phase to address:**
Phase 1 (Kubernetes Job provider) -- Job lifecycle management must be part of the initial implementation.

---

### Pitfall 5: ConfigMap Script Sourcing Hits 1MB Size Limit

**What goes wrong:**
Kubernetes ConfigMaps have a hard limit of 1,048,576 bytes (1 MiB) for the entire object including metadata. Large k6 test scripts -- especially those importing helper modules, test data, or bundled dependencies -- easily exceed this. The Kubernetes API returns a 422 (Request Entity Too Large) when creating or updating an oversized ConfigMap. Users who currently store scripts in Grafana Cloud (no size limit) will hit this silently when switching to ConfigMap sourcing.

**Why it happens:**
k6 scripts often include:
- Inline test data (JSON payloads, CSV data)
- Shared utility modules (bundled via webpack/esbuild)
- Protocol buffer definitions (for gRPC testing)
- Multi-scenario scripts with many exported functions

A single k6 script with embedded test data can easily reach 500KB-2MB.

**How to avoid:**
- Validate ConfigMap size at plugin config parse time. If the referenced ConfigMap's data exceeds a warning threshold (e.g., 800KB), log a warning.
- Document the 1MB limit prominently in all ConfigMap-related documentation and examples.
- Support ConfigMap references that point to a specific key within the ConfigMap (e.g., `configMap: {name: "my-scripts", key: "test.js"}`) rather than consuming the entire ConfigMap.
- Recommend that users externalize test data from scripts (load from files or environment variables at runtime).
- For the k6-operator provider, leverage its native `spec.script.configMap` support which handles the volume mounting automatically.
- For the Job provider, mount the ConfigMap as a volume at a known path (e.g., `/scripts/test.js`) and pass the path to the k6 binary.

**Warning signs:**
- Users report "config parsing failed" or "ConfigMap not found" when the real issue is size.
- kubectl apply of the ConfigMap silently fails in CI pipelines.
- Scripts work via Grafana Cloud but not via ConfigMap sourcing.

**Phase to address:**
ConfigMap script sourcing phase -- must be the first capability addressed, as both Job and k6-operator providers depend on it.

---

### Pitfall 6: Subprocess k6 Execution Writes to stdout, Corrupting go-plugin Protocol

**What goes wrong:**
If the plugin spawns k6 as a subprocess via `exec.Command("k6", "run", ...)`, k6 writes its progress bar, summary output, and log messages to stdout by default. Since the plugin process's stdout is owned by the go-plugin handshake protocol (already completed at that point, but stdout is redirected to a pipe read by the controller), k6's stdout output gets piped to the Argo Rollouts controller. At best this causes garbled log output; at worst it interferes with the go-plugin protocol.

**Why it happens:**
After `plugin.Serve()` completes the handshake, go-plugin replaces `os.Stdout` with a pipe. Anything written to the process's stdout FD is captured by the controller's plugin client. Spawning k6 as a child process inherits the parent's file descriptors unless explicitly redirected. Developers test k6 subprocess execution outside the plugin context (standalone binary) where stdout works normally, then it breaks inside the plugin.

**How to avoid:**
- When spawning k6 via `exec.Command`, explicitly redirect both stdout and stderr to buffers or to `os.Stderr`:
  ```go
  cmd := exec.Command("k6", "run", "--quiet", scriptPath)
  cmd.Stdout = os.Stderr  // or a bytes.Buffer
  cmd.Stderr = os.Stderr
  ```
- Use `--quiet` flag to suppress k6's progress bar output.
- Use `--log-output=stderr` to force k6's structured logging to stderr.
- Never let k6's stdout flow through to the parent process's stdout.
- Test subprocess execution IN the go-plugin context (via `goPlugin.ServeTestConfig`) not just standalone.

**Warning signs:**
- Controller logs show random k6 output mixed with plugin RPC messages.
- Plugin works in unit tests (no go-plugin wrapper) but produces garbled results when loaded by controller.
- Controller reports "unexpected data on plugin connection."

**Phase to address:**
Local binary execution phase -- this is specific to the subprocess provider and must be tested in the go-plugin context.

---

### Pitfall 7: Provider Interface Needs Expansion for Script Sourcing

**What goes wrong:**
The current `Provider` interface accepts `*PluginConfig` which contains `TestID` (Grafana Cloud test ID) and `APIToken`/`StackID` for authentication. None of these fields are relevant for in-cluster execution. The Job provider needs: script source (ConfigMap name/key, or inline script), namespace, k6 image, resource limits, service account. The k6-operator provider needs: similar fields plus parallelism, arguments. Trying to cram all these into `PluginConfig` creates an unwieldy struct where most fields are irrelevant to most providers.

**Why it happens:**
`PluginConfig` was designed for a single provider (Grafana Cloud). Adding fields for 3 more providers (Job, k6-operator, local binary) turns it into a god struct with >20 fields where only 3-5 are used per provider. Validation becomes complex ("if provider=cloud then testId is required, if provider=job then scriptConfigMap is required...").

**How to avoid:**
- Use a two-level config structure: common fields (timeout, provider type) at the top level, provider-specific config as an embedded `json.RawMessage` that each provider unmarshals into its own typed struct.
- Example:
  ```json
  {
    "provider": "kubernetes-job",
    "timeout": "10m",
    "config": {
      "namespace": "load-tests",
      "scriptConfigMap": "k6-scripts",
      "scriptKey": "test.js",
      "image": "grafana/k6:latest",
      "resources": {"limits": {"cpu": "1", "memory": "512Mi"}}
    }
  }
  ```
- Each provider gets its own config struct with its own validation.
- The Provider interface's `TriggerRun` signature may need to accept a generic config interface rather than `*PluginConfig` directly, or use an options pattern.
- Consider backward compatibility: existing users have AnalysisTemplates with `testId`, `apiToken`, `stackId` fields. These must continue to work with the cloud provider.

**Warning signs:**
- `PluginConfig` struct has >15 fields with complex `omitempty` tags.
- Config validation function has deeply nested if/else chains.
- Adding a new provider requires modifying the config struct and all existing validation logic.
- Users confused about which config fields apply to which provider.

**Phase to address:**
Phase 1 (before Kubernetes Job provider) -- the config refactoring should happen as a prerequisite to adding any new provider.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Single PluginConfig struct for all providers | No refactoring, ship fast | God struct, validation spaghetti, confusing user docs | Never -- refactor before adding second provider |
| Creating K8s client per provider call (stateless pattern) | Consistency with existing Grafana Cloud pattern | REST config parsing + TLS setup on every call (~5ms), unnecessary token re-reads | Only in prototype. InitPlugin should create client once for K8s providers |
| No ttlSecondsAfterFinished on created Jobs | Fewer fields to configure | Orphaned Jobs accumulate, etcd bloat, pod resource waste | Never -- always set TTL as safety net |
| Hardcoding k6 container image tag | Simpler config | Users stuck on stale k6 versions, can't use custom k6 with extensions | Only in v0.3.0 alpha. Must be configurable by GA |
| Using dynamic client instead of typed client for k6-operator CRDs | Avoids importing k6-operator types, lighter dependency | No compile-time type safety, runtime panics on field name typos, no IDE autocompletion | Acceptable if avoiding k6-operator Go module dependency is a priority |
| Skipping RBAC validation at startup | Less code, no extra API calls | Users get cryptic 403 errors during rollout, hours of debugging | Never -- validate in InitPlugin |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Kubernetes Jobs | Creating Job in plugin's PID namespace instead of target namespace | Always create Jobs in the namespace specified by user config (default: same namespace as the Rollout) |
| Kubernetes Jobs | Not setting `backoffLimit: 0` | k6 threshold failures (exit 99) cause Kubernetes to restart the Job pod. Set `backoffLimit: 0` to run exactly once |
| Kubernetes Jobs | Missing `restartPolicy: Never` on pod spec | Required for `backoffLimit: 0` to work. Default `restartPolicy: Always` causes infinite restart loops on threshold failure |
| k6-operator TestRun | Assuming `stage: finished` means passed | `finished` only means execution completed. Must inspect runner pod exit codes for pass/fail |
| k6-operator TestRun | Creating TestRun with same name as previous run | TestRun names must be unique. Use a generated name (e.g., `{rollout}-{analysisrun}-{timestamp}`) |
| k6-operator TestRun | Not waiting for CRD to be installed | If k6-operator is not installed, creating a TestRun returns 404 on the CRD. Validate CRD existence in InitPlugin |
| ConfigMap sourcing | Reading ConfigMap data as `binaryData` | k6 scripts are UTF-8 text -- use the `data` field, not `binaryData`. binaryData base64-encodes content |
| ConfigMap sourcing | Not specifying the key within the ConfigMap | A ConfigMap can have multiple keys. Always require both `name` and `key` in config |
| Local k6 binary | Assuming k6 binary exists at a fixed path | k6 may not be installed in the controller container. The binary path must be configurable, with existence check at startup |
| Local k6 binary | Not handling k6 exit code 99 vs 0 | Exit 0 = pass, exit 99 = thresholds failed (not an error!), other non-zero = actual error. Must map exit 99 to provider.Failed, not provider.Errored |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Creating K8s client per RPC call | Slow metric measurements (5-10ms overhead per call), high memory churn from TLS connection setup | Create client once in InitPlugin, reuse across calls. K8s in-cluster config is static | Immediately noticeable with sub-second polling intervals |
| Watching all Jobs in all namespaces | High API server load, excessive memory in plugin process from informer cache | Use field selectors to watch only Jobs with a label selector (e.g., `app.kubernetes.io/managed-by=argo-rollouts-k6-plugin`) | At 50+ concurrent rollouts with Job-backed analysis |
| Polling TestRun status via GET requests | Linear API server load growth with concurrent rollouts | Use an informer/watch for TestRun CRDs instead of polling. Falls back to list+watch with resync | At 20+ concurrent k6-operator test runs |
| Spawning k6 subprocess per measurement | Fork+exec overhead, PID exhaustion if zombie processes not reaped | Use exec.CommandContext with explicit Wait() calls. Consider a long-running k6 process with REST API for repeated runs | At 10+ concurrent measurements spawning subprocesses |
| Large ConfigMap reads on every Resume() call | Repeated 1MB reads from API server on every poll cycle | Cache the script content after first read (it doesn't change during a measurement lifecycle) | With >10 concurrent analyses reading large ConfigMaps |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Running k6 Job pods with default service account | k6 pods inherit namespace default SA which may have excessive permissions | Specify a dedicated, minimally-privileged service account in Job pod spec. Document recommended SA setup |
| Embedding k6 Cloud API token in ConfigMap or Job env vars | Token visible in `kubectl describe job`, audit logs, and etcd | Use Kubernetes Secrets referenced via `secretKeyRef` in Job env vars. For k6-operator, use `spec.token` field with Secret reference |
| Not setting SecurityContext on k6 Job pods | k6 runs as root by default in many images, can access host filesystem if misconfigured | Set `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, drop all capabilities in security context |
| Plugin creating Jobs in arbitrary namespaces | Users could configure the plugin to create workloads in system namespaces (kube-system, argo-rollouts) | Validate target namespace against an allowlist or deny system namespaces by default |
| k6 scripts in ConfigMaps containing secrets | Test scripts may embed API keys, auth tokens for the system under test | Document that scripts should use environment variables (injected via Secret) for credentials, not hardcoded values |
| Not cleaning up TestRun CRDs after completion | TestRun objects accumulate, exposing test parameters and potentially tokens in cluster state | Set `cleanup: "post"` on TestRun spec, or implement explicit deletion after result extraction |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Different config schemas per provider without clear documentation | Users copy-paste Grafana Cloud AnalysisTemplate, replace provider name, get cryptic validation errors | Provide separate example AnalysisTemplates per provider with inline comments explaining each field |
| Not surfacing which provider is active in measurement metadata | Users can't tell from AnalysisRun status whether test ran on Cloud, Job, or k6-operator | Include `provider` field in measurement metadata (already have `testRunURL` -- extend to `providerType`) |
| Requiring k6-operator to be pre-installed without checking | Users configure k6-operator provider, rollout fails with opaque "resource not found" error | Check for k6.io CRD existence in InitPlugin, log clear message: "k6-operator CRD not found -- install k6-operator first" |
| No way to view k6 output/logs from AnalysisRun | Users must manually find the Job pod and `kubectl logs` to see k6 output | Store a log reference (Job name, pod name) in measurement metadata so users know where to look |
| ConfigMap name typo causes silent failure | User references non-existent ConfigMap, error appears deep in AnalysisRun conditions | Validate ConfigMap existence when parsing config (before triggering the run), return clear error |

## "Looks Done But Isn't" Checklist

- [ ] **Kubernetes Job provider:** Often missing `backoffLimit: 0` -- verify k6 threshold failure (exit 99) does NOT cause pod restart
- [ ] **Kubernetes Job provider:** Often missing `ttlSecondsAfterFinished` -- verify completed Jobs are cleaned up automatically
- [ ] **Kubernetes Job provider:** Often missing propagation policy on delete -- verify `Terminate()` actually stops running pods, not just marks Job for deletion
- [ ] **k6-operator provider:** Often missing exit code inspection -- verify threshold-failed runs (exit 99) are reported as `provider.Failed`, not `provider.Passed`
- [ ] **k6-operator provider:** Often missing CRD existence check -- verify InitPlugin logs clear error when k6-operator is not installed
- [ ] **k6-operator provider:** Often missing unique name generation -- verify two concurrent AnalysisRuns don't create TestRuns with the same name
- [ ] **ConfigMap sourcing:** Often missing key validation -- verify error message when ConfigMap key doesn't exist (vs ConfigMap itself not found)
- [ ] **ConfigMap sourcing:** Often missing size validation -- verify warning is logged for ConfigMaps approaching 1MB
- [ ] **Local binary provider:** Often missing `cmd.Wait()` -- verify no zombie k6 processes after test completion
- [ ] **Local binary provider:** Often missing stdout redirect -- verify k6 output doesn't corrupt go-plugin protocol
- [ ] **All providers:** Often missing namespace config -- verify user can specify target namespace for Job/TestRun creation
- [ ] **RBAC:** Often missing documentation -- verify README includes exact ClusterRole additions needed per provider

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Orphaned k6 Jobs after rollout abort | LOW | `kubectl delete jobs -l app.kubernetes.io/managed-by=argo-rollouts-k6-plugin` -- label-based bulk cleanup |
| Orphaned TestRun CRDs | LOW | `kubectl delete testruns -l app.kubernetes.io/managed-by=argo-rollouts-k6-plugin` -- label-based bulk cleanup |
| RBAC permission denied at rollout time | MEDIUM | Apply corrected ClusterRole, re-trigger AnalysisRun. No data loss, but rollout must be restarted |
| k6-operator reports pass for failed test | HIGH | Rollout already promoted with bad code. Manual rollback required. Root cause: missing exit code inspection. Fix plugin, redeploy, re-run affected rollouts |
| ConfigMap too large for script | LOW | Split script into smaller modules, externalize test data, use PVC for large scripts (k6-operator only) |
| Zombie k6 processes from local binary provider | MEDIUM | Restart controller pod (kills all child processes). Fix: add explicit cmd.Wait() and process group kill |
| Binary size bloat from client-go | MEDIUM | Cannot easily undo. Mitigate with `go build -ldflags="-s -w"` for stripping debug info. Accept ~40MB binary as trade-off for K8s API access |
| go.mod dependency conflict with argo-rollouts | HIGH | Carefully align all k8s.io/* dependencies to the same version used by argo-rollouts v1.9.0. May require replace directives. Test full build before merging |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| No K8s API access (Pitfall 1) | ConfigMap sourcing or Job provider (whichever is first) | `rest.InClusterConfig()` succeeds in e2e test; ConfigMap/Job creation works |
| RBAC not granted (Pitfall 2) | Job provider phase -- ship with RBAC docs | e2e test with non-cluster-admin SA succeeds; RBAC example YAML included in release |
| k6-operator pass/fail blindness (Pitfall 3) | k6-operator CRD support phase | e2e test: k6 script with failing thresholds triggers provider.Failed, not provider.Passed |
| Orphaned Jobs on abort (Pitfall 4) | Job provider phase -- implement cleanup from day 1 | e2e test: abort rollout, verify no k6 Jobs remain after 5 minutes |
| ConfigMap 1MB limit (Pitfall 5) | ConfigMap sourcing phase | Unit test with >1MB script content returns clear error; docs mention limit |
| Subprocess stdout corruption (Pitfall 6) | Local binary execution phase | e2e test: k6 subprocess execution in go-plugin context produces clean RPC results |
| Provider config expansion (Pitfall 7) | Before Job provider phase (prerequisite) | New provider can be added without modifying existing PluginConfig struct; existing Cloud AnalysisTemplates still parse correctly |
| backoffLimit not set on Jobs | Job provider phase | e2e test: k6 script with `thresholds: {"http_req_failed": ["rate<0.01"]}` against bad endpoint runs exactly once |
| k6-operator CRD not installed check | k6-operator phase -- InitPlugin validation | e2e test: plugin without k6-operator installed logs clear error, doesn't crash |
| Zombie k6 processes | Local binary phase | e2e test: run 10 subprocess executions, verify PID count returns to baseline |

## Sources

- [Argo Rollouts Plugin Docs](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- HIGH confidence: confirms plugin inherits controller SA RBAC
- [k6-operator Issue #577: TestRun doesn't distinguish pass/fail](https://github.com/grafana/k6-operator/issues/577) -- HIGH confidence: confirms the status gap
- [k6-operator Issue #75: Handle k6 exit codes](https://github.com/grafana/k6-operator/issues/75) -- HIGH confidence: confirms exit code handling is unresolved
- [k6-operator Troubleshooting Guide](https://grafana.com/docs/k6/latest/set-up/set-up-distributed-k6/troubleshooting/) -- HIGH confidence: OOM, ServiceAccount, initializer failures
- [k6-operator TestRun CRD Types](https://pkg.go.dev/github.com/grafana/k6-operator/api/v1alpha1) -- HIGH confidence: K6Script struct with ConfigMap/VolumeClaim/LocalFile fields
- [k6 Exit Codes](https://pkg.go.dev/go.k6.io/k6/errext/exitcodes) -- HIGH confidence: exit 99 = ThresholdsHaveFailed, exit 107 = ScriptException, exit 108 = ScriptAborted
- [Kubernetes ConfigMap Size Limit](https://kubernetes.io/docs/concepts/configuration/configmap/) -- HIGH confidence: 1 MiB hard limit
- [Kubernetes Job TTL After Finished](https://kubernetes.io/docs/concepts/workloads/controllers/ttlafterfinished/) -- HIGH confidence: stable since K8s 1.23
- [Kubernetes RBAC Good Practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/) -- HIGH confidence: least privilege principle
- [go-plugin stdout Issue #164](https://github.com/hashicorp/go-plugin/issues/164) -- HIGH confidence: stdout pollution breaks handshake
- [k6 subprocess output format change (Issue #3744)](https://github.com/grafana/k6/issues/3744) -- MEDIUM confidence: k6 changes output format when detected as subprocess
- [Kubernetes In-Cluster Client Configuration](https://github.com/kubernetes/client-go/blob/master/examples/in-cluster-client-configuration/README.md) -- HIGH confidence: rest.InClusterConfig() reads mounted SA token
- [k6-operator TestRun Execution Flow](https://deepwiki.com/grafana/k6-operator/2.3-test-execution-flow) -- MEDIUM confidence: community documentation of lifecycle stages

---
*Pitfalls research for: v0.3.0 in-cluster k6 execution features*
*Researched: 2026-04-15*
