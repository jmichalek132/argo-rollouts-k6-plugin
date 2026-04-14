# Phase 4: Release & Examples — Context

**Gathered:** 2026-04-10
**Status:** Ready for planning

<domain>
## Phase Boundary

Make the project ready for community consumption: e2e tests on a kind cluster validating the full binary-loading path, goreleaser configuration producing multi-arch static binaries with checksums, GitHub Actions CI, example manifests demonstrating the three key patterns, a self-contained README, and a CONTRIBUTING.md that enables future provider authors.

Phase 3's plugin implementations are complete and not modified in this phase. This phase does NOT add new plugin behavior or modify `internal/`.

</domain>

<decisions>
## Implementation Decisions

### e2e test strategy
- **D-01:** Mock the Grafana Cloud k6 API using a lightweight in-process `net/http` mock server. The server handles all three API endpoints: `POST /loadtests/v2/tests/{id}/start-testrun` (TriggerRun), `GET /loadtests/v2/test_runs/{id}` (GetRunResult), `POST /loadtests/v2/test_runs/{id}/stop` (StopRun). No real credentials needed in CI.
- **D-02:** e2e tests use the full binary path: kind cluster loads the real compiled plugin binaries via `file://` path from an Argo Rollouts `argo-rollouts-config` ConfigMap. Controller communicates with the plugin over RPC (not in-process). This validates binary distribution, go-plugin handshake, and gob serialization end-to-end.
- **D-03:** Use `sigs.k8s.io/e2e-framework` for kind cluster lifecycle management (already identified in REQUIREMENTS.md). Tests must be under `e2e/` or `test/e2e/` directory.
- **D-04:** Minimum e2e scenarios to cover:
  - Metric plugin: AnalysisRun with `metric: thresholds` — mock returns Passed → AnalysisRun succeeds
  - Metric plugin: AnalysisRun with `metric: thresholds` — mock returns Failed → AnalysisRun fails
  - Step plugin: Rollout step trigger → poll → Passed → Rollout advances
  - Step plugin: Rollout step trigger → poll → Failed → Rollout rolls back

