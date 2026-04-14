---
status: complete
phase: 04-release-examples
source: [04-VERIFICATION.md]
started: 2026-04-10T11:35:00Z
updated: 2026-04-14T20:22:00Z
---

## Current Test

[testing complete]

## Tests

### 1. e2e Test Suite Execution

expected: On a machine with docker, kind, and kubectl, run `go test -v -tags=e2e -count=1 ./e2e/...` from repo root. All 4 tests pass: TestMetricPluginPass, TestMetricPluginFail, TestStepPluginPass, TestStepPluginFail. Kind cluster created, Argo Rollouts v1.9.0 installed, plugin binaries cross-compiled and loaded via file:// ConfigMap, mock server handles API calls, AnalysisRuns and Rollouts reach expected terminal phases.
result: pass

### 2. GitHub Release Artifact Verification

expected: Push a `v0.1.0-test` tag and inspect the resulting GitHub Release. 8 binary artifacts published (metric-plugin + step-plugin × {linux,darwin} × {amd64,arm64}) plus `checksums.txt` with SHA256 of each binary. Asset names match `{{ .Binary }}_{{ .Os }}_{{ .Arch }}` pattern.
result: pass

### 3. e2e Workflow on ubuntu-latest Runner

expected: Trigger e2e workflow via `workflow_dispatch` from GitHub Actions UI. Kind cluster created, 4 tests pass. If kind is absent from runner: add `- run: go install sigs.k8s.io/kind@v0.26.0` to `.github/workflows/e2e.yml`.
result: pass

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
