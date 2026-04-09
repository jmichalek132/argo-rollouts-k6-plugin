---
phase: 1
slug: foundation-provider
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-04-09
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing stdlib + testify v1.11.1 |
| **Config file** | none — Wave 0 installs |
| **Quick run command** | `go test -race -v -short ./...` |
| **Full suite command** | `go test -race -v -count=1 ./...` |
| **Estimated runtime** | ~10 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test -race -v -short ./...`
- **After every plan wave:** Run `go test -race -v -count=1 ./...` + `make build`
- **Before `/gsd:verify-work`:** Full suite must be green + both binaries compile with `CGO_ENABLED=0`
- **Max feedback latency:** ~10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 1-01-01 | 01 | 1 | PLUG-04 | unit (compile) | `go build ./internal/provider/...` | ❌ W0 | ⬜ pending |
| 1-01-02 | 01 | 1 | PROV-01 | unit | `go test -run TestAuth ./internal/provider/cloud/` | ❌ W0 | ⬜ pending |
| 1-01-03 | 01 | 1 | PROV-02 | unit | `go test -run TestTriggerRun ./internal/provider/cloud/` | ❌ W0 | ⬜ pending |
| 1-01-04 | 01 | 1 | PROV-03 | unit | `go test -run TestGetRunResult ./internal/provider/cloud/` | ❌ W0 | ⬜ pending |
| 1-01-05 | 01 | 1 | PROV-04 | unit | `go test -run TestStopRun ./internal/provider/cloud/` | ❌ W0 | ⬜ pending |
| 1-01-06 | 01 | 1 | DIST-01 | build | `CGO_ENABLED=0 go build ./cmd/metric-plugin && CGO_ENABLED=0 go build ./cmd/step-plugin` | ❌ W0 | ⬜ pending |
| 1-01-07 | 01 | 1 | DIST-04 | lint | `make lint-stdout` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/provider/cloud/cloud_test.go` — stubs for PROV-01, PROV-02, PROV-03, PROV-04 (httptest mock server pattern)
- [ ] `Makefile` `lint-stdout` target — forbidigo or grep-based check for `fmt.Print`/`os.Stdout` usage before `plugin.Serve()`
- [ ] `.golangci.yml` — golangci-lint v2 config with forbidigo linter enabled for stdout detection

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Plugin binary does not write stdout before Serve() | DIST-04 | Requires running the binary and observing stdout before sending the handshake cookie | `./bin/metric-plugin 2>/dev/null | xxd | head` — output should be empty until handshake |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
