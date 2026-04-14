---
phase: 3
slug: step-plugin
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-10
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing stdlib + testify v1.11.1 |
| **Config file** | none |
| **Quick run command** | `GOPATH="$HOME/go" go test -race -v -count=1 ./internal/step/...` |
| **Full suite command** | `GOPATH="$HOME/go" go test -race -v -count=1 -coverprofile=coverage.out ./internal/... && GOPATH="$HOME/go" go tool cover -func=coverage.out` |
| **Estimated runtime** | ~10 seconds (mocked, no network; race adds ~2x) |

---

## Sampling Rate

- **After every task commit:** Run `GOPATH="$HOME/go" go test -race -v -count=1 ./internal/step/...`
- **After every plan wave:** Run full suite + coverage check
- **Before `/gsd:verify-work`:** Full suite green + >=80% coverage on `internal/step/` + both binaries compile CGO_ENABLED=0
- **Max feedback latency:** ~10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 3-01-01 | 01 | 1 | PLUG-02, STEP-01 | unit (compile) | `go build ./internal/step/...` | ❌ W0 | ⬜ pending |
| 3-01-02 | 01 | 1 | STEP-02 | unit | `go test -run TestRun_FirstCall ./internal/step/...` | ❌ W0 | ⬜ pending |
| 3-01-03 | 01 | 1 | STEP-02 | unit | `go test -run TestRun_PollsInProgress ./internal/step/...` | ❌ W0 | ⬜ pending |
| 3-01-04 | 01 | 1 | STEP-03, STEP-04 | unit | `go test -run TestRun_Terminal ./internal/step/...` | ❌ W0 | ⬜ pending |
| 3-01-05 | 01 | 1 | STEP-01 | unit | `go test -run TestRun_Timeout ./internal/step/...` | ❌ W0 | ⬜ pending |
| 3-01-06 | 01 | 1 | STEP-05 | unit | `go test -run TestTerminate\|TestAbort ./internal/step/...` | ❌ W0 | ⬜ pending |
| 3-01-07 | 01 | 1 | TEST-01 | coverage | `go test -race -coverprofile=c.out ./internal/step/... && go tool cover -func=c.out` | ❌ W0 | ⬜ pending |
| 3-02-01 | 02 | 2 | PLUG-02 | build | `CGO_ENABLED=0 go build ./cmd/step-plugin` | ✅ exists | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/step/step_test.go` — test stubs for K6StepPlugin (PLUG-02, STEP-01 through STEP-05)
- [ ] No new go.mod deps needed — argo-rollouts v1.9.0 already in go.mod from Phase 2

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| testRunId accessible via step output in Rollout | STEP-03 | Requires live Argo Rollouts + Rollout spec with step output expression | Deploy Rollout with step plugin, check `kubectl get rollout -o yaml` for step output |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
