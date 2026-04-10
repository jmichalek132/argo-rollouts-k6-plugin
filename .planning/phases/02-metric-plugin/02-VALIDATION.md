---
phase: 2
slug: metric-plugin
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-04-09
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing stdlib + testify v1.11.1 |
| **Config file** | none |
| **Quick run command** | `GOPATH="$HOME/go" go test -race -v -count=1 ./internal/metric/...` |
| **Full suite command** | `GOPATH="$HOME/go" go test -race -v -count=1 -coverprofile=coverage.out ./internal/... && GOPATH="$HOME/go" go tool cover -func=coverage.out` |
| **Estimated runtime** | ~10 seconds (mocked, no network; race adds ~2x) |

---

## Sampling Rate

- **After every task commit:** Run `GOPATH="$HOME/go" go test -race -v -count=1 ./internal/metric/...`
- **After every plan wave:** Run full suite + coverage check
- **Before `/gsd:verify-work`:** Full suite green + >=80% coverage on `internal/metric/` + both binaries compile CGO_ENABLED=0
- **Max feedback latency:** ~10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 2-01-01 | 01 | 1 | PLUG-01 | unit (compile) | `go build ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-01-02 | 01 | 1 | METR-01 | unit | `go test -run TestResume_Thresholds ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-01-03 | 01 | 1 | METR-02 | unit | `go test -run TestResume_HTTPReqFailed ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-01-04 | 01 | 1 | METR-03 | unit | `go test -run TestResume_HTTPReqDuration ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-01-05 | 01 | 1 | METR-04 | unit | `go test -run TestResume_HTTPReqs ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-01-06 | 01 | 1 | METR-05 | unit | `go test -run TestResume.*Metadata ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-01-07 | 01 | 1 | TEST-01 | coverage | `go test -race -coverprofile=c.out ./internal/metric/... && go tool cover -func=c.out` | ❌ W0 | ⬜ pending |
| 2-01-08 | 01 | 1 | TEST-03 | race | `go test -race -run TestResume_ConcurrentSafety ./internal/metric/...` | ❌ W0 | ⬜ pending |
| 2-02-01 | 02 | 2 | PLUG-01 | build | `CGO_ENABLED=0 go build ./cmd/metric-plugin` | ✅ exists | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/metric/metric_test.go` — test stubs for K6MetricProvider (PLUG-01, METR-01–05, TEST-01, TEST-03)
- [ ] `internal/provider/cloud/metrics_test.go` — tests for v5 aggregate endpoint parsing
- [ ] `github.com/argoproj/argo-rollouts v1.9.0` added to go.mod — required before any metric test compiles

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Measurement.Metadata visible in kubectl | METR-05 | Requires a live AnalysisRun against k8s cluster | `kubectl get analysisrun <name> -o yaml` — check `.status.metricResults[].measurements[].metadata` |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-04-09
