# Phase 4: Release & Examples - Research

**Researched:** 2026-04-10
**Domain:** GoReleaser, GitHub Actions CI/CD, sigs.k8s.io/e2e-framework, Argo Rollouts plugin distribution & YAML authoring
**Confidence:** HIGH

## Summary

Phase 4 turns the completed plugin codebase into a community-consumable project. The work splits into four distinct areas: (1) GoReleaser configuration for multi-arch static binary distribution, (2) GitHub Actions CI/CD for lint/test/build on PR and release on tag, (3) e2e tests using sigs.k8s.io/e2e-framework with kind cluster and an in-process HTTP mock of the k6 API, and (4) example YAML manifests plus README/CONTRIBUTING documentation.

GoReleaser v2 handles two-binary builds from a single Go module natively via multiple `builds` entries. The `archives` section with `format: binary` produces flat binaries without archive wrapping, which is exactly what Argo Rollouts ConfigMap URLs need. SHA256 checksums are generated automatically.

The e2e-framework provides a TestMain-based lifecycle for kind cluster creation/teardown. Plugin binaries are loaded via `file://` URLs in the `argo-rollouts-config` ConfigMap. The mock server pattern uses a `net/http` handler with per-test response sequences, avoiding any external test infrastructure.

**Primary recommendation:** Implement in order: goreleaser config -> CI workflows -> e2e tests -> examples -> docs. The goreleaser config and CI are prerequisites for the examples (which reference release URLs) and the README (which describes installation).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- D-01: Mock k6 API using in-process `net/http` mock server handling TriggerRun, GetRunResult, StopRun endpoints
- D-02: e2e tests use full binary path via kind cluster + `file://` URL in ConfigMap
- D-03: Use `sigs.k8s.io/e2e-framework` for kind cluster lifecycle; tests under `e2e/` or `test/e2e/`
- D-04: Minimum 4 e2e scenarios (metric pass/fail, step pass/fail)
- D-05: 4 platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- D-06: Flat asset naming (`metric-plugin_linux_amd64`, etc.), no archive wrapping
- D-07: SHA256 checksums via goreleaser `checksums.txt`; LDFLAGS: `-s -w -X main.version={{.Version}}`
- D-08: CGO_ENABLED=0 for all builds
- D-09: CI on PR/main: lint, test, build-check. No release artifacts
- D-10: Release on tag `v*` via goreleaser + GITHUB_TOKEN
- D-11: e2e tests in CI only on tag push (kind overhead too high for every PR)
- D-12: Fully working example YAML with placeholder credentials
- D-13: Three examples: `examples/threshold-gate/`, `examples/error-rate-latency/`, `examples/canary-full/`
- D-14: Plugin names: `jmichalek132/k6` (metric), `jmichalek132/k6-step` (step)
- D-15: Self-contained README (no wiki/docs site)
- D-16: CONTRIBUTING.md with dev setup + provider interface guide

### Claude's Discretion
- Exact kind version to pin in CI
- Whether to use `act` for local CI testing in CONTRIBUTING.md
- goreleaser `before.hooks` (go mod tidy, generate)
- e2e test helper utilities (retry logic, wait conditions)
- Whether to add SECURITY.md stub

