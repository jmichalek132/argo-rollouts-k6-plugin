---
phase: 9
slug: metric-integration
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-16
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + testify v1.9.x |
| **Config file** | none — existing test infrastructure |
| **Quick run command** | `go test ./internal/provider/operator/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~15 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/provider/operator/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 09-01-01 | 01 | 1 | METR-01 | — | N/A | unit | `go test ./internal/provider/operator/... -run TestParseSummary` | ❌ W0 | ⬜ pending |
| 09-01-02 | 01 | 1 | METR-01 | — | N/A | unit | `go test ./internal/provider/operator/... -run TestExtractMetrics` | ❌ W0 | ⬜ pending |
| 09-02-01 | 02 | 1 | METR-02 | — | N/A | unit | `go test ./internal/provider/operator/... -run TestGetRunResult` | ❌ W0 | ⬜ pending |
| 09-02-02 | 02 | 1 | METR-02 | — | N/A | integration | `go test ./internal/provider/operator/... -run TestMetricCompatibility` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/provider/operator/summary_test.go` — stubs for METR-01 (handleSummary JSON parsing)
- [ ] `internal/provider/operator/result_test.go` — stubs for METR-02 (metric extraction and compatibility)

*Existing test infrastructure covers framework setup — no new fixtures needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| successCondition expressions evaluate identically across providers | METR-02 | Requires real AnalysisTemplate YAML + Argo Rollouts eval engine | Create AnalysisTemplate with p95 < 500 threshold, run against both Grafana Cloud and k6-operator providers, compare measurement results |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
