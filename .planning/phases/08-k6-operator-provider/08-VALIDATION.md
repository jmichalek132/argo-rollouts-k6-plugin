---
phase: 8
slug: k6-operator-provider
status: draft
nyquist_compliant: true
wave_0_complete: true
created: 2026-04-15
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none — standard Go test toolchain |
| **Quick run command** | `go test ./internal/provider/... -count=1` |
| **Full suite command** | `go test ./... -count=1` |
| **Estimated runtime** | ~15 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/provider/... -count=1`
- **After every plan wave:** Run `go test ./... -count=1`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|--------|
| 08-01-01 | 01 | 1 | K6OP-01, K6OP-03, K6OP-05, K6OP-06, K6OP-07 | T-08-01, T-08-02, T-08-04 | Naming hash, parallelism validation, owner refs (D-09) | unit (TDD) | `go test -race ./internal/provider/operator/... -run "TestBuild\|TestTestRun\|TestPlz\|TestIsCloud"` | ⬜ pending |
| 08-01-02 | 01 | 1 | K6OP-02, K6OP-04 | T-08-03 | Exit code inspection, nil-terminated safety | unit (TDD) | `go test -race ./internal/provider/operator/... -run "TestExitCode\|TestStage\|TestCheckRunner"` | ⬜ pending |
| 08-02-01 | 02 | 2 | K6OP-01, K6OP-02, K6OP-03, K6OP-04, K6OP-05, K6OP-06, K6OP-07, K6OP-08 | T-08-05 through T-08-09 | CR creation with owner refs, stage polling, exit code check, CR deletion | unit (TDD) | `go test -race ./internal/provider/operator/... -run "TestTriggerRun\|TestGetRunResult\|TestStopRun\|TestEnsureClient"` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 is not needed. All tasks use TDD with inline test creation — tests are written as part of each task's RED phase before implementation.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| TestRun CR created in real cluster | K6OP-01 | Requires kind cluster with k6-operator CRDs | Deploy to kind, trigger rollout, verify CR with kubectl |
| Runner pods cleaned up on abort | K6OP-04 | Requires running k6 pods | Start test run, abort rollout, verify pods terminated |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify commands
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 unnecessary — TDD tasks create tests inline
- [x] No watch-mode flags
- [x] Feedback latency < 15s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
