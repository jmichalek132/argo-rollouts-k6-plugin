# Feature Landscape

**Domain:** Argo Rollouts k6 Load Testing Plugin (Metric + Step)
**Researched:** 2026-04-09

## Table Stakes

Features users expect. Missing = plugin feels incomplete or unusable.

### TS-1: Metric Plugin -- k6 Cloud Test Metrics Polling

| Aspect | Detail |
|---------|--------|
| **Why Expected** | This is the core value proposition. Every Argo Rollouts metric plugin (Datadog, Prometheus, NewRelic, OpenSearch) returns metric values for `successCondition`/`failureCondition` evaluation. Without this, the plugin has no reason to exist. |
| **Complexity** | Medium |
| **Notes** | Must implement `RpcMetricProvider` interface: `InitPlugin`, `Run`, `Resume`, `Terminate`, `GarbageCollect`, `Type`, `GetMetadata`. The `Run` method starts a measurement, `Resume` polls for results. Return values go into `result` for expression evaluation. |

**AnalysisTemplate YAML:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-error-rate
spec:
  args:
    - name: test-run-id
    - name: k6-api-token
      valueFrom:
        secretKeyRef:
          name: k6-cloud-credentials
          key: api-token
    - name: k6-stack-id
      valueFrom:
        secretKeyRef:
          name: k6-cloud-credentials
          key: stack-id
  metrics:
    - name: http-error-rate
      interval: 30s
      successCondition: "result.httpReqFailedRate < 0.01"
      failureCondition: "result.httpReqFailedRate >= 0.05"
      failureLimit: 3
      provider:
        plugin:
          argo-rollouts-k6-plugin/metric:
            testRunId: "{{ args.test-run-id }}"
            apiToken: "{{ args.k6-api-token }}"
            stackId: "{{ args.k6-stack-id }}"
            metric: http_req_failed
            aggregation: rate
```

### TS-2: Step Plugin -- Trigger k6 Cloud Test and Wait

| Aspect | Detail |
|---------|--------|
| **Why Expected** | The "fire-and-forget" use case: trigger a k6 test at a canary step, block the rollout until it completes, return pass/fail. This is the simplest integration model and the first thing users will try. Without it, users must orchestrate test execution externally. |
| **Complexity** | Medium |
| **Notes** | Must implement `RpcStep` interface: `Run`, `Terminate`, `Abort`, `Type`. `Run` triggers test via `POST /loadtests/v2/tests/{id}/start-testrun`, returns `PhaseRunning` with `RequeueAfter` to poll. Subsequent `Run` calls check status. Returns `PhaseSuccessful` or `PhaseFailed` based on k6 threshold results. |

**Rollout YAML:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: argo-rollouts-k6-plugin/step
            config:
              testId: "12345"
              apiToken:
                secretRef:
                  name: k6-cloud-credentials
                  key: api-token
              stackId:
                secretRef:
                  name: k6-cloud-credentials
                  key: stack-id
              timeout: 10m
              failOnThresholdBreach: true
        - setWeight: 50
```

### TS-3: k6 Threshold Pass/Fail as a Metric

| Aspect | Detail |
|---------|--------|
| **Why Expected** | k6 thresholds are the native pass/fail mechanism in k6 scripts. Every k6 user defines thresholds (e.g., `p(95) < 500`, `rate < 0.01`). Exposing the aggregate threshold pass/fail as a single boolean metric is the most natural integration point. Users who already have well-defined k6 scripts should not have to re-define thresholds in AnalysisTemplate YAML. |
| **Complexity** | Low |
| **Notes** | Query `/loadtests/v2/thresholds` endpoint. Return `result.thresholdsPassed` boolean. Simple `successCondition: "result == true"`. This is likely the MOST used metric in practice. |

**AnalysisTemplate YAML:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-thresholds-gate
spec:
  args:
    - name: test-run-id
    - name: k6-api-token
      valueFrom:
        secretKeyRef:
          name: k6-cloud-credentials
          key: api-token
    - name: k6-stack-id
      valueFrom:
        secretKeyRef:
          name: k6-cloud-credentials
          key: stack-id
  metrics:
    - name: k6-thresholds
      interval: 60s
      successCondition: "result == true"
      failureLimit: 0
      provider:
        plugin:
          argo-rollouts-k6-plugin/metric:
            testRunId: "{{ args.test-run-id }}"
            apiToken: "{{ args.k6-api-token }}"
            stackId: "{{ args.k6-stack-id }}"
            metric: thresholds
