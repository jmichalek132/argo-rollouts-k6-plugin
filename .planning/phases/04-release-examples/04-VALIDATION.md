---
phase: 4
slug: release-examples
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-10
---

# Phase 4 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing stdlib + testify v1.11.1 + sigs.k8s.io/e2e-framework |
| **Config file** | none |
| **Quick run command** | `GOPATH="$HOME/go" go test -race -v -count=1 -tags=!e2e ./...` |
| **Full suite command** | `GOPATH="$HOME/go" go test -race -count=1 -tags=!e2e -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` |
| **e2e run command** | `GOPATH="$HOME/go" go test -v -tags=e2e -count=1 ./e2e/...` (requires kind + built binaries) |
| **Estimated runtime** | ~10s unit; ~3-5 min e2e (kind cluster startup) |

---

## Sampling Rate

- **After every task commit:** Run `GOPATH="$HOME/go" go test -race -count=1 -tags=!e2e ./...`
- **After every plan wave:** Run full suite + coverage check
- **Before `/gsd:verify-work`:** Full suite green + e2e green + both binaries built by goreleaser + README exists
- **Max feedback latency:** ~10 seconds (unit); ~5 min (e2e — only after wave 2)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 4-01-01 | 01 | 1 | DIST-01 | build | `go build ./cmd/metric-plugin ./cmd/step-plugin` | ✅ exists | ⬜ pending |
| 4-01-02 | 01 | 1 | DIST-02 | goreleaser | `goreleaser check` | ❌ W0 | ⬜ pending |
| 4-01-03 | 01 | 1 | DIST-03 | CI lint | `golangci-lint run` | ❌ W0 | ⬜ pending |
| 4-02-01 | 02 | 2 | TEST-02 | e2e compile | `go build -tags=e2e ./e2e/...` | ❌ W0 | ⬜ pending |
| 4-02-02 | 02 | 2 | TEST-02 | e2e run | `go test -v -tags=e2e ./e2e/...` | ❌ W0 | ⬜ pending |
| 4-02-03 | 02 | 2 | PLUG-03 | e2e | (covered by e2e run) | ❌ W0 | ⬜ pending |
| 4-03-01 | 03 | 3 | EXAM-01 | file | `test -f examples/threshold-gate/analysis-template.yaml` | ❌ W0 | ⬜ pending |
| 4-03-02 | 03 | 3 | EXAM-02 | file | `test -f examples/error-rate-latency/analysis-template.yaml` | ❌ W0 | ⬜ pending |
| 4-03-03 | 03 | 3 | EXAM-03 | file | `test -f examples/canary-full/rollout.yaml` | ❌ W0 | ⬜ pending |
| 4-03-04 | 03 | 3 | EXAM-04 | content | `grep -q '## Installation' README.md` | ❌ W0 | ⬜ pending |
| 4-03-05 | 03 | 3 | EXAM-05 | content | `grep -q 'Provider interface' CONTRIBUTING.md` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `.goreleaser.yaml` — goreleaser configuration (two binaries, 4 platforms, flat naming, SHA256)
- [ ] `.github/workflows/ci.yml` — CI workflow (lint, test, build)
- [ ] `.github/workflows/release.yml` — Release workflow (goreleaser on v* tag)
- [ ] `e2e/main_test.go` — e2e TestMain with kind cluster lifecycle
- [ ] `e2e/helpers/mock_server.go` — programmable net/http mock for k6 API
- [ ] `cmd/metric-plugin/main.go` and `cmd/step-plugin/main.go` — add `var version = "dev"` for LDFLAGS injection

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| GitHub Release assets appear correctly | DIST-02 | Requires actual tag push to GitHub | Push a test tag, verify assets in GitHub Releases UI |
| ConfigMap plugin loading with checksum | PLUG-03 | Requires live Argo Rollouts controller with real binary URL | Deploy with argo-rollouts-config ConfigMap, verify plugin loads |
| README renders correctly on GitHub | EXAM-04 | Markdown rendering is a browser concern | View README.md on GitHub repo page |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s (unit) / 5min (e2e acceptable)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
