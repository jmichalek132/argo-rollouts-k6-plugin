---
phase: 260420-hal-bump-golangci-lint-action-version-in-ci-
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - .github/workflows/ci.yml
autonomous: true
requirements:
  - QUICK-CI-LINT-FIX
must_haves:
  truths:
    - "CI lint job uses golangci-lint built on Go 1.25.0 (matching go.mod `go 1.25.0`)"
    - "The `version:` input to golangci/golangci-lint-action@v9 is `v2.11.4`"
    - "Local `make lint` still exits 0 (no config drift introduced by the bump)"
  artifacts:
    - path: ".github/workflows/ci.yml"
      provides: "CI pipeline with golangci-lint v2.11.4 pinned under the golangci-lint-action@v9 step"
      contains: "version: v2.11.4"
  key_links:
    - from: ".github/workflows/ci.yml (lint job)"
      to: "go.mod (go 1.25.0 directive)"
      via: "golangci/golangci-lint-action@v9 `version: v2.11.4` (built on Go 1.25.0)"
      pattern: "version: v2\\.11\\.4"
---

<objective>
Unblock CI by bumping the `golangci/golangci-lint-action@v9` pinned golangci-lint `version` from `v2.1.6` to `v2.11.4` so the lint job no longer panics with "package requires newer Go version go1.25 (application built with go1.24)".

Purpose: go.mod declares `go 1.25.0`; golangci-lint v2.1.6 was built with Go 1.24 and cannot load packages requiring Go 1.25. golangci-lint v2.11.4 is built with Go 1.25.0 (verified via `gh api /repos/golangci/golangci-lint/contents/go.mod?ref=v2.11.4`).
Output: Updated `.github/workflows/ci.yml` lint step; no other files touched.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@.github/workflows/ci.yml
@Makefile
@go.mod

<interfaces>
<!-- Current CI lint step (from .github/workflows/ci.yml lines 11-21) -->
```yaml
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
```

<!-- Makefile lint target (from Makefile lines 35-44) — no pinned version, uses locally installed golangci-lint -->
```make
lint: lint-stdout
	golangci-lint run

lint-stdout:
	@echo "Checking for stdout usage in non-test code..."
	@if grep -rn 'fmt\.Print\|fmt\.Fprint.*os\.Stdout\|os\.Stdout' cmd/ internal/ --include='*.go' | grep -v '_test.go' | grep -v '// stdout-ok'; then \
		echo "ERROR: found stdout usage in non-test code (stdout reserved for go-plugin handshake)"; \
		exit 1; \
	fi
	@echo "No stdout usage found -- OK"
```
</interfaces>

**Scope note:** The only pinned golangci-lint version in the repo is `.github/workflows/ci.yml` line 21. Grep confirmed: Makefile calls `golangci-lint run` unpinned; `.golangci.yml` config format is v2 (compatible with both v2.1.6 and v2.11.4); CONTRIBUTING.md refers to "golangci-lint v2" without a specific patch version. No other files need updating.

**User feedback memory:** "Always run `make lint` before `git push` to catch CI issues locally." Task includes a local `make lint` verification before finishing.
</context>

<tasks>

<task type="auto">
  <name>Task 1: Bump golangci-lint version pin in CI workflow</name>
  <files>.github/workflows/ci.yml</files>
  <action>
In `.github/workflows/ci.yml`, change the single line inside the lint job:

  From:    `          version: v2.1.6`
  To:      `          version: v2.11.4`

This is the only edit. Do NOT touch:
- `go.mod` / `go.sum`
- `.golangci.yml` (v2 config format is compatible with both v2.1.6 and v2.11.4)
- `Makefile` (the local `lint` target intentionally uses whatever golangci-lint is installed on the developer's PATH — no pin to sync)
- `CONTRIBUTING.md` (refers to "golangci-lint v2" generically)
- `renovate.json` or any other config

After the edit, verify there are no remaining `v2.1.6` references in the workflow file.
  </action>
  <verify>
<automated>cd /Users/jorge/git/work/argo-rollouts-k6s-demo/argo-rollouts-k6-plugin && grep -n 'version: v2\.11\.4' .github/workflows/ci.yml && ! grep -n 'v2\.1\.6' .github/workflows/ci.yml && echo "CI VERSION PIN UPDATED"</automated>
  </verify>
  <done>`.github/workflows/ci.yml` contains `version: v2.11.4` on the golangci-lint-action step and no longer contains `v2.1.6`.</done>
</task>

<task type="auto">
  <name>Task 2: Run local make lint to confirm no regressions before push</name>
  <files></files>
  <action>
Per the user's standing feedback ("Run linter before pushing"), run the full local lint gate to confirm nothing else regressed. This uses the locally-installed golangci-lint (unrelated to the CI pin change, but a smoke test that the repo still lints cleanly with a v2 config).

If local golangci-lint is not at v2.11.4, install it first (matches the version CI will now use, so local output matches CI output):

```bash
which golangci-lint && golangci-lint --version || \
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
```

Then run `make lint`. Expect "0 issues." and the `lint-stdout` custom check to pass.

If `make lint` fails:
- Report the failure verbatim in the summary
- Do NOT attempt to "fix" unrelated lint findings in this quick task — that's out of scope
- Flag it as a blocker for the commit: the CI bump is correct, but something else in the tree is broken
  </action>
  <verify>
<automated>cd /Users/jorge/git/work/argo-rollouts-k6s-demo/argo-rollouts-k6-plugin && (which golangci-lint || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4) && make lint && echo "LOCAL LINT PASSES"</automated>
  </verify>
  <done>`make lint` exits 0 locally using golangci-lint v2.11.4 (or the already-installed version if it's v2.x). stdout-ok custom check passes. No new issues introduced.</done>
</task>

</tasks>

<verification>
- `.github/workflows/ci.yml` line inside the lint job reads exactly `          version: v2.11.4`.
- No other references to `v2.1.6` remain in the workflow file.
- `make lint` exits 0 locally.
- No other files were modified (git diff should be limited to a single line in ci.yml).

Phase-level check:
```bash
cd /Users/jorge/git/work/argo-rollouts-k6s-demo/argo-rollouts-k6-plugin && \
  git diff --name-only | grep -q '^\.github/workflows/ci\.yml$' && \
  [ "$(git diff --name-only | wc -l | tr -d ' ')" = "1" ] && \
  grep -q 'version: v2\.11\.4' .github/workflows/ci.yml && \
  ! grep -q 'v2\.1\.6' .github/workflows/ci.yml && \
  echo "SCOPE OK: single-file, single-line bump"
```
</verification>

<success_criteria>
- CI lint job will no longer panic on Go 1.25 packages (runs golangci-lint v2.11.4 built on Go 1.25.0 against go.mod `go 1.25.0`).
- Local `make lint` exits 0.
- Exactly one file changed (`.github/workflows/ci.yml`), exactly one line changed (`v2.1.6` → `v2.11.4`).
- No drift introduced in `.golangci.yml`, `Makefile`, `go.mod`, or documentation.
</success_criteria>

<output>
After completion, create `.planning/quick/260420-hal-bump-golangci-lint-action-version-in-ci-/260420-hal-SUMMARY.md` capturing:
- The old → new version pin
- Evidence local `make lint` passed
- Confirmation no other files were touched
</output>
