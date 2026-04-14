# Phase 5: CI Pipeline Fix - Research

**Researched:** 2026-04-15
**Domain:** GitHub Actions CI workflow for e2e tests (kind + Docker + Go)
**Confidence:** HIGH

## Summary

Phase 5 fixes two issues in `.github/workflows/e2e.yml`: (1) the `kind` binary is not installed before e2e tests, and (2) the `go test` invocation is missing `-timeout=15m`. Both decisions (D-01, D-02) from CONTEXT.md are straightforward, but research uncovered a **critical blocker**: the Makefile's `test-e2e` target unconditionally sets `DOCKER_HOST` to a macOS Colima socket path that does not exist on GitHub Actions ubuntu-latest runners. Using `make test-e2e` as-is (D-02) will cause Docker/kind operations to fail in CI.

The Makefile must be patched to make `DOCKER_HOST` conditional before `make test-e2e` can work in CI. This is a small change (one line) but essential for D-02 to succeed.

**Primary recommendation:** Fix the Makefile's DOCKER_HOST to use conditional assignment (`?=`), install kind v0.31.0 via `go install`, then replace the inline `go test` with `make test-e2e` in the workflow.

<user_constraints>

## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Install kind via `go install sigs.k8s.io/kind@v0.27.0` -- Go project already has setup-go in the workflow, no extra action dependency, pinned version
- **D-02:** Replace inline `go test` command with `make test-e2e` -- DRY, Makefile already has correct flags (-timeout=15m, -tags=e2e, -count=1), CI and local dev stay in sync automatically

### Claude's Discretion
- Kind version pin: use latest stable (v0.27.0 or verify current latest at implementation time)
- Whether to also add `make build` before `make test-e2e` (currently has inline `CGO_ENABLED=0 make build`)

### Deferred Ideas (OUT OF SCOPE)
None.

</user_constraints>

<phase_requirements>

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CI-01 | e2e GitHub Actions workflow installs `kind` binary before running tests | D-01: `go install sigs.k8s.io/kind@v0.31.0` adds kind to `$GOPATH/bin` which is on PATH after `actions/setup-go@v5`. Verified: kind v0.31.0 is latest stable, compatible with Go 1.24. |
| CI-02 | e2e GitHub Actions workflow uses `-timeout=15m` matching Makefile's `test-e2e` target | D-02: `make test-e2e` already includes `-timeout=15m`. Requires Makefile DOCKER_HOST fix first (see Pitfall 1). |

</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| kind binary availability | CI Runner (GitHub Actions) | -- | kind must be installed on the runner before any test step |
| e2e test execution | CI Runner (GitHub Actions) | Makefile (build orchestration) | Workflow delegates to `make test-e2e` which wraps `go test` with correct flags |
| Docker socket access | CI Runner (GitHub Actions) | Makefile (env config) | Runner provides Docker; Makefile must not override with a platform-specific socket path |

## Standard Stack

### Core (already in place)

| Tool | Version | Purpose | Why Standard |
|------|---------|---------|--------------|
| `actions/checkout` | v4 | Checkout repo | Standard GitHub Actions checkout [VERIFIED: existing ci.yml] |
| `actions/setup-go` | v5 | Install Go from go.mod | Standard Go CI setup, adds `$GOPATH/bin` to PATH [VERIFIED: existing ci.yml] |
| `kind` | v0.31.0 | Create ephemeral K8s cluster for e2e | Latest stable release (2024-12-18), defaults to Kubernetes 1.35.0. Compatible with Go 1.24. [VERIFIED: github.com/kubernetes-sigs/kind/releases] |
| Docker | pre-installed | Container runtime for kind | Docker is pre-installed on `ubuntu-latest` runners at `/var/run/docker.sock` [VERIFIED: GitHub docs] |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `go install sigs.k8s.io/kind@v0.31.0` | `helm/kind-action@v1` | GitHub Action installs kind + creates cluster in one step, but our e2e-framework manages cluster lifecycle itself (envfuncs.CreateCluster). Using kind-action would duplicate cluster creation. `go install` is simpler and matches D-01. |
| `go install` kind | Download binary from GitHub releases | More portable (no Go required), but Go is already set up. `go install` is one line, no curl/checksum dance. |
| kind v0.31.0 | kind v0.27.0 (D-01 original) | v0.27.0 is 10 months older (2024-02-15). v0.31.0 has newer Kubernetes defaults and bug fixes. CONTEXT.md gives discretion on version. |

