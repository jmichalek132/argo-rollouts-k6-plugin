---
phase: 10-documentation-e2e
fixed_at: 2026-04-16T12:30:00Z
review_path: .planning/phases/10-documentation-e2e/10-REVIEW.md
iteration: 1
findings_in_scope: 4
fixed: 4
skipped: 0
status: all_fixed
---

# Phase 10: Code Review Fix Report

**Fixed at:** 2026-04-16T12:30:00Z
**Source review:** .planning/phases/10-documentation-e2e/10-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 4 (Critical: 0, Warning: 4)
- Fixed: 4
- Skipped: 0

## Fixed Issues

### WR-01: Top-level README misrepresents k6-operator example contents

**Files modified:** `README.md`
**Commit:** 069559e
**Applied fix:** Reworded the "Each example directory contains" block to scope
the file list to cloud-mode examples (`threshold-gate`, `error-rate-latency`,
`canary-full`) and added a paragraph pointing users to
`examples/k6-operator/README.md` for the in-cluster example with its different
contents (ClusterRole, ClusterRoleBinding, ConfigMap script).

### WR-02: examples/k6-operator/README.md contradicts pinned-version recommendation

**Files modified:** `examples/k6-operator/rollout-step.yaml`, `examples/k6-operator/analysistemplate.yaml`, `examples/k6-operator/README.md`
**Commit:** 36f6bb6
**Applied fix:** Added `runnerImage: "grafana/k6:0.56.0"` to
`rollout-step.yaml` (inside the plugin config) and `analysistemplate.yaml`
(inside `jmichalek132/k6:` block). Updated the Notes section of the examples
README to reflect that the examples now pin the image and strengthened the
warning about `grafana/k6:latest` being non-deterministic. Note: the finding
also listed `rollout-metric.yaml` but per the finding text that file "does not
set it directly, but analysistemplate.yaml should" -- so no change was needed
there (the runnerImage lives in the AnalysisTemplate, which is now pinned).

### WR-03: countTestRuns race -- TestRun may be garbage-collected before the assertion reads it

**Files modified:** `e2e/k6_operator_test.go`
**Commit:** c7b0e50
**Applied fix:** Reordered the Assess step to poll for TestRun creation BEFORE
waiting for Rollout to reach Healthy. Inline polling loop (no new helper added)
with 5-minute deadline and 3-second interval, mirroring the pattern used by
`waitForRolloutPhase`. On timeout, dumps diagnostics and fails. This eliminates
the race where a future Terminate/cleanup hook could delete the TestRun between
Healthy transition and the assertion.

### WR-04: Step plugin rollout trigger uses pod-template annotation; no guarantee k6 step ever executes

**Files modified:** `e2e/k6_operator_test.go`
**Commit:** f225e3d
**Applied fix:** Added `kubectl delete testruns --all` in Setup immediately
before the canary trigger patch. Now any TestRun counted in Assess must have
been created by the post-patch canary step, proving the step plugin actually
executed (not coincidental from a transient reconcile earlier in the test
lifecycle). This is the "minimum viable" option from the finding.

## Skipped Issues

None.

---

_Fixed: 2026-04-16T12:30:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
