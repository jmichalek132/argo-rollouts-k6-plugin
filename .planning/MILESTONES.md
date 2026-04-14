# Milestones

## v1.0 MVP (Shipped: 2026-04-14)

**Phases completed:** 4 phases, 9 plans, 16 tasks

**Key accomplishments:**

- Provider interface with 4 methods, RunResult/RunState types, and fully tested GrafanaCloudProvider using k6-cloud-openapi-client-go v6 API
- Two static plugin binaries with go-plugin handshake (metricprovider/step), Makefile build pipeline, and golangci-lint v2 with forbidigo stdout detection
- K6MetricProvider implementing all 7 RpcMetricProvider methods with async Run/Resume lifecycle, 4 metric types (thresholds/http_req_failed/http_req_duration/http_reqs), v5 aggregate HTTP client, and 91.7% test coverage
- Metric-plugin binary fully wired with GrafanaCloudProvider -> K6MetricProvider -> RpcMetricProviderPlugin, both binaries compile statically, 91.7% test coverage
- K6StepPlugin with fire-and-wait lifecycle: trigger/poll via Provider interface, timeout management, json.RawMessage state persistence, 89.1% test coverage
- Wired K6StepPlugin into step-plugin binary with GrafanaCloudProvider backend and RpcStepPlugin registration
- GoReleaser v2 multi-arch config (8 binaries, SHA256 checksums) with CI/release/e2e GitHub Actions workflows
- Kind cluster e2e test infrastructure with configurable mock k6 API server and 4 test scenarios validating full plugin binary loading path
- Three example YAML patterns (threshold-gate, error-rate+latency, full canary) plus README installation guide and CONTRIBUTING provider interface documentation

---