## Architecture Patterns

### System Architecture Diagram

```
GitHub Actions Runner (ubuntu-latest)
    |
    v
[actions/checkout@v4] --> [actions/setup-go@v5] --> [go install kind] --> [make build] --> [make test-e2e]
                                   |                        |                  |                  |
                                   v                        v                  v                  v
                            Go 1.24 + GOPATH/bin      kind binary         bin/metric-plugin   go test -v -tags=e2e
                               on PATH                 on PATH            bin/step-plugin     -count=1 -timeout=15m
                                                                                                   |
                                                                                                   v
                                                                                          e2e-framework TestMain
                                                                                                   |
                                                                                        +----------+----------+
                                                                                        |                     |
                                                                                 kind create cluster    kind delete cluster
                                                                                        |
                                                                                 docker build image
                                                                                 kind load docker-image
                                                                                 kubectl apply manifests
                                                                                 run tests against cluster
```

### Pattern 1: Go Install for CI Tool Dependencies

**What:** Use `go install` to install Go-based CLI tools needed in CI.
**When to use:** When `actions/setup-go` is already in the workflow and the tool is a Go module.
**Example:**
```yaml
# Source: kind quick-start docs (kind.sigs.k8s.io/docs/user/quick-start/)
- name: Install kind
  run: go install sigs.k8s.io/kind@v0.31.0
```

After `actions/setup-go@v5`, `$GOPATH/bin` is on `$PATH`, so the `kind` binary is immediately available. [VERIFIED: actions/setup-go adds GOPATH/bin to PATH]

### Pattern 2: Makefile as Single Source of Truth for Test Commands

**What:** CI workflows invoke `make <target>` instead of inline commands.
**When to use:** When the Makefile already defines the command with correct flags.
**Example:**
```yaml
# Before (duplicated flags, drift risk):
- run: go test -v -tags=e2e -count=1 ./e2e/...

# After (single source of truth):
- run: make test-e2e
```

### Anti-Patterns to Avoid
- **Hardcoded platform-specific paths in Makefile:** Setting `DOCKER_HOST` to a macOS-specific Colima socket path breaks CI on Linux runners. Use conditional assignment or leave it to the user's shell environment.
- **Duplicating Makefile flags in workflow YAML:** Leads to drift (exactly what happened -- Makefile has `-timeout=15m` but workflow does not).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| kind installation | curl + chmod + move binary | `go install sigs.k8s.io/kind@v0.31.0` | Go already set up, one line, version pinned |
| Test flag management | Inline `go test` in YAML | `make test-e2e` | DRY, single source of truth for flags |
| Cluster lifecycle | Manual kind create/delete in workflow | `sigs.k8s.io/e2e-framework` (already in code) | Framework handles create, namespace, cleanup in TestMain |

## Common Pitfalls

### Pitfall 1: DOCKER_HOST Colima Override Breaks CI (CRITICAL)

