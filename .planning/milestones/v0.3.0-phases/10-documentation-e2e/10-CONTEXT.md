# Phase 10: Documentation & E2E - Context

**Gathered:** 2026-04-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Users have working RBAC examples, complete AnalysisTemplate/Rollout YAML for the k6-operator provider (both step and metric plugins), and the full k6-operator integration is validated end-to-end on a kind cluster with the real k6-operator controller installed.

</domain>

<decisions>
## Implementation Decisions

### Example YAML Structure
- **D-01:** Claude decides the example directory structure. Recommended approach: mirror `examples/canary-full/` pattern with a new `examples/k6-operator/` directory containing individual YAML files (clusterrole.yaml, analysistemplate.yaml, rollout-step.yaml, rollout-metric.yaml, configmap-script.yaml).
- **D-02:** RBAC ClusterRole covers both TestRun AND PrivateLoadZone CRDs — users don't need to modify RBAC when switching between in-cluster and cloud-connected modes. Also includes pods, pods/log, and configmaps permissions.
- **D-03:** Both step plugin and metric plugin examples included. Step plugin Rollout (trigger k6 run as canary step) and metric plugin AnalysisTemplate (poll k6 metrics with successCondition) show both integration patterns.

### k6 Test Script
- **D-04:** Single simple k6 script with handleSummary export. HTTP GET against a target, thresholds (p95<500, error rate<1%), and `handleSummary()` writing JSON to stdout. Minimal, copy-paste ready. No advanced multi-scenario variant.

### e2e Test Strategy
- **D-05:** Install the real k6-operator controller in kind (CRDs + controller deployment). Create a TestRun CR, let the controller create runner pods, wait for completion. Most realistic approach.
- **D-06:** Claude decides e2e test depth. Recommended: full path validation (TestRun → runner pods → pod logs → handleSummary JSON → RunResult → metric values) since this is the milestone's capstone validation.

### README
- **D-07:** Keep documentation in `examples/k6-operator/` only — the directory gets its own README.md with setup instructions. Main project README links to it but does not duplicate content.

### Claude's Discretion
- e2e test framework setup details (how to install k6-operator in kind, version pinning)
- ConfigMap creation for k6 script delivery in e2e tests
- CI timeout adjustments if needed for k6-operator e2e
- Whether to validate metric values in e2e or just verify non-zero population
- Mock target service reuse vs new deployment for k6-operator tests
- Example YAML annotations and comments style

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Existing Examples (pattern to follow)
- `examples/canary-full/` — Grafana Cloud example directory structure (analysistemplate.yaml, configmap-snippet.yaml, rollout.yaml, secret.yaml)
- `examples/threshold-gate/` — Simpler example pattern
- `examples/k6-plugin-demo.js` — Existing k6 script (no handleSummary — Phase 10 creates the k6-operator variant)

### e2e Test Framework
- `e2e/main_test.go` — Test environment setup (kind cluster, plugin deploy, mock server)
- `e2e/metric_plugin_test.go` — Existing metric plugin e2e test pattern
- `e2e/step_plugin_test.go` — Existing step plugin e2e test pattern
- `e2e/testdata/` — Existing test YAML (analysistemplate-thresholds.yaml, argo-rollouts-config.yaml, rollout-step.yaml)
- `e2e/mock/main.go` — Mock HTTP server for k6 to test against

### Provider Implementation (what e2e validates)
- `internal/provider/operator/operator.go` — K6OperatorProvider.GetRunResult (creates TestRun, polls status, extracts metrics)
- `internal/provider/operator/summary.go` — handleSummary JSON parsing from pod logs
- `internal/provider/operator/exitcode.go` — Runner pod exit code checking
- `internal/provider/provider.go` — RunResult struct, Provider interface

### Requirements
- `.planning/REQUIREMENTS.md` — DOCS-01, DOCS-02, TEST-01

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `e2e/main_test.go` — Full kind cluster lifecycle with `sigs.k8s.io/e2e-framework`, plugin binary deployment, namespace management
- `e2e/mock/main.go` — HTTP mock server already deployed in kind for k6 to target
- `examples/canary-full/` — Template for example directory structure
- `examples/k6-plugin-demo.js` — Base k6 script pattern (needs handleSummary addition)

### Established Patterns
- e2e tests use `//go:build e2e` build tag
- kind cluster named with `envconf.RandomName`
- Plugin binaries built and loaded into kind via `buildDeployPluginsAndMock`
- Test YAML applied via `envfuncs.CreateNamespace` and kubectl
- `argo-rollouts-config.yaml` in testdata configures plugin registration

### Integration Points
- `e2e/testdata/argo-rollouts-config.yaml` — needs k6-operator plugin entries added
- k6-operator CRDs and controller need to be installed in the kind cluster before tests
- ConfigMap with k6 script needs to be created in the test namespace

</code_context>

<specifics>
## Specific Ideas

- The k6 script must include `export function handleSummary(data) { return { stdout: JSON.stringify(data) } }` per D-01 from Phase 9
- RBAC must cover `k6.io` API group for TestRun and PrivateLoadZone resources, plus core API for pods, pods/log, and configmaps
- k6-operator installation in kind: use official manifest from k6-operator releases (CRDs + controller)
- The mock target service from e2e/mock should be reusable — k6 scripts just need to point to its ClusterIP

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 10-documentation-e2e*
*Context gathered: 2026-04-16*
