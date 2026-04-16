---
phase: 08-k6-operator-provider
reviewed: 2026-04-16T12:00:00Z
depth: standard
files_reviewed: 7
files_reviewed_list:
  - internal/provider/operator/testrun.go
  - internal/provider/operator/testrun_test.go
  - internal/provider/operator/exitcode.go
  - internal/provider/operator/exitcode_test.go
  - internal/provider/config.go
  - internal/provider/operator/operator.go
  - internal/provider/operator/operator_test.go
findings:
  critical: 1
  warning: 5
  info: 3
  total: 9
status: issues_found
---

# Phase 8: Code Review Report

**Reviewed:** 2026-04-16T12:00:00Z
**Depth:** standard
**Files Reviewed:** 7
**Status:** issues_found

## Summary

The k6-operator provider implementation is well-structured with clean separation of concerns (CR building, exit code inspection, operator lifecycle). The code follows the project's established patterns (slog logging, provider interface contract, functional options). Test coverage is comprehensive with good edge-case handling.

Key concerns: (1) a potential panic from `gvrForResource` reachable through `decodeRunID` with crafted input in a race condition, (2) the `OwnerReference` missing the `Name` field which may cause Kubernetes GC issues, (3) the `testRunName` function truncation can produce invalid CR names by cutting mid-hash, and (4) label values from user-controlled `RolloutName` are not validated against Kubernetes label constraints.

## Critical Issues

### CR-01: OwnerReference with empty Name field may break Kubernetes garbage collection

**File:** `internal/provider/operator/testrun.go:61`
**Issue:** The `analysisRunOwnerRef` function sets `Name: ""` with a comment claiming "Name is not strictly required for GC; UID is the key field." However, the Kubernetes API server **requires** the `Name` field in OwnerReferences for validation. While the UID is used for the actual GC lookup, a CREATE request with an empty Name in an OwnerReference may be rejected by API server admission depending on the Kubernetes version, or it may create an OwnerReference that confuses tooling and `kubectl` output. The `Name` field is part of the OwnerReference schema and omitting it violates the Kubernetes API contract.
**Fix:**
Pass the AnalysisRun name alongside the UID and populate both fields:
```go
func analysisRunOwnerRef(analysisRunName, analysisRunUID string) []metav1.OwnerReference {
    if analysisRunUID == "" {
        return nil
    }
    return []metav1.OwnerReference{{
        APIVersion: "argoproj.io/v1alpha1",
        Kind:       "AnalysisRun",
        Name:       analysisRunName,
        UID:        types.UID(analysisRunUID),
    }}
}
```
This requires adding `AnalysisRunName` to `PluginConfig` alongside the existing `AnalysisRunUID`, populated from `AnalysisRun.ObjectMeta.Name` at the plugin layer.

## Warnings

### WR-01: testRunName truncation can produce duplicate or invalid CR names

**File:** `internal/provider/operator/testrun.go:46-48`
**Issue:** When the rollout name is extremely long, `testRunName` truncates at 253 characters. The truncation may cut into the 8-character hash suffix, producing names that lack uniqueness guarantees. For example, a 250-char rollout name produces `k6-<250chars>-<8hex>` = 263 chars, truncated to 253 chars, leaving only 0 of the 8 hex hash characters. Two runs of the same long-named rollout would then produce identical CR names (both truncated to the same prefix), causing `AlreadyExists` errors on the second create.
**Fix:**
Reserve space for the `k6-` prefix (3 chars), separator (1 char), and hash (8 chars) = 12 chars minimum. Truncate the rollout name to fit:
```go
func testRunName(rolloutName, namespace string) string {
    h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", namespace, rolloutName, time.Now().UnixNano())))
    short := fmt.Sprintf("%x", h[:4]) // 8 hex chars
    // Reserve: "k6-" (3) + "-" (1) + hash (8) = 12 chars
    maxRolloutLen := 253 - 12
    if len(rolloutName) > maxRolloutLen {
        rolloutName = rolloutName[:maxRolloutLen]
    }
    return fmt.Sprintf("k6-%s-%s", rolloutName, short)
}
```

### WR-02: Label value from RolloutName not validated against Kubernetes constraints

**File:** `internal/provider/operator/testrun.go:113` and `internal/provider/operator/testrun.go:155`
**Issue:** `cfg.RolloutName` is used directly as a Kubernetes label value (`labelRollout`). Kubernetes label values must be at most 63 characters and match the regex `^[a-z0-9A-Z]([a-z0-9A-Z._-]*[a-z0-9A-Z])?$` (or be empty). If `RolloutName` exceeds 63 characters or contains invalid characters, the CR creation will fail with a validation error from the API server, producing an unhelpful error message for the user.
**Fix:**
Truncate and sanitize the rollout name before using it as a label value:
```go
func sanitizeLabelValue(v string) string {
    if len(v) > 63 {
        v = v[:63]
    }
    // Trim trailing non-alphanumeric characters after truncation
    v = strings.TrimRight(v, ".-_")
    return v
}
```

