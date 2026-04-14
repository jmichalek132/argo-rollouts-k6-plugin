# Phase 6: Automated Dependency Management - Context

**Gathered:** 2026-04-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Add a Renovate configuration file (`renovate.json`) to the repository root so Renovate bot automatically creates PRs for Go module and GitHub Actions dependency updates.

</domain>

<decisions>
## Implementation Decisions

### PR Strategy
- **D-01:** Separate PRs per dependency update — easier to review, bisect, and revert individually

### Automerge Policy
- **D-02:** No automerge — all PRs require manual review. Plugin must match Argo Rollouts versions precisely; automatic merges risk silent breakage.

### Claude's Discretion
- Renovate config schema version and preset base
- Schedule (e.g., weekly, monthly, or default)
- Whether to pin GitHub Actions to SHA digests vs version tags

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Dependencies
- `go.mod` — current Go module dependencies and versions
- `.github/workflows/ci.yml` — GitHub Actions used (checkout, setup-go, golangci-lint-action, goreleaser-action)
- `.github/workflows/release.yml` — GitHub Actions used in release workflow
- `.github/workflows/e2e.yml` — GitHub Actions used in e2e workflow

</canonical_refs>

<code_context>
## Existing Code Insights

### Current Dependencies
- Go modules: argo-rollouts, go-plugin, k6-cloud-openapi-client-go, testify, e2e-framework
- GitHub Actions: actions/checkout@v4, actions/setup-go@v5, golangci-lint-action@v7 (ci.yml), goreleaser-action@v7 (release.yml)

### No Existing Renovate Config
- No `renovate.json`, `.renovaterc`, or `.renovaterc.json` exists

</code_context>

<specifics>
## Specific Ideas

No specific requirements — standard Renovate setup for a Go project with GitHub Actions.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 06-automated-dependency-management*
*Context gathered: 2026-04-15*