```

### TS-4: Secrets Management via Kubernetes Secrets

| Aspect | Detail |
|---------|--------|
| **Why Expected** | Every Argo Rollouts plugin that connects to an external service handles credentials through `secretKeyRef` in AnalysisTemplate args (Datadog, NewRelic, Prometheus with auth). Hardcoding tokens in YAML is a non-starter for any production user. |
| **Complexity** | Low |
| **Notes** | Follow Datadog's pattern: support both inline `secretKeyRef` in template args AND a dedicated Secret in the argo-rollouts namespace for cluster-wide defaults. The Grafana Cloud k6 API requires `Authorization: Bearer <token>` and `X-Stack-Id: <id>` headers. |

**Secret YAML:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: k6-cloud-credentials
  namespace: argo-rollouts
type: Opaque
stringData:
  api-token: "your-k6-api-token-here"
  stack-id: "12345"
```

### TS-5: HTTP Latency Percentile Metrics (p95, p99)

| Aspect | Detail |
|---------|--------|
| **Why Expected** | Latency percentiles are the second most common deployment gate after error rate. Every monitoring-based plugin exposes this. k6 natively tracks `http_req_duration` as a Trend metric with percentile support. Users expect to gate deployments on "p95 latency < 500ms". |
| **Complexity** | Low-Medium |
| **Notes** | Query k6 Cloud metrics API (`/cloud/v5/test_runs/{id}/query_aggregate_k6`) with `histogram_quantile` for p50, p90, p95, p99. Return as numeric value for `successCondition` evaluation. |

**AnalysisTemplate YAML:**

```yaml
metrics:
  - name: p95-latency
    interval: 30s
    successCondition: "result < 500"
    failureCondition: "result >= 2000"
    failureLimit: 3
    provider:
      plugin:
        argo-rollouts-k6-plugin/metric:
          testRunId: "{{ args.test-run-id }}"
          apiToken: "{{ args.k6-api-token }}"
          stackId: "{{ args.k6-stack-id }}"
          metric: http_req_duration
          aggregation: p95
```

### TS-6: HTTP Error Rate Metric

| Aspect | Detail |
|---------|--------|
| **Why Expected** | Error rate is THE canonical deployment gate. If new code causes errors above threshold, roll back. k6's `http_req_failed` Rate metric tracks this natively. |
| **Complexity** | Low |
| **Notes** | Query `http_req_failed` metric from k6 Cloud API. Return as float (0.0 to 1.0). Direct use in `successCondition: "result < 0.01"`. |

### TS-7: HTTP Throughput Metric (requests/sec)

| Aspect | Detail |
|---------|--------|
| **Why Expected** | Throughput regression (fewer req/s than expected) can indicate performance degradation even when error rate and latency look fine. k6's `http_reqs` Counter metric tracks this. |
| **Complexity** | Low |
| **Notes** | Query `http_reqs` metric with rate aggregation from k6 Cloud API. |

### TS-8: Configurable Plugin Registration via ConfigMap

