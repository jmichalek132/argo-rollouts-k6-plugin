# argo-rollouts-k6-plugin

An Argo Rollouts plugin that integrates Grafana Cloud k6 load testing as analysis gates in canary and blue-green deployments.

## How It Works

The plugin ships as two binaries: **metric-plugin** and **step-plugin**.

- **Metric plugin** -- Polls k6 Cloud test run metrics on each AnalysisRun interval and returns configurable metric values (error rate, latency percentiles, threshold pass/fail) for AnalysisTemplate `successCondition` evaluation.
- **Step plugin** -- Triggers a k6 Cloud test run, waits for completion, and returns pass/fail as a canary step gate.

Both plugins use the Grafana Cloud k6 REST API to interact with test runs. The controller loads each plugin binary from a URL specified in the `argo-rollouts-config` ConfigMap.

## Installation

### 1. Download the plugin binaries

Download the binaries for your platform from [GitHub Releases](https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases):

```bash
# Metric plugin
curl -LO https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/metric-plugin_linux_amd64

# Step plugin (optional -- only needed for step-based canary gates)
curl -LO https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/step-plugin_linux_amd64

# Verify checksums
curl -LO https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/checksums.txt
sha256sum -c checksums.txt
```

### 2. Register the plugins in the Argo Rollouts ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  metricProviderPlugins: |
    - name: "jmichalek132/k6"
      location: "https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/metric-plugin_linux_amd64"
      sha256: "<SHA256_FROM_CHECKSUMS_TXT>"
  stepPlugins: |
    - name: "jmichalek132/k6-step"
      location: "https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/step-plugin_linux_amd64"
      sha256: "<SHA256_FROM_CHECKSUMS_TXT>"
```

Replace `<SHA256_FROM_CHECKSUMS_TXT>` with the actual SHA256 checksums from the release's `checksums.txt` file.

### 3. Restart the Argo Rollouts controller

```bash
kubectl rollout restart deployment argo-rollouts -n argo-rollouts
```

## Credentials

Create a Kubernetes Secret with your Grafana Cloud k6 credentials:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: k6-cloud-credentials
type: Opaque
stringData:
  apiToken: "<YOUR_API_TOKEN>"
  stackId: "<YOUR_STACK_ID>"
```

- **apiToken**: Generate a personal API token at Grafana Cloud UI > Testing & Synthetics > Performance > Settings > Access > Personal token.
- **stackId**: Your Grafana Cloud stack ID, visible in the stack URL.

## Quick Start

The simplest pattern uses the metric plugin to check k6 threshold pass/fail:

```yaml
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

Create an AnalysisRun that references this template:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: k6-check
spec:
  metrics:
    - name: k6-thresholds
      provider:
        plugin:
          jmichalek132/k6:
            testId: "12345"
            apiToken: "<YOUR_API_TOKEN>"
            stackId: "<YOUR_STACK_ID>"
            metric: thresholds
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
      value: "12345"
```

See `examples/` for more patterns.

## Examples

| Pattern | Directory | Description |
|---------|-----------|-------------|
| Threshold gate | [`examples/threshold-gate/`](examples/threshold-gate/) | Simplest: pass/fail based on k6 thresholds |
| Error rate + latency | [`examples/error-rate-latency/`](examples/error-rate-latency/) | Combined HTTP error rate and p95 latency gate |
| Full canary | [`examples/canary-full/`](examples/canary-full/) | Step plugin trigger + metric analysis gate |
| k6-operator (in-cluster) | [`examples/k6-operator/`](examples/k6-operator/) | In-cluster k6 execution via k6-operator with RBAC and ConfigMap script |

Each example directory contains:
- `analysistemplate.yaml` (or `rollout.yaml`) -- the main resource
- `secret.yaml` -- credential Secret with placeholder values
- `configmap-snippet.yaml` -- ConfigMap snippet to register the plugin(s)

## Available Metrics

| Metric | Config value | Description | Result |
|--------|-------------|-------------|--------|
| Thresholds | `metric: thresholds` | k6 native threshold pass/fail | `1` (passed) or `0` (failed) |
| HTTP error rate | `metric: http_req_failed` | Fraction of failed HTTP requests | `0.0` - `1.0` |
| HTTP latency | `metric: http_req_duration` | Request duration percentile (requires `aggregation`) | milliseconds |
| HTTP throughput | `metric: http_reqs` | Request rate | requests/second |

For `http_req_duration`, you must specify an `aggregation` value: `p50`, `p95`, or `p99`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, project structure, and how to implement new providers.

## License

Apache 2.0 -- see [LICENSE](LICENSE) for details.
