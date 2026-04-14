---
phase: 04-release-examples
plan: 01
subsystem: infra
tags: [goreleaser, github-actions, ci-cd, multi-arch, sha256]

# Dependency graph
requires:
  - phase: 01-foundation-provider
    provides: "Binary entrypoints (cmd/metric-plugin, cmd/step-plugin), Makefile build targets"
  - phase: 02-metric-plugin
    provides: "Metric plugin implementation compiled by goreleaser"
  - phase: 03-step-plugin
    provides: "Step plugin implementation compiled by goreleaser"
provides:
  - "GoReleaser v2 config producing 8 static binaries (2 plugins x 4 platforms) with SHA256 checksums"
  - "CI workflow (lint + test + build) on every PR and main push"
  - "Release workflow producing GitHub Releases on v* tag push"
  - "e2e workflow running kind-based tests on v* tag push and manual dispatch"
  - "Version variable in both binaries for LDFLAGS injection"
affects: [04-02, 04-03, 04-04]

# Tech tracking
tech-stack:
  added: [goreleaser-v2, github-actions, golangci-lint-action-v9, goreleaser-action-v7]
  patterns: [multi-arch-static-build, tag-triggered-release, parallel-ci-jobs]

key-files:
  created:
    - .goreleaser.yaml
    - .github/workflows/ci.yml
    - .github/workflows/release.yml
    - .github/workflows/e2e.yml
  modified:
    - cmd/metric-plugin/main.go
    - cmd/step-plugin/main.go

key-decisions:
  - "GoReleaser v2 with format: binary (flat naming, no archive wrapping) per D-06"
  - "CI uses golangci-lint-action@v9 with v2.1.6 to match .golangci.yml v2 config"
  - "e2e workflow on tag push and workflow_dispatch only (not PR) per D-11"
  - "fetch-depth: 0 in release workflow for goreleaser version detection"

patterns-established:
  - "Version injection: var version = dev in main.go + LDFLAGS -X main.version at build time"
  - "CI mirrors Makefile targets: make lint, make test, make build"
  - "Separate workflows per concern: ci.yml, release.yml, e2e.yml"

requirements-completed: [DIST-02, DIST-03]

# Metrics
duration: 2min
completed: 2026-04-10
---

# Phase 4 Plan 01: GoReleaser & CI/CD Summary

**GoReleaser v2 multi-arch config (8 binaries, SHA256 checksums) with CI/release/e2e GitHub Actions workflows**

## Performance

- **Duration:** 2 min
- **Started:** 2026-04-10T11:09:11Z
- **Completed:** 2026-04-10T11:10:52Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments
- Added `var version = "dev"` to both plugin binaries enabling LDFLAGS version injection at build time
- Created GoReleaser v2 config targeting 4 platforms (linux/darwin x amd64/arm64) for both binaries with flat naming and SHA256 checksums
- Created three GitHub Actions workflows: CI (lint+test+build on PR/push), Release (goreleaser on v* tag), e2e (kind tests on tag/manual dispatch)

## Task Commits

Each task was committed atomically:

1. **Task 1: Add version variable and GoReleaser config** - `2a1584d` (feat)
2. **Task 2: Create GitHub Actions CI, Release, and e2e workflows** - `239322b` (feat)

## Files Created/Modified
- `cmd/metric-plugin/main.go` - Added var version = "dev" for LDFLAGS injection
- `cmd/step-plugin/main.go` - Added var version = "dev" for LDFLAGS injection
- `.goreleaser.yaml` - GoReleaser v2 config: 2 builds x 4 platforms, binary format, SHA256
- `.github/workflows/ci.yml` - CI pipeline: lint (golangci-lint v2.1.6), test, build
- `.github/workflows/release.yml` - Release pipeline: goreleaser on v* tag with GITHUB_TOKEN
- `.github/workflows/e2e.yml` - e2e pipeline: kind tests on v* tag and workflow_dispatch

## Decisions Made
- Used golangci-lint-action@v9 with explicit version v2.1.6 to match the project's .golangci.yml v2 format
- Release workflow uses fetch-depth: 0 to ensure goreleaser can determine version from git tags
- e2e workflow deliberately excluded from PR triggers per D-11 (kind cluster overhead too high)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Release infrastructure complete: goreleaser config, CI/CD workflows all in place
- Ready for 04-02 (e2e tests), 04-03 (example manifests), 04-04 (README/CONTRIBUTING)
- When v* tag is pushed, release workflow will produce GitHub Release with 8 binaries + checksums

## Self-Check: PASSED

All 7 files verified present. Both task commits (2a1584d, 239322b) found in git log.

---
*Phase: 04-release-examples*
*Completed: 2026-04-10*