| Aspect | Detail |
|---------|--------|
| **Why Expected** | Standard Argo Rollouts plugin installation pattern. Every plugin must register in `argo-rollouts-config` ConfigMap with name, location (URL or file://), and optional SHA256 checksum. Without following this convention, the plugin won't load. |
| **Complexity** | Low (convention compliance, not custom code) |
| **Notes** | Must publish static binary to GitHub Releases with checksum file. |

**ConfigMap YAML:**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  metricProviderPlugins: |-
    - name: "argo-rollouts-k6-plugin/metric"
      location: "https://github.com/<org>/argo-rollouts-k6-plugin/releases/download/v0.1.0/metric-plugin-linux-amd64"
      sha256: "<checksum>"
  stepPlugins: |-
    - name: "argo-rollouts-k6-plugin/step"
      location: "https://github.com/<org>/argo-rollouts-k6-plugin/releases/download/v0.1.0/step-plugin-linux-amd64"
      sha256: "<checksum>"
```

### TS-9: Example AnalysisTemplates for Common Use Cases

| Aspect | Detail |
|---------|--------|
| **Why Expected** | Every successful Argo Rollouts plugin ships with ready-to-use examples. The Prometheus sample plugin, Datadog docs, and NewRelic docs all lead with YAML examples. Users copy-paste-modify, they don't read interface specs. |
| **Complexity** | Low |
| **Notes** | Ship examples for: (1) simple threshold gate, (2) error rate + latency combined, (3) step plugin fire-and-wait, (4) combined step + metric workflow. |

## Differentiators

Features that set this plugin apart. Not expected, but valued.

### D-1: Combined Step + Metric Workflow (Trigger then Monitor)

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | No existing Argo Rollouts plugin combines test execution AND metric monitoring in one coherent workflow. The step plugin triggers a k6 test, and the metric plugin can monitor that same test run's metrics as a separate analysis. This "trigger then gate" pattern is unique to load testing and doesn't exist for Datadog/Prometheus (which are passive observers). |
| **Complexity** | Medium |
| **Notes** | The step plugin returns the `testRunId` in its status. The metric plugin accepts `testRunId` as an arg. Users wire them together via AnalysisTemplate args. This is the killer workflow. |

**Combined Rollout YAML:**

```yaml
spec:
  strategy:
    canary:
      steps:
        - setWeight: 20
        # Step 1: trigger the load test
        - plugin:
            name: argo-rollouts-k6-plugin/step
            config:
              testId: "12345"
              apiToken:
                secretRef:
                  name: k6-cloud-credentials
                  key: api-token
              stackId:
                secretRef:
                  name: k6-cloud-credentials
                  key: stack-id
        # Step 2: gate on the test results
        - analysis:
            templates:
              - templateName: k6-thresholds-gate
            args:
              - name: test-run-id
                value: "{{steps.plugin.status.testRunId}}"
        - setWeight: 80
```

### D-2: Provider Abstraction Interface

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | Design the internal architecture so that Grafana Cloud k6 is one "provider" behind a Go interface. Future providers (in-cluster k6-operator Jobs, local k6 binary) plug in without changing the plugin's external API. This future-proofs the plugin and signals to the community that it's a serious, extensible project. |
| **Complexity** | Medium |
| **Notes** | Define `TestProvider` interface with methods like `TriggerTest`, `GetTestStatus`, `GetMetrics`, `GetThresholds`, `StopTest`. Implement `GrafanaCloudProvider` first. |

### D-3: Structured Multi-Metric Result Object

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | Instead of returning a single scalar value per metric query (like Prometheus), return a structured object with multiple fields. This lets users write richer `successCondition` expressions like `result.p95 < 500 && result.errorRate < 0.01` in a SINGLE metric definition. Reduces AnalysisTemplate boilerplate. |
| **Complexity** | Medium |
| **Notes** | Argo Rollouts' expression engine supports map access. Return `map[string]interface{}` from the metric plugin. Fields: `httpReqDuration.p50`, `httpReqDuration.p95`, `httpReqDuration.p99`, `httpReqFailedRate`, `httpReqs`, `thresholdsPassed`, `thresholdsTotal`, `thresholdsFailed`. |

**AnalysisTemplate YAML:**

```yaml
metrics:
  - name: k6-comprehensive
    interval: 30s
    successCondition: >
      result.httpReqDuration.p95 < 500 &&
      result.httpReqFailedRate < 0.01 &&
      result.thresholdsPassed == true
    failureLimit: 3
    provider:
      plugin:
        argo-rollouts-k6-plugin/metric:
          testRunId: "{{ args.test-run-id }}"
          apiToken: "{{ args.k6-api-token }}"
          stackId: "{{ args.k6-stack-id }}"
          metric: summary
```

### D-4: Test Run Status Metadata in GetMetadata

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | The `GetMetadata` method on the metric interface lets plugins return extra key-value pairs stored alongside measurements. Use this to surface k6 Cloud URLs, test run status, VU counts, and threshold details in the AnalysisRun status. Makes debugging failed rollouts much easier -- users can click through to the k6 Cloud dashboard. |
| **Complexity** | Low |
| **Notes** | Return metadata like `k6CloudUrl`, `testRunStatus`, `vuCount`, `duration`. Visible in `kubectl get analysisrun -o yaml`. |

### D-5: Automatic Test Run ID Resolution

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | Instead of requiring users to manually pass `testRunId`, the metric plugin can accept a `testId` (the load test definition ID) and automatically find the latest running or most recent completed test run. This eliminates the need for manual wiring between step and metric plugins in simple use cases. |
| **Complexity** | Medium |
| **Notes** | Query `/loadtests/v2/tests/{id}` to get `test_run_ids`, pick the latest. Configurable: `latestRun: true` or `testRunId: explicit`. |

**AnalysisTemplate YAML (simplified):**

```yaml
metrics:
  - name: k6-gate
    interval: 30s
    successCondition: "result.thresholdsPassed == true"
    provider:
      plugin:
        argo-rollouts-k6-plugin/metric:
          testId: "12345"
          latestRun: true
          apiToken: "{{ args.k6-api-token }}"
          stackId: "{{ args.k6-stack-id }}"
          metric: thresholds
```

### D-6: Graceful Termination with Test Stop

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | When a rollout is aborted or terminated, the step plugin's `Terminate`/`Abort` methods should stop the running k6 Cloud test. Prevents orphaned test runs from consuming cloud credits and generating misleading results. No other community plugin handles this level of cleanup for external resources. |
| **Complexity** | Low-Medium |
| **Notes** | Call k6 Cloud API to stop the test run on `Terminate` and `Abort`. Store `testRunId` in step status for retrieval. |

### D-7: Custom k6 Metric Support

| Aspect | Detail |
|---------|--------|
| **Value Proposition** | k6 supports custom metrics (Counter, Gauge, Rate, Trend) defined in test scripts. Power users define business-specific metrics like `successful_logins` or `checkout_conversion_rate`. Exposing arbitrary custom metrics via the plugin config makes this useful beyond HTTP-level testing. |
| **Complexity** | Medium |
| **Notes** | Accept `metric: custom` with `metricName: my_custom_metric` and `aggregation: p95|rate|count|value`. Query k6 Cloud API with the custom metric name. |

**AnalysisTemplate YAML:**

```yaml
metrics:
  - name: checkout-conversion
    interval: 30s
    successCondition: "result > 0.95"
    provider:
      plugin:
        argo-rollouts-k6-plugin/metric:
          testRunId: "{{ args.test-run-id }}"
          apiToken: "{{ args.k6-api-token }}"
          stackId: "{{ args.k6-stack-id }}"
          metric: custom
          metricName: checkout_success_rate
          aggregation: rate
```

## Anti-Features

Features to explicitly NOT build in v1.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| **In-cluster k6 Job execution** | Significant Kubernetes RBAC complexity (creating Jobs, watching Pods, log collection). Grafana Cloud k6 covers the initial use case. Build the provider interface (D-2) so this can be added later without breaking changes. | Defer to v2. Ship provider interface abstraction now. |
| **ConfigMap-based k6 script upload** | Grafana Cloud k6 tests are identified by test ID. Uploading scripts requires managing script content, dependencies, and versioning -- a different problem domain. The k6 Cloud UI already handles this well. | Defer to v2. Accept `testId` only in v1. |
| **k6 binary execution mode** | Running k6 as a subprocess inside the rollout controller is fragile, resource-intensive, and creates security concerns. The plugin runs as a child process of the controller -- it shouldn't spawn its own children. | Defer to v2+. Use k6 Cloud or k6-operator instead. |
| **VUs/duration override in step plugin** | Tempting to let users override VU count or test duration at trigger time, but this creates divergence between what's defined in k6 Cloud and what runs. Leads to "works locally, fails in CI" confusion. | Accept the test configuration as defined in k6 Cloud. If users need overrides, they should update the test definition. |
| **Grafana dashboard generation** | Out of scope -- separate concern. Dashboards require Grafana API access, templates, and maintenance. Many users already have their own dashboard setup. | Document how to link to k6 Cloud test results URL from AnalysisRun metadata (D-4). |
| **Test script sourcing from URL/git** | Network reliability concerns, git auth complexity, no caching story. These are v3+ concerns. | Defer. Use `testId` referencing existing k6 Cloud tests. |
| **Multi-provider in single binary** | Shipping both metric and step plugin as one binary adds complexity to the build, registration, and upgrade story. | Ship as two separate binaries with a shared Go module for common code (API client, types, provider interface). |
| **Real-time streaming metrics** | k6 Cloud evaluates thresholds every 60 seconds. Polling more frequently than that returns stale data and wastes API calls. Don't build a streaming/websocket integration. | Use polling with sensible intervals (30s-60s). Document recommended `interval` values. |
| **Helm chart or operator** | Distribution complexity. Argo Rollouts plugins are binaries registered via ConfigMap -- no operator needed. A Helm chart adds a maintenance burden without clear value for a plugin. | Provide clear ConfigMap YAML examples and a one-liner install guide. |

## Feature Dependencies

```
TS-4 (Secrets) --> TS-1 (Metric Plugin)    [metrics need auth]
TS-4 (Secrets) --> TS-2 (Step Plugin)      [step needs auth]
TS-1 (Metric Plugin) --> TS-3 (Thresholds) [thresholds are a metric type]
TS-1 (Metric Plugin) --> TS-5 (Latency)    [latency is a metric type]
TS-1 (Metric Plugin) --> TS-6 (Error Rate) [error rate is a metric type]
TS-1 (Metric Plugin) --> TS-7 (Throughput) [throughput is a metric type]
TS-8 (ConfigMap Reg) --> TS-1, TS-2        [plugins must register to load]
TS-1 + TS-2 --> D-1 (Combined Workflow)    [needs both plugins working]
TS-1 --> D-3 (Structured Results)          [enhancement to metric return]
TS-1 --> D-4 (Metadata)                    [enhancement to metric metadata]
TS-1 --> D-5 (Auto Test Run ID)            [enhancement to metric config]
TS-2 --> D-6 (Graceful Termination)        [enhancement to step lifecycle]
TS-1 --> D-7 (Custom Metrics)              [enhancement to metric queries]
D-2 (Provider Interface) is architectural  [no runtime dependency, but design dependency on all features]
```

## MVP Recommendation

### Phase 1: Core Plugin Infrastructure

Prioritize:
1. **TS-8** (ConfigMap registration) + **D-2** (Provider interface) -- foundational architecture
2. **TS-4** (Secrets management) -- required by everything else
3. **TS-2** (Step plugin: trigger and wait) -- simplest end-to-end value
4. **TS-3** (Threshold pass/fail metric) -- simplest metric, highest value

This gives users the most common workflow: "trigger a k6 Cloud test at canary step 20%, wait for it to finish, gate on threshold pass/fail."

### Phase 2: Rich Metrics

5. **TS-1** (Full metric plugin with configurable metrics)
6. **TS-5** (p95/p99 latency)
7. **TS-6** (Error rate)
8. **TS-7** (Throughput)
9. **TS-9** (Example templates)
10. **D-3** (Structured multi-metric results)

### Phase 3: Polish and Differentiation

11. **D-1** (Combined step + metric workflow)
12. **D-4** (Metadata with k6 Cloud URLs)
13. **D-5** (Automatic test run ID resolution)
14. **D-6** (Graceful termination)
15. **D-7** (Custom metric support)

### Rationale

- Step plugin first because it's self-contained (trigger, poll, return pass/fail) and demonstrates value with zero AnalysisTemplate complexity
- Threshold metric second because it reuses existing k6 script thresholds rather than forcing users to redefine criteria in YAML
- Rich metrics third because they add flexibility but require users to understand `successCondition` expressions
- Differentiators last because they polish the experience but aren't blockers for adoption

Defer: VUs override, ConfigMap scripts, in-cluster execution, Helm chart -- all explicitly out of scope for v1 per PROJECT.md.

## Sources

- [Argo Rollouts Plugin System](https://argo-rollouts.readthedocs.io/en/stable/plugins/)
- [Argo Rollouts Analysis & Progressive Delivery](https://argoproj.github.io/argo-rollouts/features/analysis/)
- [Argo Rollouts Canary Step Plugin](https://argoproj.github.io/argo-rollouts/features/canary/plugins/)
- [Argo Rollouts Plugin Types Go Package](https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types)
- [Prometheus Sample Metric Plugin](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus)
- [OpenSearch Metric Plugin](https://github.com/argoproj-labs/rollouts-plugin-metric-opensearch)
- [Datadog Analysis Template Docs](https://argo-rollouts.readthedocs.io/en/stable/analysis/datadog/)
- [NewRelic Analysis Template Docs](https://argo-rollouts.readthedocs.io/en/stable/analysis/newrelic/)
- [Grafana Cloud k6 REST API](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/)
- [Grafana Cloud k6 Metrics API](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/metrics/)
- [Grafana Cloud k6 Test Runs API](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/test-runs/)
- [Grafana Cloud k6 Authentication](https://grafana.com/docs/grafana-cloud/testing/k6/author-run/tokens-and-cli-authentication/)
- [k6 Thresholds Documentation](https://grafana.com/docs/k6/latest/using-k6/thresholds/)
- [k6 Metrics Documentation](https://grafana.com/docs/k6/latest/using-k6/metrics/)
- [k6 Cloud Test Status Codes](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-test-status-codes/)