**What goes wrong:** `make test-e2e` unconditionally sets `DOCKER_HOST="unix://$(HOME)/.colima/default/docker.sock"` in the recipe. On GitHub Actions ubuntu-latest, this path does not exist. All Docker and kind commands fail with "Cannot connect to the Docker daemon."
**Why it happens:** The Makefile was written for macOS local development using Colima. The inline env var in the recipe overrides any existing `DOCKER_HOST` in the environment.
**How to avoid:** Change the Makefile to use conditional assignment at the top level:
```makefile
# Default to Colima socket for local macOS dev; CI and standard Docker installs
# leave DOCKER_HOST unset, which tells Docker to use /var/run/docker.sock.
DOCKER_HOST ?= unix://$(HOME)/.colima/default/docker.sock

test-e2e:
	GOPATH="$(HOME)/go" DOCKER_HOST="$(DOCKER_HOST)" \
	go test -v -tags=e2e -count=1 -timeout=15m ./e2e/...
```
Then in the CI workflow, set `DOCKER_HOST` to empty or the standard path:
```yaml
env:
  DOCKER_HOST: unix:///var/run/docker.sock
```
Or simply unset it in CI -- Docker on ubuntu-latest uses the default socket when DOCKER_HOST is empty. [VERIFIED: Makefile inline env vars unconditionally override shell env; tested locally]

**Warning signs:** `make test-e2e` works locally (macOS/Colima) but fails in CI with Docker connection errors.

### Pitfall 2: Missing kind in PATH

**What goes wrong:** e2e-framework calls `kind` binary via `exec.Command("kind", ...)` (in the `sigs.k8s.io/e2e-framework/support/kind` package). If kind is not installed, tests fail with "exec: kind: executable file not found in $PATH."
**Why it happens:** The current e2e.yml has no kind installation step.
**How to avoid:** Add `go install sigs.k8s.io/kind@v0.31.0` step before test execution.
**Warning signs:** Test failure mentioning "kind" not found, cluster creation failure.

### Pitfall 3: Test Timeout Default (10 minutes)

**What goes wrong:** Go's default test timeout is 10 minutes. E2e tests that create a kind cluster, install Argo Rollouts, build/load images, and run tests can easily exceed this.
**Why it happens:** The workflow uses bare `go test` without `-timeout=15m`.
**How to avoid:** Use `make test-e2e` which includes `-timeout=15m`. [VERIFIED: Makefile line 20]

### Pitfall 4: Build Step Ordering

**What goes wrong:** The current workflow has `CGO_ENABLED=0 make build` before `go test`. The e2e tests in `main_test.go` actually compile binaries themselves (lines 107-123), so the pre-built binaries in `bin/` are not directly used by the e2e tests. However, `make build` serves as a fast-fail gate -- if the code doesn't compile, there's no point running expensive e2e tests.
**Why it happens:** Build errors waste CI minutes if caught only during e2e test setup.
**How to avoid:** Keep `make build` as a fast-fail step before `make test-e2e`. CONTEXT.md gives discretion on whether to change the inline `CGO_ENABLED=0 make build` to just `make build` (the Makefile already sets `CGO_ENABLED=0`). [VERIFIED: Makefile build targets already include CGO_ENABLED=0]

## Code Examples

### Fixed e2e.yml (target state)

```yaml
# Source: Synthesis of existing ci.yml patterns + research findings
name: e2e

on:
  push:
    tags:
      - "v*"
  workflow_dispatch:

permissions:
  contents: read

env:
  DOCKER_HOST: unix:///var/run/docker.sock

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install kind
        run: go install sigs.k8s.io/kind@v0.31.0
      - name: Build plugin binaries
        run: make build
      - name: Run e2e tests
        run: make test-e2e
```

### Fixed Makefile test-e2e target

