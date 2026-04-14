# Phase 5: CI Pipeline Fix - Context

**Gathered:** 2026-04-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix the e2e GitHub Actions workflow (`.github/workflows/e2e.yml`) so it runs successfully on ubuntu-latest runners. This phase changes ONE file — the e2e workflow. No plugin code changes.

</domain>

<decisions>
## Implementation Decisions

### Kind Installation
- **D-01:** Install kind via `go install sigs.k8s.io/kind@v0.27.0` — Go project already has setup-go in the workflow, no extra action dependency, pinned version

### Workflow Invocation
- **D-02:** Replace inline `go test` command with `make test-e2e` — DRY, Makefile already has correct flags (-timeout=15m, -tags=e2e, -count=1), CI and local dev stay in sync automatically

### Claude's Discretion
- Kind version pin: use latest stable (v0.27.0 or verify current latest at implementation time)
- Whether to also add `make build` before `make test-e2e` (currently has inline `CGO_ENABLED=0 make build`)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### CI/CD
- `.github/workflows/e2e.yml` — the file being fixed
- `.github/workflows/ci.yml` — reference for workflow patterns (setup-go, lint, test, build)
- `Makefile` — `test-e2e` target has the correct flags to match

</canonical_refs>

<code_context>
## Existing Code Insights

### Current e2e.yml State
- Triggers on `push: tags: v*` and `workflow_dispatch`
- Uses `actions/checkout@v4` and `actions/setup-go@v5`
- Has inline `CGO_ENABLED=0 make build` then `go test -v -tags=e2e -count=1 ./e2e/...`
- Missing: kind install step, -timeout=15m flag

### Makefile test-e2e Target
- `go test -v -tags=e2e -count=1 -timeout=15m ./e2e/...`
- Also has `test-e2e-live` with `-timeout=30m` for real Grafana Cloud tests

### Integration Points
- e2e tests use `sigs.k8s.io/e2e-framework` which calls `kind` binary from PATH
- Tests cross-compile plugins with GOOS=linux, load into kind via docker cp

</code_context>

<specifics>
## Specific Ideas

No specific requirements — straightforward CI fix with clear decisions above.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 05-ci-pipeline-fix*
*Context gathered: 2026-04-15*
