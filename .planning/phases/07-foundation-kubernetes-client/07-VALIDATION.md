---
phase: 7
slug: foundation-kubernetes-client
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-15
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + testify v1.11.x |
| **Config file** | none — existing test infrastructure |
| **Quick run command** | `go test ./internal/provider/... -count=1 -short` |
| **Full suite command** | `go test ./... -count=1 -race` |
| **Estimated runtime** | ~15 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/provider/... -count=1 -short`
- **After every plan wave:** Run `go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 07-01-01 | 01 | 1 | FOUND-01 | — | Provider routing defaults to grafana-cloud | unit | `go test ./internal/provider/ -run TestRouter -count=1` | ❌ W0 | ⬜ pending |
| 07-01-02 | 01 | 1 | FOUND-01 | — | Unknown provider returns error | unit | `go test ./internal/provider/ -run TestRouter -count=1` | ❌ W0 | ⬜ pending |
| 07-02-01 | 02 | 1 | FOUND-02 | — | K8s client created via InClusterConfig | unit | `go test ./internal/provider/operator/ -run TestEnsureClient -count=1` | ❌ W0 | ⬜ pending |
| 07-02-02 | 02 | 1 | FOUND-02 | — | WithClient option overrides lazy init | unit | `go test ./internal/provider/operator/ -run TestWithClient -count=1` | ❌ W0 | ⬜ pending |
| 07-03-01 | 03 | 2 | FOUND-03 | — | ConfigMap read returns script content | unit | `go test ./internal/provider/operator/ -run TestReadScript -count=1` | ❌ W0 | ⬜ pending |
| 07-03-02 | 03 | 2 | FOUND-03 | — | Missing key returns error | unit | `go test ./internal/provider/operator/ -run TestReadScript -count=1` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/provider/router_test.go` — tests for Router dispatch and default behavior
- [ ] `internal/provider/operator/operator_test.go` — tests for K8s client lifecycle and ConfigMap reading

*Existing test infrastructure (go test, testify) covers all framework needs.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| In-cluster client init works in real K8s | FOUND-02 | Requires actual cluster with service account | Deploy plugin to kind cluster, check slog output for successful client creation |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