### WR-03: checkRunnerExitCodes only inspects ContainerStatuses, not InitContainerStatuses

**File:** `internal/provider/operator/exitcode.go:89`
**Issue:** The function iterates only over `pod.Status.ContainerStatuses`. If the k6-operator ever uses init containers for setup (e.g., script download, cloud registration), failures in init containers would be invisible. While current k6-operator versions do not use init containers in runner pods, this is a defensive coding gap. More concretely, if a pod has zero `ContainerStatuses` entries (e.g., pod scheduled but containers not yet created), the loop body never executes and the function returns `Passed` -- which is incorrect for a pod that hasn't run anything.
**Fix:**
Add a guard for empty container statuses before the loop:
```go
allStatuses := pod.Status.ContainerStatuses
if len(allStatuses) == 0 {
    // Pod exists but no container status yet -- still starting up
    return provider.Running, nil
}
```

### WR-04: Redundant readScript call in TriggerRun discards the script content

**File:** `internal/provider/operator/operator.go:183-185`
**Issue:** `TriggerRun` calls `readScript` at line 183 to verify the ConfigMap exists, but discards the returned script content (assigned to `_`). The actual script reference in the CR uses `cfg.ConfigMapRef.Name` and `cfg.ConfigMapRef.Key` directly (line 231). This means the ConfigMap is read once for validation, then the k6-operator reads it again independently. While this is not a bug (it's a pre-flight check), it performs unnecessary I/O. If the ConfigMap is deleted between this check and the k6-operator reading it, the pre-flight check provides a false sense of security.
**Fix:**
This is a design tradeoff. If the pre-flight check is kept for better error messages, document clearly that it's a best-effort check and the real read happens in the operator. Alternatively, remove it entirely and let the k6-operator report ConfigMap errors through the CR status:
```go
// Option A: Keep but document the TOCTOU nature
// readScript is a best-effort pre-flight check. The k6-operator independently
// reads the ConfigMap; this check provides early user-facing error messages.
```

### WR-05: decodeRunID accepts slashes in the name component via SplitN

**File:** `internal/provider/operator/testrun.go:76`
**Issue:** `strings.SplitN(runID, "/", 3)` splits into at most 3 parts, meaning the third part (name) can contain slashes. While Kubernetes resource names cannot contain slashes, a malformed runID like `ns/testruns/name/extra/stuff` would pass validation and be used as a CR name in API calls, which would fail at the API server with an unhelpful error. The `decodeRunID` function should validate that the name component is a valid Kubernetes name.
**Fix:**
Add a basic validation that the name does not contain slashes:
```go
if strings.Contains(name, "/") {
    return "", "", "", fmt.Errorf("invalid run ID %q: name component contains slash", runID)
}
```

## Info

### IN-01: testRunName uses time.Now() making it non-deterministic and hard to unit test

**File:** `internal/provider/operator/testrun.go:42`
**Issue:** `testRunName` calls `time.Now().UnixNano()` directly, making it impossible to produce deterministic names in tests. The test `TestTestRunName_HashIncludesNamespace` works around this by generating many names and checking set cardinality, which is fragile. Consider injecting a clock or a nonce generator for testability.
**Fix:**
Accept a nonce parameter or use a clock interface:
```go
func testRunName(rolloutName, namespace string, nonce int64) string {
    h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", namespace, rolloutName, nonce)))
    // ...
}
```
Callers pass `time.Now().UnixNano()` in production, tests pass a fixed value.

### IN-02: buildPrivateLoadZone does not set the Script field

**File:** `internal/provider/operator/testrun.go:144-171`
**Issue:** `buildPrivateLoadZone` constructs a `PrivateLoadZone` CR but does not set a `Script` field or any equivalent. The `buildTestRun` function sets `Spec.Script` with the ConfigMap reference, but the PLZ builder does not. This may be intentional if PLZ CRs pull scripts via Grafana Cloud, but it is not documented in the function comment. If a user configures both cloud credentials and a ConfigMapRef, the ConfigMapRef is silently ignored for PLZ mode.
**Fix:**
Add a comment documenting this design decision:
```go
// buildPrivateLoadZone constructs a PrivateLoadZone CR for Grafana Cloud-connected
// in-cluster execution. Unlike buildTestRun, no Script field is set -- the PLZ
// CRD pulls the test script from Grafana Cloud via the API token.
// Any configMapRef in the user's config is intentionally ignored in PLZ mode.
```

### IN-03: Unused plzGVR variable in non-PLZ code paths

**File:** `internal/provider/operator/testrun.go:24-28`
**Issue:** `plzGVR` is declared as a package-level variable alongside `testRunGVR`. While it is used in `gvrForResource` and the PLZ code path, it contributes to package-level state. This is a minor point -- the current code is correct. Just noting that if PLZ support were ever removed, this would be dead code.
**Fix:** No action needed. This is informational for future maintenance.

---

_Reviewed: 2026-04-16T12:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
