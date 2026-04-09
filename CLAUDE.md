<!-- GSD:project-start source:PROJECT.md -->
## Project

**argo-rollouts-k6-plugin**

An open-source Argo Rollouts plugin written in Go that integrates k6 load testing as analysis gates in canary and blue-green deployments. It ships as both a **metric plugin** (polls k6 metrics on interval for AnalysisTemplate threshold evaluation) and a **step plugin** (one-shot: trigger a k6 run, wait for completion, return pass/fail). Initially targets Grafana Cloud k6 as the execution backend, with an extensible provider interface designed for in-cluster k6 Jobs and direct binary execution in future releases.

**Core Value:** Rollouts automatically pass or roll back based on real load test results — no manual gates, no guesswork.

### Constraints

- **Tech stack**: Go — matches Argo Rollouts ecosystem, k6 is also Go-native
- **Plugin interface**: Must implement the Argo Rollouts plugin gRPC interface exactly — breaking changes to the interface are not permitted
- **Distribution**: Binary must be statically linked and published to GitHub Releases with SHA256 checksum for controller verification
- **Dependencies**: Grafana Cloud k6 API credentials (token + org/project ID) passed via Kubernetes Secret, referenced in AnalysisTemplate
<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->
## Technology Stack

## Recommended Stack
### Core Framework
| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Go | 1.23+ | Plugin language | Argo Rollouts plugins communicate via `net/rpc` (gob encoding) -- must be Go. The controller's go.mod uses Go 1.26.1, but the plugin is a separate binary; Go 1.23 is the minimum to match the k6 client library |
| `github.com/argoproj/argo-rollouts` | v1.9.0 | Plugin interface types | Latest stable (released 2026-03-20). Provides `v1alpha1` CRD types, `utils/plugin/types` (RpcMetricProvider, RpcStep interfaces), and `metricproviders/plugin/rpc` / `rollout/steps/plugin/rpc` RPC wrappers |
| `github.com/hashicorp/go-plugin` | v1.6.3 | Plugin host framework | Argo Rollouts v1.9.0 uses this version (verified from go.mod). Provides the process management, handshake, and `net/rpc` transport between controller and plugin binary |
| `github.com/grafana/k6-cloud-openapi-client-go` | v1.7.0-0.1.0 | Grafana Cloud k6 API client | Official auto-generated Go client for the k6 Cloud REST API v6. Type-safe, covers all endpoints (LoadTests, TestRuns, Metrics). Released 2025-10-22 |
### Supporting Libraries
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `log/slog` | stdlib (Go 1.21+) | Structured logging | All plugin logging — JSON handler to stderr. **D-12 locked decision overrides ecosystem logrus convention.** Zero external deps; Go 1.24 target makes slog available. |
| `encoding/json` | stdlib | Config parsing | Parsing `metric.Provider.Plugin["plugin-name"]` config from AnalysisTemplate and `context.Config` from Rollout step |
| `github.com/stretchr/testify` | v1.9.x | Test assertions | Unit and integration tests |
| `sigs.k8s.io/e2e-framework` | v0.4.x | E2E test framework | Integration tests against kind cluster |
### Infrastructure
| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| kind | latest | Local K8s for e2e tests | Standard for Argo Rollouts plugin testing; lightweight, runs in CI |
| GoReleaser | latest | Cross-compilation + release | Build static binaries for linux/amd64 and linux/arm64, generate SHA256 checksums, publish to GitHub Releases |
| GitHub Actions | N/A | CI/CD | Build, test, release pipeline |
## Plugin Communication Architecture
### How It Works
### Handshake Configuration
- `metricproviders/plugin/client/client.go`: `MagicCookieValue: "metricprovider"`
- `rollout/steps/plugin/client/client.go`: `MagicCookieValue: "step"`
### Plugin Map Keys
- Metric plugin: `"RpcMetricProviderPlugin"`
- Step plugin: `"RpcStepPlugin"`
## Two Binaries, One Module
- Explicit: each binary has one purpose, no runtime dispatch logic
- Matches the convention from all official Argo Rollouts plugin samples
- Separate binary names make ConfigMap entries unambiguous
- GoReleaser builds both from the same module trivially
## Metric Plugin Interface
### Key types:
### Measurement lifecycle:
### Config access pattern:
### main.go entry point (cmd/metric-plugin/main.go):
## Step Plugin Interface
### Key types:
### Step plugin lifecycle (for k6 "fire-and-wait"):
### main.go entry point (cmd/step-plugin/main.go):
## Grafana Cloud k6 REST API v6
### Base URL
### Authentication
- **Personal API token**: user-scoped, generated in Grafana Cloud UI under Testing & Synthetics > Performance > Settings > Access > Personal token
- **Stack API token**: service-account-scoped, not tied to a user
### Client initialization:
### Key API operations for this plugin:
### TestRunApiModel key fields:
### Test run status lifecycle:
### Result values (available after run completes):
| Result | Meaning |
|--------|---------|
| `"passed"` | All k6 thresholds passed |
| `"failed"` | One or more k6 thresholds breached |
| `"error"` | Execution error (script crash, infra issue) |
## Plugin Distribution
### Controller ConfigMap (`argo-rollouts-config`):
### Plugin naming convention:
### AnalysisTemplate referencing the metric plugin:
### Rollout referencing the step plugin:
## Go Module Structure
### go.mod:
## Alternatives Considered
| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| k6 API client | `k6-cloud-openapi-client-go` | Raw `net/http` + manual JSON | Official client is auto-generated from OpenAPI spec, type-safe, handles auth, maintained by Grafana. No reason to DIY |
| Plugin protocol | `net/rpc` (as Argo Rollouts requires) | gRPC | Argo Rollouts uses `net/rpc` exclusively for plugins. `go-plugin` supports gRPC but Argo Rollouts does not use it. Must use `net/rpc` |
| Logging | log/slog (stdlib) | logrus, zerolog | D-12 locked decision: slog has zero external deps, outputs structured JSON to stderr, available since Go 1.21. Ecosystem uses logrus but D-12 explicitly chose slog. |
| Build tool | GoReleaser | Manual `go build` scripts | GoReleaser handles cross-compilation, checksums, GitHub Release uploads in one config file |
| Testing framework | testify + e2e-framework | Go stdlib testing | testify for assertions/mocking, e2e-framework for kind cluster lifecycle. Standard in the Kubernetes ecosystem |
| Binary count | Two binaries (metric + step) | Single binary with env var dispatch | Two binaries is explicit, matches Argo Rollouts conventions. Single binary requires runtime detection via `ARGO_ROLLOUTS_RPC_PLUGIN` env var which is an implementation detail of go-plugin, not a public API |
## What NOT to Use
| Technology | Why Not |
|------------|---------|
| gRPC for plugin communication | Argo Rollouts plugins use `net/rpc`, not gRPC. The `go-plugin` library supports both but Argo Rollouts only implements the `net/rpc` path. Using gRPC will silently fail |
| `k6` Go library directly | `go.k6.io/k6` is the k6 runtime. It does not provide a client for Grafana Cloud k6 API. The OpenAPI client is what you need for triggering cloud runs |
| Cobra/CLI framework | Plugin binary is started by the controller, not by users. No CLI needed. Only the `hashicorp/go-plugin` Serve() entrypoint |
| Kubernetes client-go directly | Plugin runs as a child process of the controller, not as a separate pod. It has no direct K8s API access and doesn't need client-go. The controller passes all needed data via RPC |
| Helm chart | Out of scope per PROJECT.md. The plugin is configured via ConfigMap, not deployed separately |
| Protobuf code generation | `net/rpc` uses `encoding/gob`, not protobuf. The RPC interfaces are already defined in the argo-rollouts module |
## Build & Release
### GoReleaser config (`.goreleaser.yaml`):
### SHA256 checksum:
## Testing Strategy
### Unit tests:
- Mock the Provider interface to test metric plugin logic (Run/Resume/Terminate/GarbageCollect)
- Mock the Provider interface to test step plugin polling and state management (Run/Terminate/Abort)
- Test config parsing from `metric.Provider.Plugin` and `context.Config`
- Test provider state machine (status transitions, result extraction)
### RPC integration tests:
- Use `goPlugin.ServeTestConfig` for in-process plugin testing without starting a real binary
- Verify gob serialization round-trips correctly for all RPC argument types
- Test handshake with mock implementations
### E2E tests (kind cluster):
- Use `sigs.k8s.io/e2e-framework` to manage kind cluster lifecycle
- Install Argo Rollouts CRDs and controller
- Deploy the plugin binaries (file:// path)
- Create AnalysisTemplate + AnalysisRun, verify measurement results
- Create Rollout with step plugin, verify step execution
- Mock the k6 API with a local HTTP server for deterministic tests (no real Grafana Cloud account needed)
### CI pipeline:
## Sources
- [Argo Rollouts Plugin Docs](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: metricproviders/plugin/rpc/rpc.go](https://github.com/argoproj/argo-rollouts/blob/master/metricproviders/plugin/rpc/rpc.go) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: rollout/steps/plugin/rpc/rpc.go](https://github.com/argoproj/argo-rollouts/blob/master/rollout/steps/plugin/rpc/rpc.go) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: utils/plugin/types/types.go](https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types) -- HIGH confidence
- [Argo Rollouts v1.9.0 source: metricproviders/plugin/client/client.go](https://github.com/argoproj/argo-rollouts/blob/master/metricproviders/plugin/client/client.go) -- HIGH confidence (handshake verification)
- [Argo Rollouts v1.9.0 source: rollout/steps/plugin/client/client.go](https://github.com/argoproj/argo-rollouts/blob/master/rollout/steps/plugin/client/client.go) -- HIGH confidence (handshake verification)
- [Argo Rollouts metrics plugin sample (Prometheus)](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus) -- HIGH confidence
- [Grafana k6-cloud-openapi-client-go](https://github.com/grafana/k6-cloud-openapi-client-go) -- HIGH confidence
- [Grafana Cloud k6 REST API docs](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/) -- HIGH confidence
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin) -- HIGH confidence
- [Argo Rollouts Canary Step Plugin docs](https://argoproj.github.io/argo-rollouts/features/canary/plugins/) -- HIGH confidence
- [Argo Rollouts Releases](https://github.com/argoproj/argo-rollouts/releases) -- HIGH confidence
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd:quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd:debug` for investigation and bug fixing
- `/gsd:execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->



<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd:profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
