---
phase: 260420-hal-bump-golangci-lint-action-version-in-ci-
plan: 01
subsystem: ci
tags: [ci, tooling, golangci-lint, go1.25]
requires: []
provides:
  - "CI lint job compatible with go.mod go 1.25.0 directive"
affects:
  - .github/workflows/ci.yml
tech-stack:
  added: []
  patterns: []
key-files:
  created: []
  modified:
    - .github/workflows/ci.yml
decisions:
  - "Pin golangci-lint CI action to v2.11.4 (built on Go 1.25.0) to match go.mod go 1.25.0; no other files touched — Makefile intentionally uses whatever golangci-lint is on the developer's PATH"
metrics:
  duration_seconds: ~60
  tasks_completed: 2
  files_changed: 1
  lines_changed: 2
  completed_date: "2026-04-20"
---

# Quick Task 260420-hal Plan 01: Bump golangci-lint-action version in CI Summary

**One-liner:** Bumped `golangci/golangci-lint-action@v9` `version:` input from `v2.1.6` (Go 1.24) to `v2.11.4` (Go 1.25.0) so the CI lint job stops panicking on Go 1.25 packages declared by `go.mod`.

## What Changed

- `.github/workflows/ci.yml` lint job step: `version: v2.1.6` → `version: v2.11.4`

That is the entire change. One file, one line.

## Why

`go.mod` declares `go 1.25.0`. golangci-lint v2.1.6 was built with Go 1.24 and fails to load Go 1.25 packages with:

```
package requires newer Go version go1.25 (application built with go1.24)
```

golangci-lint v2.11.4 is built on Go 1.25.0 (verified upstream against `/repos/golangci/golangci-lint/contents/go.mod?ref=v2.11.4`), so it can load the module's packages.

## Tasks

| Task | Name                                                          | Commit   | Files                       |
| ---- | ------------------------------------------------------------- | -------- | --------------------------- |
| 1    | Bump golangci-lint version pin in CI workflow                 | d6a7945  | .github/workflows/ci.yml    |
| 2    | Run local `make lint` to confirm no regressions before push   | (no-op)  | —                           |

Task 2 has no file changes — it is a verification gate per the user's standing feedback ("Run linter before pushing"). No commit was produced.

## Verification Evidence

**Automated verify for Task 1:**
```
$ grep -n 'version: v2\.11\.4' .github/workflows/ci.yml && ! grep -n 'v2\.1\.6' .github/workflows/ci.yml && echo "CI VERSION PIN UPDATED"
21:          version: v2.11.4
CI VERSION PIN UPDATED
```

**Local `make lint` output (Task 2):**
```
$ golangci-lint --version
golangci-lint has version 2.11.4 built with go1.26.2 ...

$ make lint
Checking for stdout usage in non-test code...
No stdout usage found -- OK
golangci-lint run
0 issues.
```

Local installed binary is now v2.11.4 (via `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4`), matching the pin CI will now use. Running against `go.mod go 1.25.0`: clean, 0 issues. `lint-stdout` gate also green.

**Scope check (phase-level):**
```
$ git diff HEAD~1 HEAD --stat
 .github/workflows/ci.yml | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)
```

One file, two lines (one added, one removed = one logical change). No drift in `.golangci.yml`, `Makefile`, `go.mod`, `go.sum`, `CONTRIBUTING.md`, or `renovate.json`.

## Success Criteria — all met

- [x] CI lint job will no longer panic on Go 1.25 packages (runs golangci-lint v2.11.4 built on Go 1.25.0 against `go.mod go 1.25.0`).
- [x] Local `make lint` exits 0.
- [x] Exactly one file changed (`.github/workflows/ci.yml`), exactly one line changed (`v2.1.6` → `v2.11.4`).
- [x] No drift introduced in `.golangci.yml`, `Makefile`, `go.mod`, or documentation.

## Must-haves — all met

- [x] CI lint job uses golangci-lint built on Go 1.25.0 (matching `go.mod go 1.25.0`)
- [x] The `version:` input to `golangci/golangci-lint-action@v9` is `v2.11.4`
- [x] Local `make lint` still exits 0 (no config drift introduced by the bump)
- [x] `.github/workflows/ci.yml` contains `version: v2.11.4`

## Deviations from Plan

None — plan executed exactly as written.

## Deferred Issues

None.

## Self-Check: PASSED

- Created files: none expected.
- Modified files:
  - `.github/workflows/ci.yml` — FOUND, contains `version: v2.11.4` at line 21, no `v2.1.6` references remain.
- Commits:
  - `d6a7945` — FOUND in `git log` (ci(260420-hal-01): bump golangci-lint pin from v2.1.6 to v2.11.4).
