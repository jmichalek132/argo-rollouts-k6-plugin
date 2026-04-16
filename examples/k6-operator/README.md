# k6-operator Examples

These examples show how to use the argo-rollouts-k6-plugin with the
[grafana/k6-operator](https://github.com/grafana/k6-operator) for **in-cluster**
k6 execution. No Grafana Cloud account is required -- k6 runs as Kubernetes
Jobs managed by the operator, and the plugin reads results from runner pod
logs via the `handleSummary` export.

## Prerequisites

- Kubernetes cluster with [Argo Rollouts](https://argo-rollouts.readthedocs.io)
  installed (v1.9.0+)
- k6-operator installed:
  ```bash
  kubectl apply --server-side -f https://raw.githubusercontent.com/grafana/k6-operator/v1.3.2/bundle.yaml
  ```
- Plugin binaries registered in the `argo-rollouts-config` ConfigMap
  (see the project [main README](../../README.md) for installation details).

## Setup

1. **Apply the RBAC ClusterRole and ClusterRoleBinding** so the Argo Rollouts
   controller can manage k6-operator CRDs, read pod logs, and read ConfigMaps:
   ```bash
   kubectl apply -f clusterrole.yaml
   kubectl apply -f clusterrolebinding.yaml
   ```
   If your Argo Rollouts controller uses a different ServiceAccount or
   namespace than the default `argo-rollouts/argo-rollouts`, edit the
   `subjects` block in `clusterrolebinding.yaml` before applying.

2. **Create the k6 script ConfigMap** in the namespace where your Rollout
   lives:
   ```bash
   kubectl apply -f configmap-script.yaml -n <your-namespace>
   ```
   Before applying, edit `TARGET_URL` in the script to point at your
   application's cluster-internal service address (for example
   `http://my-app.default.svc.cluster.local`).

## Step Plugin (Canary Step)

Use `rollout-step.yaml` when you want a one-shot k6 test as a canary gate.
The step plugin creates a k6-operator TestRun, waits for the runner pods to
finish, then returns pass/fail to the controller. If the test fails, the
rollout rolls back automatically.

```bash
kubectl apply -f rollout-step.yaml -n <your-namespace>
```

The step plugin does **not** poll metrics -- it trusts k6 thresholds and pod
exit codes for its pass/fail decision. For richer metric-based gates, see the
metric plugin below.

## Metric Plugin (Analysis Gate)

Use `analysistemplate.yaml` + `rollout-metric.yaml` when you want
threshold-based analysis that the controller polls on an interval.

```bash
kubectl apply -f analysistemplate.yaml -n <your-namespace>
kubectl apply -f rollout-metric.yaml -n <your-namespace>
```

The metric plugin triggers a k6-operator TestRun and polls the runner until
it terminates, then evaluates the metric you configured (`thresholds`,
`http_req_failed`, `http_req_duration`, or `http_reqs`) against
`successCondition`.

**Using extracted metrics instead of thresholds:** The AnalysisTemplate
defaults to `metric: thresholds` (pass/fail). To instead gate on a specific
metric extracted from `handleSummary`, replace the metric block with one of
the examples in the comment header of `analysistemplate.yaml`. Available
options:

| Metric | Type | Example `successCondition` |
|--------|------|-----------------------------|
| `http_req_failed` | float 0.0-1.0 (fraction failed) | `result < 0.01` |
| `http_req_duration` (with `aggregation: p95`) | milliseconds | `result < 500` |
| `http_reqs` | requests/second | `result > 100` |

These detailed metrics require the k6 script to export `handleSummary` --
see the next section and `configmap-script.yaml`.

## Initial Deploy Behavior

Argo Rollouts **skips canary steps on the initial deployment** of a Rollout.
After your first `kubectl apply -f rollout-step.yaml` (or `rollout-metric.yaml`)
you will **not** see any k6 activity -- no TestRun CRs, no runner pods. This
is standard Argo Rollouts behavior, not specific to the k6 plugin.

To trigger the first canary with k6 steps executing, push a second revision.
For example:

- Change the container image tag (e.g. `nginx:1.27` to `nginx:1.27.1`) and
  re-apply the Rollout manifest.
- Or patch the rollout in place:
  ```bash
  kubectl argo rollouts set image my-app my-app=nginx:1.27.1
  ```

After the second revision starts, the canary steps run normally and the
k6 step / analysis gate executes.

## Files

| File | Description |
|------|-------------|
| `clusterrole.yaml` | RBAC ClusterRole with least-privilege permissions for k6.io CRDs, pods, pods/log, and configmaps |
| `clusterrolebinding.yaml` | Binds the ClusterRole to the `argo-rollouts` ServiceAccount |
| `analysistemplate.yaml` | Metric plugin AnalysisTemplate (threshold check with extracted-metric examples in comments) |
| `rollout-step.yaml` | Step plugin Rollout (trigger k6 + wait for pass/fail) |
| `rollout-metric.yaml` | Metric plugin Rollout (analysis step polling k6 results) |
| `configmap-script.yaml` | k6 test script ConfigMap with `handleSummary` export |

## Notes

- ConfigMap scripts must be under **1 MiB** (Kubernetes ConfigMap size limit).
  Keep k6 scripts minimal; split large scenarios into multiple files if needed.
- The `handleSummary` function in the k6 script is **required** for detailed
  metric extraction (`http_req_failed`, `http_req_duration`, `http_reqs`).
  Without it, only threshold pass/fail is available via runner pod exit codes.
- RBAC includes PrivateLoadZone CRD permissions for forward compatibility
  with Grafana Cloud-connected in-cluster execution. Current in-cluster-only
  code paths do not create PrivateLoadZone CRs.
- The k6 runner image defaults to `grafana/k6:latest`. For production, pin a
  specific version by setting the `runnerImage` config field in your plugin
  config (for example `runnerImage: grafana/k6:1.0.0`).