### Deferred Ideas (OUT OF SCOPE)
- SECURITY.md (add stub only if trivial)
- Nightly/snapshot builds
- GitHub Container Registry image publishing
- Wiki or docs site
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PLUG-03 | Both binaries registered in argo-rollouts-config ConfigMap with name, location (GitHub Releases URL), and SHA256 checksum | ConfigMap structure verified: `metricProviderPlugins` / `stepPlugins` sections with `name`, `location`, `sha256` fields |
| DIST-02 | goreleaser produces multi-arch binaries (4 platforms) with SHA256 checksums | GoReleaser v2 builds config verified; `format: binary` for flat naming; `checksum.algorithm: sha256` |
| DIST-03 | GitHub Actions CI: lint, test, build on PR/push; release on tag | Two-workflow pattern: `ci.yml` (PR/push) + `release.yml` (v* tag); golangci-lint-action v9 + goreleaser-action v7 |
| EXAM-01 | Example AnalysisTemplate for threshold-only gate | AnalysisTemplate structure verified from sample-prometheus plugin; plugin config under `provider.plugin["jmichalek132/k6"]` |
| EXAM-02 | Example AnalysisTemplate for error rate + p95 latency combined | Same structure as EXAM-01 but with two metrics in the same AnalysisTemplate |
| EXAM-03 | Example Rollout with step plugin trigger + metric analysis gate | Rollout canary steps with `plugin:` entries verified; step plugin `status.stepPluginStatuses[]` available |
| EXAM-04 | README with installation, credentials, quick-start | Covers ConfigMap setup, binary download + checksum verification, Secret YAML, minimal AnalysisTemplate |
| EXAM-05 | CONTRIBUTING.md for contributors | Dev setup mirrors Makefile targets; provider interface guide from internal/provider/provider.go |
| TEST-02 | e2e tests against kind cluster with mocked k6 API | e2e-framework v0.6.0 TestMain pattern; kind cluster lifecycle; `file://` binary loading; in-process mock server |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| GoReleaser (CLI) | v2.15.x | Cross-compilation + release | Standard Go release tool; handles multi-binary, multi-arch, checksums, GitHub Releases in one config |
| goreleaser/goreleaser-action | v7 | GitHub Actions integration | Official action; `version: "~> v2"` pins to latest v2.x |
| golangci/golangci-lint-action | v9 | CI linting | Official action; works with golangci-lint v2 |
| sigs.k8s.io/e2e-framework | v0.6.0 | e2e test lifecycle | Standard K8s e2e framework; TestMain pattern for kind cluster |
| actions/setup-go | v5 | Go installation in CI | Official; `go-version-file: 'go.mod'` reads version automatically |
| actions/checkout | v4 | Git checkout in CI | `fetch-depth: 0` required for goreleaser version detection |

### Supporting (already in go.mod)
| Library | Version | Purpose |
|---------|---------|---------|
| github.com/stretchr/testify | v1.11.1 | Assertions in e2e tests (already a dependency) |

### New Dependencies Required
| Library | Purpose | Impact |
|---------|---------|--------|
| sigs.k8s.io/e2e-framework v0.6.0 | kind cluster lifecycle for e2e tests | Adds client-go, controller-runtime to go.mod. Use `//go:build e2e` tag to isolate from unit test builds |

## Architecture Patterns

### Recommended Project Structure (new files)
```
.goreleaser.yaml                    # GoReleaser v2 config (two builds)
.github/
  workflows/
    ci.yml                          # PR/push: lint + test + build
    release.yml                     # v* tag: goreleaser release
e2e/
  main_test.go                      # TestMain: kind cluster setup/teardown
  metric_plugin_test.go             # Metric plugin e2e scenarios
  step_plugin_test.go               # Step plugin e2e scenarios
  mock_k6_server.go                 # Configurable HTTP mock for k6 API
  testdata/
    argo-rollouts-config.yaml       # ConfigMap template with file:// paths
    analysistemplate-thresholds.yaml # AnalysisTemplate for metric e2e
    rollout-step.yaml               # Rollout for step e2e
examples/
  threshold-gate/
    analysistemplate.yaml
    secret.yaml
    configmap-snippet.yaml
  error-rate-latency/
    analysistemplate.yaml
    secret.yaml
    configmap-snippet.yaml
  canary-full/
    rollout.yaml
    analysistemplate.yaml
    secret.yaml
    configmap-snippet.yaml
README.md
CONTRIBUTING.md
```

### Pattern 1: GoReleaser Two-Binary Build
**What:** Single `.goreleaser.yaml` with two `builds` entries, each pointing to a different `cmd/` directory
**When to use:** Always -- this is the project's release mechanism