```makefile
# Source: Research finding -- conditional DOCKER_HOST for cross-platform compatibility
DOCKER_HOST ?= unix://$(HOME)/.colima/default/docker.sock

test-e2e:
	GOPATH="$(HOME)/go" DOCKER_HOST="$(DOCKER_HOST)" \
	go test -v -tags=e2e -count=1 -timeout=15m ./e2e/...
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| kind v0.27.0 | kind v0.31.0 | 2024-12-18 | Newer Kubernetes defaults (1.35.0), cgroup v2 only, bug fixes |
| Inline `go test` in CI | `make <target>` delegation | Industry standard | Single source of truth for test flags |
| Hardcoded DOCKER_HOST | Conditional / environment-driven | Best practice | Cross-platform Makefile compatibility |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `actions/setup-go@v5` adds `$GOPATH/bin` to PATH | Pattern 1 | kind binary not found after `go install`; fix: add explicit PATH export |
| A2 | Docker is pre-installed on ubuntu-latest | Standard Stack | kind cannot create cluster; fix: add Docker setup step |
| A3 | e2e tests complete within 15 minutes on GitHub Actions | Pitfall 3 | Timeout; fix: increase to 20m or 30m |

**A1 and A2 are high-confidence assumptions** -- they are standard GitHub Actions behavior documented extensively. A3 depends on runner performance but 15 minutes matches the Makefile target and is a reasonable upper bound for kind cluster lifecycle + mock-based tests.

## Open Questions

1. **Should DOCKER_HOST be removed entirely from the Makefile or made conditional?**
   - What we know: Removing it entirely means Colima users must export DOCKER_HOST in their shell. Conditional (`?=`) preserves the current local-dev experience.
   - Recommendation: Use `?=` conditional assignment -- least disruptive to existing local workflow.

2. **Should kind version be v0.27.0 (D-01) or v0.31.0 (latest)?**
   - What we know: D-01 says v0.27.0, but CONTEXT.md grants discretion to use "latest stable (v0.27.0 or verify current latest at implementation time)". Latest is v0.31.0.
   - Recommendation: Use v0.31.0. It's 10 months newer with bug fixes and better Kubernetes support.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go testing + sigs.k8s.io/e2e-framework v0.6.0 |
| Config file | go.mod (build tags: `e2e`) |
| Quick run command | `make test` (unit tests only, ~10s) |
| Full suite command | `make test-e2e` (requires kind + Docker, ~10min) |

### Phase Requirements -> Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CI-01 | kind binary available in CI | workflow validation | Push tag or `workflow_dispatch` trigger | N/A (workflow file, not code test) |
| CI-02 | `-timeout=15m` applied to e2e tests | workflow validation | `grep -q 'timeout=15m' Makefile` (sanity check) | N/A (workflow file) |

### Sampling Rate
- **Per task commit:** `make test` (unit tests, fast-fail sanity check)
- **Per wave merge:** `workflow_dispatch` trigger of e2e workflow (manual validation)
- **Phase gate:** e2e workflow completes green on a push or manual trigger

### Wave 0 Gaps
None -- this phase modifies CI configuration files (`.github/workflows/e2e.yml` and `Makefile`), not application code. No new test files needed. Validation is the CI workflow itself running green.

## Security Domain

Not applicable. This phase modifies CI workflow configuration only -- no authentication, secrets, input validation, or cryptographic operations are introduced or changed. The existing e2e.yml already has `permissions: contents: read` (principle of least privilege). No ASVS categories apply.

## Sources

### Primary (HIGH confidence)
- [kind releases](https://github.com/kubernetes-sigs/kind/releases) - v0.31.0 verified as latest (2024-12-18)
- [kind quick-start](https://kind.sigs.k8s.io/docs/user/quick-start/) - `go install` installation method
- Existing codebase: `.github/workflows/e2e.yml`, `.github/workflows/ci.yml`, `Makefile`, `e2e/main_test.go` - verified current state
- Local testing: Makefile inline env var override behavior confirmed empirically

### Secondary (MEDIUM confidence)
- [GitHub Actions ubuntu-latest runner docs](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) - Docker pre-installed, default socket at `/var/run/docker.sock`

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - all tools already in use or well-documented
- Architecture: HIGH - single-file change with one supporting Makefile fix
- Pitfalls: HIGH - DOCKER_HOST issue verified empirically via local testing

**Research date:** 2026-04-15
**Valid until:** 2026-05-15 (stable CI tooling, kind releases infrequent)
