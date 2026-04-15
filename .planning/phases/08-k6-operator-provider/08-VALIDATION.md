---
phase: 8
slug: k6-operator-provider
status: draft
nyquist_compliant: false
wave_0_complete: false
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
| **Quick run command** | `go test ./internal/provider/... -run TestK6Operator -count=1` |
| **Full suite command** | `go test ./... -count=1` |
| **Estimated runtime** | ~15 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/provider/... -run TestK6Operator -count=1`
- **After every plan wave:** Run `go test ./... -count=1`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 08-01-01 | 01 | 1 | K6OP-01 | — | N/A | unit | `go test ./internal/provider/... -run TestK6OperatorConfig` | ❌ W0 | ⬜ pending |
| 08-01-02 | 01 | 1 | K6OP-05 | — | Consistent naming with hash | unit | `go test ./internal/provider/... -run TestTestRunNaming` | ❌ W0 | ⬜ pending |
| 08-02-01 | 02 | 1 | K6OP-01 | — | N/A | unit | `go test ./internal/provider/... -run TestCreateTestRun` | ❌ W0 | ⬜ pending |
| 08-02-02 | 02 | 1 | K6OP-02 | — | Exit code inspection | unit | `go test ./internal/provider/... -run TestPollExitCodes` | ❌ W0 | ⬜ pending |
| 08-03-01 | 03 | 2 | K6OP-03 | — | Resource limits enforced | unit | `go test ./internal/provider/... -run TestConfigFields` | ❌ W0 | ⬜ pending |
| 08-04-01 | 04 | 2 | K6OP-04 | — | CR deletion on abort | unit | `go test ./internal/provider/... -run TestTerminateCleanup` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/provider/k6operator_test.go` — test stubs for K6OP-01 through K6OP-08
- [ ] Test fixtures for mock dynamic client responses (TestRun CR, Pod list)

*Existing Go test infrastructure covers framework needs.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| TestRun CR created in real cluster | K6OP-01 | Requires kind cluster with k6-operator CRDs | Deploy to kind, trigger rollout, verify CR with kubectl |
| Runner pods cleaned up on abort | K6OP-04 | Requires running k6 pods | Start test run, abort rollout, verify pods terminated |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