```yaml
# .goreleaser.yaml
version: 2

builds:
  - id: metric-plugin
    main: ./cmd/metric-plugin
    binary: metric-plugin
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w -X main.version={{.Version}}
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

  - id: step-plugin
    main: ./cmd/step-plugin
    binary: step-plugin
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w -X main.version={{.Version}}
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

archives:
  - format: binary
    name_template: "{{ .Binary }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  github:
    owner: jmichalek132
    name: argo-rollouts-k6-plugin
```
**Source:** [GoReleaser Builds docs](https://goreleaser.com/customization/builds/go/), [GoReleaser Archives docs](https://goreleaser.com/customization/package/archives/)

**Critical:** Both `cmd/metric-plugin/main.go` and `cmd/step-plugin/main.go` need a `var version = "dev"` declaration for the `-X main.version={{.Version}}` LDFLAGS injection to work. The Makefile already has the LDFLAGS but the variable is missing from the source.

### Pattern 2: e2e-framework TestMain with Kind
**What:** `TestMain` function manages kind cluster lifecycle; individual tests use the framework's Feature/Assess pattern
**When to use:** All e2e tests

```go
// e2e/main_test.go
//go:build e2e

package e2e

import (
    "os"
    "testing"

    "sigs.k8s.io/e2e-framework/pkg/env"
    "sigs.k8s.io/e2e-framework/pkg/envconf"
    "sigs.k8s.io/e2e-framework/pkg/envfuncs"
    "sigs.k8s.io/e2e-framework/support/kind"
)

var testenv env.Environment

func TestMain(m *testing.M) {
    testenv = env.New()
    kindClusterName := envconf.RandomName("k6-plugin", 16)
    namespace := envconf.RandomName("k6-e2e", 16)

    testenv.Setup(
        envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
        envfuncs.CreateNamespace(namespace),
        // Custom: install Argo Rollouts CRDs + controller
        // Custom: apply argo-rollouts-config ConfigMap with file:// paths
    )
    testenv.Finish(
        envfuncs.DeleteNamespace(namespace),
        envfuncs.DestroyCluster(kindClusterName),
    )

    os.Exit(testenv.Run(m))
}
```
**Source:** [e2e-framework README](https://github.com/kubernetes-sigs/e2e-framework), [e2e-framework examples](https://github.com/kubernetes-sigs/e2e-framework/blob/main/examples/)

### Pattern 3: Configurable Per-Test HTTP Mock Server
**What:** An `net/http` handler that can be programmed per test with a sequence of responses
**When to use:** All e2e tests (and could be reused for integration tests)

```go
// e2e/mock_k6_server.go
//go:build e2e

package e2e

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "sync"
)

// MockK6Server simulates the Grafana Cloud k6 API for testing.
type MockK6Server struct {
    server    *httptest.Server
    mu        sync.Mutex
    responses map[string][]mockResponse // path -> sequence of responses
    callCount map[string]int
}

type mockResponse struct {
    statusCode int
    body       interface{}
}

func NewMockK6Server() *MockK6Server {
    m := &MockK6Server{
        responses: make(map[string][]mockResponse),
        callCount: make(map[string]int),
    }
    m.server = httptest.NewServer(http.HandlerFunc(m.handler))
    return m
}

func (m *MockK6Server) URL() string { return m.server.URL }
func (m *MockK6Server) Close()      { m.server.Close() }

// OnPath programs a sequence of responses for a given path pattern.
func (m *MockK6Server) OnPath(path string, responses ...mockResponse) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.responses[path] = responses
}

func (m *MockK6Server) handler(w http.ResponseWriter, r *http.Request) {
    m.mu.Lock()
    defer m.mu.Unlock()
    // Match path and return next response in sequence
    // ...
}
```

This pattern allows each test to program its own mock behavior: "first call returns Running, second returns Passed" etc.

### Pattern 4: Plugin Registration ConfigMap
**What:** The `argo-rollouts-config` ConfigMap that registers both plugins with the controller
**When to use:** Every deployment and every e2e test

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  metricProviderPlugins: |-
    - name: "jmichalek132/k6"
      location: "https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/metric-plugin_linux_amd64"
      sha256: "<SHA256_FROM_CHECKSUMS_TXT>"
  stepPlugins: |-
    - name: "jmichalek132/k6-step"
      location: "https://github.com/jmichalek132/argo-rollouts-k6-plugin/releases/download/v0.1.0/step-plugin_linux_amd64"
      sha256: "<SHA256_FROM_CHECKSUMS_TXT>"
```
**Source:** [Argo Rollouts Plugin Docs](https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/plugins.md), [Metric Plugin Docs](https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/analysis/plugins.md)

For e2e tests (kind cluster), use `file://` paths instead:
```yaml
  metricProviderPlugins: |-
    - name: "jmichalek132/k6"
      location: "file:///tmp/argo-rollouts/metric-plugin"
  stepPlugins: |-
    - name: "jmichalek132/k6-step"
      location: "file:///tmp/argo-rollouts/step-plugin"
```

### Pattern 5: Plugin AnalysisTemplate YAML
**What:** AnalysisTemplate referencing the k6 metric plugin
**When to use:** All metric plugin examples

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
**Source:** [rollouts-plugin-metric-sample-prometheus README](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus)

Note: For the `thresholds` metric, the plugin returns `1` (passed) or `0` (failed) as Measurement.Value. `successCondition: "result == 1"` gates on all thresholds passing.

### Pattern 6: Step Plugin in Rollout Canary Steps
**What:** Rollout spec with a step plugin step
**When to use:** canary-full example

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: my-canary
spec:
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
        - analysis:
            templates:
              - templateName: k6-threshold-check
            args:
              - name: api-token
                value: "<YOUR_API_TOKEN>"
              - name: stack-id
                value: "<YOUR_STACK_ID>"
              - name: test-id
                value: "<YOUR_TEST_ID>"
        - setWeight: 50
        - pause: {duration: 30s}
        - setWeight: 100
```
**Source:** [Argo Rollouts Canary Step Plugin docs](https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/features/canary/plugins.md)

### Anti-Patterns to Avoid
- **Sharing mock server between tests:** Each test must create its own mock with its own response sequence. Shared state causes flaky tests.
- **e2e deps in main go.mod without build tag:** The `sigs.k8s.io/e2e-framework` pulls in client-go and controller-runtime. Use `//go:build e2e` tag on all e2e test files to avoid bloating `go test ./...`.
- **Hardcoding binary paths in e2e tests:** Use `os.Getenv("METRIC_PLUGIN_PATH")` or build the binaries in TestMain setup and reference them dynamically.
- **Using `go test -tags e2e` in regular CI:** e2e tests need kind + docker. Only run on tag push or manual dispatch.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Multi-arch cross-compilation | Shell scripts with `GOOS=X GOARCH=Y go build` | GoReleaser `builds` | Handles 8 build targets (2 binaries x 4 platforms), checksums, GitHub Release upload |
| SHA256 checksum generation | Manual `shasum -a 256` scripts | GoReleaser `checksum` | Automatic, included in release artifacts, names match binaries |
| Kind cluster lifecycle | Manual `kind create cluster` + `kind delete cluster` | e2e-framework `envfuncs.CreateCluster`/`DestroyCluster` | Handles cluster naming, kubeconfig, cleanup on failure |
| CI workflow for Go projects | Custom shell-based CI | golangci-lint-action + setup-go + goreleaser-action | Maintained by respective projects, handles caching, version pinning |

## Common Pitfalls

### Pitfall 1: Missing `var version` in main.go
**What goes wrong:** GoReleaser LDFLAGS `-X main.version={{.Version}}` silently does nothing if the variable doesn't exist in the binary's `main` package.
**Why it happens:** The Makefile already has the LDFLAGS but neither `cmd/metric-plugin/main.go` nor `cmd/step-plugin/main.go` declares `var version = "dev"`.
**How to avoid:** Add `var version = "dev"` to both main.go files before configuring goreleaser.
**Warning signs:** `goreleaser build` succeeds but the binary reports "dev" for its version.

### Pitfall 2: GoReleaser v2 requires `version: 2` in config
**What goes wrong:** GoReleaser v2 rejects v1 config files without the version field.
**Why it happens:** GoReleaser v2 removed deprecated v1 options; the config schema changed.
**How to avoid:** Always include `version: 2` as the first line of `.goreleaser.yaml`.
**Warning signs:** `goreleaser` CLI exits with schema validation errors.

### Pitfall 3: archive format binary naming
**What goes wrong:** With `format: binary`, the `name_template` in archives controls the upload filename, NOT the filename in the `dist/` directory. This confuses local testing.
**Why it happens:** GoReleaser's archive naming only applies to the release artifact name.
**How to avoid:** Test with `goreleaser build --snapshot --clean` first, then verify release artifact names with `goreleaser release --snapshot --clean`.

### Pitfall 4: e2e-framework dependency explosion
**What goes wrong:** Adding `sigs.k8s.io/e2e-framework` to go.mod brings in 50+ transitive dependencies (client-go, controller-runtime, etc.).
**Why it happens:** The framework needs full K8s client capabilities.
**How to avoid:** Use `//go:build e2e` tag on all e2e test files. Regular `go test ./...` won't compile them. CI e2e job uses `go test -tags e2e ./e2e/...`.
**Warning signs:** `go mod tidy` adding many new dependencies; `go test ./...` suddenly much slower.

### Pitfall 5: kind cluster plugin binary not accessible
**What goes wrong:** Plugin binary compiled on macOS (darwin/arm64) can't run inside the kind cluster (linux/amd64).
**Why it happens:** kind runs Linux containers. The plugin binary must be linux-targeted.
**How to avoid:** In e2e setup, cross-compile: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/argo-rollouts/metric-plugin ./cmd/metric-plugin`. Then copy into the kind container using `docker cp` or kind's `extraMounts`.
**Warning signs:** Controller log shows "exec format error" when loading plugin.

### Pitfall 6: fetch-depth: 0 omitted in checkout
**What goes wrong:** GoReleaser can't determine version from git tags, produces incorrect version strings.
**Why it happens:** GitHub Actions `checkout` defaults to `fetch-depth: 1` (shallow clone).
**How to avoid:** Always use `fetch-depth: 0` in the checkout step for the release workflow.
**Warning signs:** GoReleaser version shows "0.0.0" or a commit hash instead of the tag.

### Pitfall 7: Step plugin status -> AnalysisTemplate arg passthrough not native
**What goes wrong:** Attempting to use `{{steps.k6-load-test.outputs.testRunId}}` in AnalysisTemplate args -- this expression syntax does not exist in Argo Rollouts.
**Why it happens:** Step plugin status is available in `status.stepPluginStatuses[]` on the Rollout object, but there is no built-in template engine to pass step outputs into inline AnalysisTemplate args.
**How to avoid:** For the `canary-full` example, the step plugin triggers the run AND the metric plugin triggers its own independent run using the same `testId`. They don't share a `testRunId`. This is the correct v1 pattern. The testRunId handoff (STEP2-01) is explicitly a v2 requirement.
**Warning signs:** Rollout YAML with `{{steps.X.outputs.Y}}` fails to parse because this is not a valid Argo Rollouts expression.

## Code Examples

### GoReleaser v2 Config (complete)
```yaml
# .goreleaser.yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - id: metric-plugin
    main: ./cmd/metric-plugin
    binary: metric-plugin
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w -X main.version={{.Version}}
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

  - id: step-plugin
    main: ./cmd/step-plugin
    binary: step-plugin
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w -X main.version={{.Version}}
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

archives:
  - format: binary
    name_template: "{{ .Binary }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  github:
    owner: jmichalek132
    name: argo-rollouts-k6-plugin
```
**Source:** [GoReleaser Go Builds](https://goreleaser.com/customization/builds/go/), [GoReleaser Archives](https://goreleaser.com/customization/package/archives/), [GoReleaser Checksum](https://goreleaser.com/customization/checksum/)

### GitHub Actions CI Workflow
```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@v9
        with:
          version: v2.1.6

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: make test

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: make build
```

### GitHub Actions Release Workflow
```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```
**Source:** [GoReleaser GitHub Actions](https://goreleaser.com/ci/actions/), [goreleaser-action](https://github.com/goreleaser/goreleaser-action)

### e2e TestMain Pattern
```go
//go:build e2e

package e2e

import (
    "os"
    "testing"

    "sigs.k8s.io/e2e-framework/pkg/env"
    "sigs.k8s.io/e2e-framework/pkg/envconf"
    "sigs.k8s.io/e2e-framework/pkg/envfuncs"
    "sigs.k8s.io/e2e-framework/support/kind"
)

var testenv env.Environment

func TestMain(m *testing.M) {
    testenv = env.New()
    kindClusterName := envconf.RandomName("k6-plugin", 16)
    namespace := envconf.RandomName("k6-e2e", 16)

    testenv.Setup(
        envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
        envfuncs.CreateNamespace(namespace),
        installArgoRollouts,     // Custom: kubectl apply CRDs + controller
        buildAndLoadPlugins,     // Custom: cross-compile + docker cp into kind node
        applyPluginConfigMap,    // Custom: ConfigMap with file:// paths
    )
    testenv.Finish(
        envfuncs.DeleteNamespace(namespace),
        envfuncs.DestroyCluster(kindClusterName),
    )

    os.Exit(testenv.Run(m))
}
```
**Source:** [e2e-framework examples](https://github.com/kubernetes-sigs/e2e-framework)

### AnalysisTemplate for Threshold-Only Gate (EXAM-01)
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

### AnalysisTemplate for Error Rate + p95 Combined (EXAM-02)
```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: k6-error-rate-latency
spec:
  args:
    - name: api-token
    - name: stack-id
    - name: test-id
    - name: max-error-rate
      value: "0.01"       # 1% default
    - name: max-p95-ms
      value: "500"        # 500ms default
  metrics:
    - name: k6-error-rate
      interval: 30s
      successCondition: "asFloat(result) <= asFloat(args.max-error-rate)"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            testId: "{{args.test-id}}"
            apiToken: "{{args.api-token}}"
            stackId: "{{args.stack-id}}"
            metric: http_req_failed
    - name: k6-p95-latency
      interval: 30s
      successCondition: "asFloat(result) <= asFloat(args.max-p95-ms)"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            testId: "{{args.test-id}}"
            apiToken: "{{args.api-token}}"
            stackId: "{{args.stack-id}}"
            metric: http_req_duration
            aggregation: p95
```

### Secret YAML (placeholder)
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

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| GoReleaser v1 config | GoReleaser v2 with `version: 2` | 2024 | Removed deprecated options; config must specify version |
| golangci-lint-action v4 | golangci-lint-action v9 | 2026 | Works with golangci-lint v2 (config version "2") |
| goreleaser-action v5 | goreleaser-action v7 | 2025/2026 | Better v2 support, `version: "~> v2"` pin |
| e2e-framework v0.4.x | e2e-framework v0.6.0 | Jan 2025 | Benchmark support, vcluster/k3d providers, Go 1.23 minimum |

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | All builds/tests | Yes | 1.26.1 | -- |
| Docker | kind (e2e tests) | Yes | 29.3.0 | -- |
| golangci-lint | CI lint | Yes | v2.1.6 | -- |
| GoReleaser | Release workflow | No (CI only) | -- | Only needed in GitHub Actions; local `goreleaser build --snapshot` optional |
| kind | e2e tests | No (local) | -- | Install via `go install sigs.k8s.io/kind@latest` or `brew install kind` |
| gh | PR creation | Yes | 2.88.1 | -- |

**Missing dependencies with no fallback:**
- None -- all critical tools are either available locally or only needed in CI (where they're installed by Actions)

**Missing dependencies with fallback:**
- GoReleaser: Not installed locally but only needed in CI. Local testing via `go install github.com/goreleaser/goreleaser/v2@latest` or Homebrew
- kind: Not installed locally but can be installed via `go install` or Homebrew for local e2e development

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify v1.11.1 (unit), e2e-framework v0.6.0 (e2e) |
| Config file | `.golangci.yml` (lint), no pytest/jest equivalent -- Go stdlib |
| Quick run command | `make test` (`go test -race -v -count=1 ./...`) |
| Full suite command | `make lint && make test && go test -tags e2e -v -count=1 ./e2e/...` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TEST-02a | Metric plugin: AnalysisRun pass (mock returns Passed) | e2e | `go test -tags e2e -v -run TestMetricPluginPass ./e2e/...` | Wave 0 |
| TEST-02b | Metric plugin: AnalysisRun fail (mock returns Failed) | e2e | `go test -tags e2e -v -run TestMetricPluginFail ./e2e/...` | Wave 0 |
| TEST-02c | Step plugin: Rollout advances (mock returns Passed) | e2e | `go test -tags e2e -v -run TestStepPluginPass ./e2e/...` | Wave 0 |
| TEST-02d | Step plugin: Rollout rolls back (mock returns Failed) | e2e | `go test -tags e2e -v -run TestStepPluginFail ./e2e/...` | Wave 0 |
| DIST-02 | goreleaser produces 8 binaries + checksums | smoke | `goreleaser build --snapshot --clean && ls dist/` | Wave 0 |
| DIST-03 | CI workflows valid YAML | lint | `actionlint .github/workflows/*.yml` (optional) | Wave 0 |
| PLUG-03 | ConfigMap YAML has correct structure | manual | Visual inspection of example YAML | -- |
| EXAM-01 | Threshold-gate example valid YAML | manual | `kubectl apply --dry-run=client -f examples/threshold-gate/` | -- |
| EXAM-02 | Error-rate+latency example valid YAML | manual | `kubectl apply --dry-run=client -f examples/error-rate-latency/` | -- |
| EXAM-03 | Canary-full example valid YAML | manual | `kubectl apply --dry-run=client -f examples/canary-full/` | -- |
| EXAM-04 | README completeness | manual | Review sections: install, credentials, quick-start | -- |
| EXAM-05 | CONTRIBUTING completeness | manual | Review sections: dev setup, provider guide | -- |

### Sampling Rate
- **Per task commit:** `make lint && make test`
- **Per wave merge:** `make lint && make test && goreleaser build --snapshot --clean`
- **Phase gate:** Full suite including e2e (requires kind + docker)

### Wave 0 Gaps
- [ ] `e2e/main_test.go` -- TestMain with kind cluster lifecycle
- [ ] `e2e/mock_k6_server.go` -- Configurable HTTP mock for k6 API
- [ ] `e2e/metric_plugin_test.go` -- Metric plugin e2e scenarios
- [ ] `e2e/step_plugin_test.go` -- Step plugin e2e scenarios
- [ ] `go get sigs.k8s.io/e2e-framework@v0.6.0` -- Add e2e-framework dependency
- [ ] `var version = "dev"` in both cmd/*/main.go -- Required for LDFLAGS injection

## Open Questions

1. **Step plugin -> metric plugin testRunId handoff (canary-full example)**
   - What we know: Step plugin stores `runId` in `status.stepPluginStatuses[].status` (JSON). Metric plugin can accept `testRunId` for poll-only mode. Argo Rollouts does NOT have a template engine to pass step outputs into AnalysisTemplate args.
   - What's unclear: Whether the `canary-full` example should show both plugins using the same `testId` (triggering independent runs) or whether to document this as a future capability (STEP2-01 in v2 requirements).
   - Recommendation: For v1, the canary-full example shows the step plugin as a gate (trigger + wait for pass/fail) followed by an analysis step that triggers its own independent metric evaluation. The testRunId handoff is a v2 feature (STEP2-01). Document this limitation in the example's README with a "Future: v2 will support testRunId passthrough" note.

2. **e2e: Plugin binary loading into kind nodes**
   - What we know: `file://` URLs require the binary to exist on the controller's filesystem. The controller runs as a pod inside kind.
   - What's unclear: Exact mechanism to get the cross-compiled linux/amd64 binary into the controller pod.
   - Recommendation: Use kind's `extraMounts` to mount a host directory into the kind node, then the controller pod can access it via a hostPath volume. Alternative: `docker cp` into the kind node, then the controller deployment mounts the path. The e2e setup function should handle this.

3. **e2e: Mock server accessibility from inside kind**
   - What we know: The mock k6 API server runs on the host (in the test process). The plugin binary runs inside the controller pod, which makes HTTP calls to the k6 API.
   - What's unclear: How the plugin inside kind reaches the host mock server.
   - Recommendation: The mock server listens on the host. From inside kind, the host is accessible at a known IP (Docker Desktop: `host.docker.internal`, Linux Docker: `172.17.0.1` or the host network gateway). Alternatively, the PluginConfig `stackId` could encode the mock server URL. The GrafanaCloudProvider constructs the base URL from stackId, so the mock URL needs to be injected via the config. This needs careful e2e test design -- the plugin config in the AnalysisTemplate/Rollout must point to the mock server URL.

## Sources

### Primary (HIGH confidence)
- [GoReleaser Go Builds](https://goreleaser.com/customization/builds/go/) -- Multi-binary config, CGO_ENABLED, ldflags, goos/goarch
- [GoReleaser Archives](https://goreleaser.com/customization/package/archives/) -- `format: binary`, name_template
- [GoReleaser Checksum](https://goreleaser.com/customization/checksum/) -- SHA256 algorithm, checksums.txt
- [GoReleaser GitHub Actions](https://goreleaser.com/ci/actions/) -- Release workflow YAML, goreleaser-action v7
- [goreleaser/goreleaser-action](https://github.com/goreleaser/goreleaser-action) -- Action parameters, version pinning
- [golangci/golangci-lint-action](https://github.com/golangci/golangci-lint-action) -- v9 with golangci-lint v2 support
- [kubernetes-sigs/e2e-framework](https://github.com/kubernetes-sigs/e2e-framework) -- TestMain pattern, kind lifecycle, v0.6.0
- [Argo Rollouts Plugin Docs (source)](https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/plugins.md) -- ConfigMap structure with name/location/sha256
- [Argo Rollouts Metric Plugin Docs (source)](https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/analysis/plugins.md) -- AnalysisTemplate plugin reference
- [Argo Rollouts Step Plugin Docs (source)](https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/features/canary/plugins.md) -- Rollout step plugin config
- [rollouts-plugin-metric-sample-prometheus](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus) -- AnalysisTemplate YAML example for plugin provider

### Secondary (MEDIUM confidence)
- [GoReleaser v2 announcement](https://goreleaser.com/blog/goreleaser-v2/) -- Version 2 config changes
- [e2e-framework examples](https://github.com/kubernetes-sigs/e2e-framework/blob/main/examples/) -- TestMain and Feature patterns

### Tertiary (LOW confidence)
- Step plugin status -> AnalysisTemplate arg passthrough: Not found in official docs. Negative claim: this feature does not exist in Argo Rollouts v1.9.0. Needs validation if the canary-full example requires this pattern.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- GoReleaser, GitHub Actions, e2e-framework all verified from official docs and releases
- Architecture: HIGH -- GoReleaser config verified from docs; e2e patterns from framework examples; YAML from Argo Rollouts source
- Pitfalls: HIGH -- Binary cross-compilation for kind, LDFLAGS injection, archive naming all verified from goreleaser docs and practical experience
- e2e mock server accessibility: MEDIUM -- Host -> kind networking has multiple viable approaches but exact implementation depends on Docker runtime

**Research date:** 2026-04-10
**Valid until:** 2026-05-10 (stable domain -- GoReleaser v2, e2e-framework v0.6.0, Argo Rollouts v1.9.0 all released)