### GoReleaser targets
- **D-05:** Build 4 platforms: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`. Both binaries (`metric-plugin`, `step-plugin`) for each platform.
- **D-06:** Asset naming: flat filenames — `metric-plugin_linux_amd64`, `step-plugin_linux_arm64`, etc. No archive wrapping; raw binaries so users can set a direct URL in ConfigMap. goreleaser `binary` naming convention.
- **D-07:** goreleaser generates SHA256 checksums file automatically (`checksums.txt`). LDFLAGS: `-s -w -X main.version={{.Version}}` (Makefile already has `-s -w`; add version injection).
- **D-08:** CGO_ENABLED=0 for all builds (already established in Phase 1).

### CI/CD pipeline
- **D-09:** GitHub Actions CI runs on PR and main push: lint (`golangci-lint`), test (`go test -race ./...`), and build-check (`CGO_ENABLED=0 go build`). No release artifacts on non-tag runs.
- **D-10:** GitHub Actions release runs on tag push matching `v*`: runs goreleaser, publishes to GitHub Releases. Uses `GITHUB_TOKEN` (auto-provided by Actions) + no additional secrets needed for the release itself.
- **D-11:** e2e tests run in CI only on tag push (to avoid kind cluster overhead on every PR). Or gate behind a manual workflow dispatch. Not on every PR — kind setup is slow.

### Example manifests
- **D-12:** Fully working examples with placeholder credentials. Each example includes: the AnalysisTemplate (or Rollout), the Secret YAML with `<YOUR_API_TOKEN>` and `<YOUR_STACK_ID>` placeholders, and the argo-rollouts-config ConfigMap snippet for that plugin type. User can `kubectl apply` after substituting placeholders.
- **D-13:** Three examples in `examples/` with subdirectories:
  - `examples/threshold-gate/` — AnalysisTemplate with `metric: thresholds` only (EXAM-01: simplest pattern)
  - `examples/error-rate-latency/` — AnalysisTemplate combining `http_req_failed` + `http_req_duration/p95` (EXAM-02)
  - `examples/canary-full/` — Complete Rollout with step plugin (trigger) + metric plugin analysis (gate) showing the trigger→coordinate workflow (EXAM-03)
- **D-14:** Plugin names in example YAML: `jmichalek132/k6` for metric plugin, `jmichalek132/k6-step` for step plugin (established in prior phases).

### Documentation
- **D-15:** README is self-contained — no wiki, no separate docs site. Sections: project badge/description, how it works (2-3 sentences), installation (ConfigMap YAML + binary download with checksum verification), credential setup (Secret YAML), quick-start (minimal copy-paste AnalysisTemplate), link to `examples/` for full patterns. Roughly one screenful before the fold.
- **D-16:** CONTRIBUTING.md covers: dev setup (build, test, lint commands — mirrors Makefile), provider interface guide (what `TriggerRun`/`GetRunResult`/`StopRun` must do, error conventions, the PluginConfig flow), and how to wire a new provider into the binaries. This document enables Phase 2-style work on future providers (KubernetesJobProvider, LocalBinaryProvider) without requiring the original author.

### Claude's Discretion
- Exact kind version to pin in CI
- Whether to use `act` for local CI testing setup in CONTRIBUTING.md
- goreleaser `before.hooks` (go mod tidy, generate) if needed
- e2e test helper utilities (retry logic, wait conditions for AnalysisRun readiness)
- Whether to add a `SECURITY.md` stub

</decisions>

<specifics>
## Specific Ideas

- ConfigMap example should show actual download URLs pointing to GitHub Releases (with placeholder version: `v0.1.0`), not placeholder paths
- The `canary-full` example should show the `testRunId` handoff pattern: step plugin returns `testRunId` in Status, metric plugin consumes it via AnalysisTemplate args referencing `{{steps.k6-load-test.outputs.testRunId}}`
- e2e mock server should be configurable per test — allow tests to program the mock to return specific states (Running first, then Passed or Failed)

</specifics>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project planning
- `.planning/PROJECT.md` — Project goals, constraints, community adoption goals
- `.planning/REQUIREMENTS.md` — Phase 4 requirements: PLUG-03, DIST-02, DIST-03, EXAM-01 through EXAM-05, TEST-02

### Prior phase artifacts
- `.planning/phases/01-foundation-provider/01-CONTEXT.md` — Locked decisions: CGO_ENABLED=0, slog to stderr, golangci-lint v2, forbidigo
- `.planning/phases/02-metric-plugin/02-CONTEXT.md` — Metric plugin behavior, PluginConfig field names used in example YAML
- `.planning/phases/03-step-plugin/03-CONTEXT.md` — Step plugin behavior, Status keys (runId, testRunURL, finalStatus), timeout config
- `internal/provider/config.go` — PluginConfig struct: field names for example YAML (`testId`, `apiToken`, `stackId`, `timeout`, `metric`, `aggregation`)
- `Makefile` — Existing build/test/lint targets that CI must mirror
- `.golangci.yml` — Linter config that CI must use

### Argo Rollouts plugin distribution
- `https://github.com/argoproj/argo-rollouts/blob/v1.9.0/rollout/steps/plugin/rpc/rpc.go` — RpcStep interface (step plugin binary)
- `https://github.com/argoproj/argo-rollouts/tree/master/test/cmd/step-plugin-sample` — Reference step plugin with e2e test example

### GoReleaser reference
- `https://goreleaser.com/customization/builds/` — Build configuration for multiple binaries + CGO_ENABLED=0
- `https://goreleaser.com/customization/checksum/` — Checksum generation

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `Makefile` — `build`, `test`, `lint`, `lint-stdout` targets; CI workflow should call these directly rather than duplicating commands
- `internal/provider/config.go` — PluginConfig struct fields are the exact keys to use in example YAML plugin config blocks
- `cmd/metric-plugin/main.go` + `cmd/step-plugin/main.go` — Fully wired; goreleaser builds both binaries from these entry points

### Established Patterns
- `CGO_ENABLED=0` for all binary builds (non-negotiable — static linking required)
- `GOPATH="$HOME/go"` may be needed in CI (observed in test validation steps)
- `slog` JSON handler to stderr — do NOT add any stdout writes in e2e test helpers
- Plugin name constants: `"jmichalek132/k6"` and `"jmichalek132/k6-step"` — these appear in ConfigMap entries

### Integration Points
- goreleaser requires `.goreleaser.yaml` at repo root with two `builds` entries (one per binary)
- GitHub Actions requires `.github/workflows/` directory (does not exist yet)
- e2e tests go in a new top-level directory (`e2e/` or `test/e2e/`) — no existing test infrastructure there
- `examples/` directory does not exist yet — create with three subdirectories

</code_context>

<deferred>
## Deferred Ideas

- `SECURITY.md` — If needed, add a stub; not a blocker for v1 community release
- Nightly/snapshot builds from `main` — deferred; tag-only release sufficient for v1
- GitHub Container Registry (ghcr.io) image publishing — controllers can pull the binary via initContainer from a container image; deferred to v2 distribution model
- wiki or docs site — deferred; README + examples/ is sufficient for v1

</deferred>

---

*Phase: 04-release-examples*
*Context gathered: 2026-04-10*
