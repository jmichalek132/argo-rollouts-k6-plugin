---
phase: 08-k6-operator-provider
fixed_at: 2026-04-16T15:09:45Z
review_path: .planning/phases/08-k6-operator-provider/08-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 8: Code Review Fix Report

**Fixed at:** 2026-04-16T15:09:45Z
**Source review:** .planning/phases/08-k6-operator-provider/08-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 6
- Fixed: 6
- Skipped: 0

## Fixed Issues

### CR-01: OwnerReference with empty Name field may break Kubernetes garbage collection

**Files modified:** `internal/provider/config.go`, `internal/provider/operator/testrun.go`, `internal/provider/operator/testrun_test.go`, `internal/provider/operator/operator_test.go`
**Commit:** 6765e63
**Applied fix:** Added `AnalysisRunName` field to `PluginConfig`. Updated `analysisRunOwnerRef` signature to accept both name and UID parameters. Updated both callers (`buildTestRun`, `buildPrivateLoadZone`) to pass `cfg.AnalysisRunName`. Updated all tests that set `AnalysisRunUID` to also set `AnalysisRunName` and assert the Name field is populated in OwnerReferences.

### WR-01: testRunName truncation can produce duplicate or invalid CR names

**Files modified:** `internal/provider/operator/testrun.go`, `internal/provider/operator/testrun_test.go`
**Commit:** 4a406db
**Applied fix:** Changed truncation strategy: instead of truncating the final composed name (which could cut into the hash suffix), the rollout name component is now truncated to `253 - 12` characters before composition, reserving space for the `k6-` prefix (3), separator (1), and hash (8). Added test assertion that the 8-char hex hash suffix is always fully present even with very long rollout names.

### WR-02: Label value from RolloutName not validated against Kubernetes constraints

**Files modified:** `internal/provider/operator/testrun.go`, `internal/provider/operator/testrun_test.go`
**Commit:** cfc8509
**Applied fix:** Added `sanitizeLabelValue` helper that truncates to 63 characters and trims trailing non-alphanumeric characters (`.-_`). Applied it to both `buildTestRun` and `buildPrivateLoadZone` where `cfg.RolloutName` is used as a label value. Added 5 unit tests covering short values, empty string, truncation at 63, trailing special char trimming, and edge case where truncation lands on a special character.

### WR-03: checkRunnerExitCodes only inspects ContainerStatuses, not InitContainerStatuses

**Files modified:** `internal/provider/operator/exitcode.go`, `internal/provider/operator/exitcode_test.go`
**Commit:** c9bf7e0
**Applied fix:** Added guard before the container status loop: if a pod has zero `ContainerStatuses` entries (scheduled but containers not yet created), return `provider.Running` instead of falling through to return `Passed`. Added test `TestCheckRunnerExitCodes_EmptyContainerStatuses` that creates a pod with nil ContainerStatuses and verifies Running is returned.

### WR-04: Redundant readScript call in TriggerRun discards the script content

**Files modified:** `internal/provider/operator/operator.go`
**Commit:** 94480d0
**Applied fix:** Added a multi-line comment documenting the TOCTOU (time-of-check/time-of-use) nature of the readScript pre-flight check. The comment explains that the k6-operator independently reads the ConfigMap, and this check provides early user-facing error messages but cannot prevent races where the ConfigMap is deleted between the check and the operator's read.

### WR-05: decodeRunID accepts slashes in the name component via SplitN

**Files modified:** `internal/provider/operator/testrun.go`, `internal/provider/operator/testrun_test.go`
**Commit:** badd9f0
**Applied fix:** Added validation after SplitN that rejects name components containing slashes. A malformed runID like `ns/testruns/name/extra/stuff` now returns a clear error instead of passing through to the Kubernetes API. Added test `TestDecodeRunID_NameContainsSlash` that verifies the error message.

## Skipped Issues

None -- all findings were fixed.

---

_Fixed: 2026-04-16T15:09:45Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
