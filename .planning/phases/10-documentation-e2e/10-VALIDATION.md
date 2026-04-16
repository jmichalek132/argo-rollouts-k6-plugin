---
phase: 10
slug: documentation-e2e
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-16
---

# Phase 10 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + sigs.k8s.io/e2e-framework |
| **Config file** | `e2e/main_test.go` (TestMain with envconf) |
| **Quick run command** | `go test ./...` |
| **Full suite command** | `go test -tags e2e -v ./e2e/...` |
| **Estimated runtime** | ~120 seconds (kind cluster lifecycle + k6-operator install) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./...`
- **After every plan wave:** Run `go test -tags e2e -v ./e2e/...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 10-01-01 | 01 | 1 | DOCS-01 | — | N/A | manual | `kubectl apply -f examples/k6-operator/` | ❌ W0 | ⬜ pending |
| 10-01-02 | 01 | 1 | DOCS-02 | — | N/A | manual | `kubectl apply -f examples/k6-operator/` | ❌ W0 | ⬜ pending |
| 10-02-01 | 02 | 2 | TEST-01 | — | N/A | e2e | `go test -tags e2e -v -run TestK6Operator ./e2e/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `e2e/k6operator_test.go` — e2e test file for k6-operator integration
- [ ] `examples/k6-operator/` — example directory with YAML files

*Existing e2e infrastructure (main_test.go, mock server) covers base requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| RBAC ClusterRole grants correct permissions | DOCS-01 | RBAC correctness verified by successful TestRun creation in e2e | Apply clusterrole.yaml, create TestRun, verify no RBAC errors |
| Example YAML works out of the box | DOCS-02 | Requires manual cluster with k6-operator | Apply all YAML files to a cluster with k6-operator installed |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
