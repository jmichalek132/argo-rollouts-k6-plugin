---
phase: 09-metric-integration
reviewed: 2026-04-16T00:00:00Z
depth: standard
files_reviewed: 4
files_reviewed_list:
  - internal/provider/operator/summary.go
  - internal/provider/operator/summary_test.go
  - internal/provider/operator/operator.go
  - internal/provider/operator/operator_test.go
findings:
  critical: 0
  warning: 3
  info: 2
  total: 5
status: issues_found
---

# Phase 09: Code Review Report

**Reviewed:** 2026-04-16
**Depth:** standard
**Files Reviewed:** 4
**Status:** issues_found

## Summary

Four files reviewed: `summary.go` (pod log scanning and metric aggregation), `summary_test.go` (unit tests for all summary functions), `operator.go` (K6OperatorProvider implementation), and `operator_test.go` (integration tests for the provider).

The overall implementation is solid. The brace-counting JSON scanner, weighted-average aggregation, and graceful degradation pattern are well-designed and well-tested. Coverage of edge cases (truncated JSON, console.log JSON rejection, multi-pod aggregation, pod still running, zero-request-count divide-by-zero guard) is thorough.

Three warnings require attention:

1. The `findSummaryJSON` brace-counting algorithm mis-reports truncated JSON when `foundCandidate` is false and the log contains `{` in non-JSON context (e.g., shell output like `INFO {service=k6}`). This produces a spurious error that prevents graceful `nil, nil` return.
2. `TriggerRun` calls `readScript` (which hits the Kubernetes API to read a ConfigMap) even when the provider is configured for PrivateLoadZone (cloud-connected mode). The ConfigMap is not used by the PLZ execution path, making this a mandatory-but-unnecessary I/O operation.
3. `gvrForResource` in `testrun.go` panics on an unknown resource string. The panic is currently unreachable due to `decodeRunID` validation, but a future code change could break that invariant silently.

---

## Warnings

### WR-01: Spurious truncation error on non-JSON log lines containing `{`

**File:** `internal/provider/operator/summary.go:174-185`

**Issue:** The truncation detection code at lines 174-185 fires when `!foundCandidate` (no line ending with `}` was found) AND some log line contains `{`. This produces `fmt.Errorf("truncated JSON in pod logs: unbalanced braces")` for benign logs. Consider k6 structured output lines such as:

```
time="2024-01-01" level=info msg="{service=k6}"
```

or any log framework outputting `key={value}`. Because no such line ends with `}` (the `}` is mid-line), `foundCandidate` stays false, but the `{` check on line 179 fires, returning an error instead of `nil, nil`. The caller in `parseSummaryFromPods` logs this as a Warn and skips the pod — which is the intended behavior for a missing summary — but the error message incorrectly claims "truncated JSON" when there is no JSON at all.

**Fix:** Restrict the truncation check to lines that look like the start of a JSON object (trimmed line starts with `{`), not just any line containing `{`:

```go
// Detect truncated JSON: look for a line that is the start of a JSON object
// (trimmed content begins with '{') but no closing '}'-ended line was found.
if !foundCandidate {
    for _, l := range lines {
        trimmed := strings.TrimSpace(l)
        if strings.HasPrefix(trimmed, "{") {
            return nil, fmt.Errorf("truncated JSON in pod logs: unbalanced braces")
        }
    }
}
```

---

### WR-02: `TriggerRun` performs unnecessary ConfigMap read for PrivateLoadZone path

**File:** `internal/provider/operator/operator.go:199-210`

**Issue:** `TriggerRun` always calls `readScript` (step 2, line 206-209) before checking `isCloudConnected`. For PrivateLoadZone runs, the ConfigMap is never referenced by the operator — the k6 script comes from Grafana Cloud. This means:

- A valid, accessible ConfigMap must exist in the cluster even for pure cloud-connected runs.
- An unnecessary Kubernetes API call is made on every trigger.
- The error message "get configmap" will confuse users configuring PLZ-only runs who omitted `configMapRef`.

This inconsistency is compounded by the fact that `ValidateK6Operator` (called at step 1) requires `ConfigMapRef` to be non-nil for ALL k6-operator configs, regardless of cloud connectivity.

**Fix:** Gate the ConfigMap read on the execution path:

```go
// 2. Read script from ConfigMap only for direct TestRun path.
// PrivateLoadZone uses a cloud-hosted script; ConfigMap is not needed.
if !isCloudConnected(cfg) {
    if _, err := p.readScript(ctx, cfg); err != nil {
        return "", err
    }
}
```

Consider also relaxing `ValidateK6Operator` to not require `configMapRef` when `APIToken` and `StackID` are both set (since those fields indicate PLZ mode).

---

### WR-03: `gvrForResource` panics on unknown resource

**File:** `internal/provider/operator/testrun.go:112-121`

**Issue:** `gvrForResource` uses `panic` as its error path:

```go
default:
    panic(fmt.Sprintf("unknown resource %q", resource))
```

The function is currently called only from `GetRunResult` and `StopRun` after `decodeRunID` validation, which rejects any resource other than `"testruns"` or `"privateloadzones"`. The panic is therefore unreachable in the current code. However, the invariant is not compiler-enforced: a future caller that passes a resource string directly (bypassing `decodeRunID`) would cause a production panic.

**Fix:** Return an error instead of panicking, matching Go error handling conventions:

```go
func gvrForResource(resource string) (schema.GroupVersionResource, error) {
    switch resource {
    case "testruns":
        return testRunGVR, nil
    case "privateloadzones":
        return plzGVR, nil
    default:
        return schema.GroupVersionResource{}, fmt.Errorf("unknown k6-operator resource %q", resource)
    }
}
```

Update callers (`GetRunResult`, `StopRun`) to handle the error return.

---

## Info

### IN-01: `findSummaryJSON` brace counter does not handle `{`/`}` inside JSON string values

**File:** `internal/provider/operator/summary.go:129-144`

**Issue:** The brace-counting loop at lines 129-144 counts all `{` and `}` characters regardless of whether they appear inside JSON string values. Log lines containing JSON with string fields like `"msg":"completed {all} checks"` will produce incorrect depth counts, potentially selecting the wrong block boundaries. The comment at line 130 acknowledges this as "simple counting, not inside strings". The practical impact is limited because handleSummary JSON from k6 does not contain braces in string values, and the subsequent `json.Unmarshal` acts as a correctness gate. No behavior change is recommended unless k6 custom summary output with brace-containing strings becomes a use case.

---

### IN-02: `checkRunnerExitCodes` error path in `GetRunResult` swallows transient Kubernetes API errors

**File:** `internal/provider/operator/operator.go:323-335`

**Issue:** When `checkRunnerExitCodes` returns an error (e.g., transient Kubernetes API unavailability), `GetRunResult` returns `&provider.RunResult{State: provider.Errored}, nil`. The nil error makes the result indistinguishable from a deterministic error (script crash, infra issue). The orchestrator will treat the run as permanently failed and roll back. The root cause is logged via `slog.Warn` at line 325-331, which is good for observability, but the decision to absorb transient errors into a permanent Errored state could cause unnecessary rollbacks during brief API blips.

This is a documented design choice (the comment at line 79-90 of `operator.go` covers the sync.Once permanent-failure rationale). Flagging for awareness; no code change required unless the rollback sensitivity becomes a production concern.

---

_Reviewed: 2026-04-16_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
