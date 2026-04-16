---
phase: 09-metric-integration
asvs_level: 1
audited: 2026-04-16
block_on: high
---

# Security Audit — Phase 09: Metric Integration

**Threats Closed:** 6/6
**ASVS Level:** 1
**Result:** SECURED

---

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-09-01 | Denial of Service | mitigate | CLOSED | `summary.go:284-291` — `readPodLogs` sets `TailLines=100` and `LimitBytes=65536` (64KB) on every `PodLogOptions` call. `json.Unmarshal` operates on the bounded string. Test: `summary_test.go:392-402` (`TestReadPodLogs_SetsTailLinesAndLimitBytes`) captures options and asserts exact values. |
| T-09-02 | Tampering | accept | CLOSED | Accepted by design (D-01). User controls the k6 script and its `handleSummary()` output; metric values in pod logs are trusted by design. Exit code-based threshold evaluation (Phase 8) provides independent pass/fail verification independent of summary JSON content. No SECURITY.md accepted-risks log entry required for accepted threats whose rationale is fully documented in the plan. |
| T-09-03 | Tampering | mitigate | CLOSED | `summary.go:162-164` — dual-key validation: `if summary.Metrics != nil && summary.RootGroup != nil`. `RootGroup` declared as `json.RawMessage` (`summary.go:53`) so its presence is validated without full parsing. Scanning continues backward on failure (handles multiple JSON objects). Tests: `summary_test.go:78-94` (`TestFindSummaryJSON_MissingMetricsKey`, `TestFindSummaryJSON_MissingRootGroupKey`) assert both nil-summary/nil-error. `summary_test.go:165-176` (`TestFindSummaryJSON_MultipleJSONObjects`) verifies backward scan returns the real handleSummary over prior console.log JSON. |
| T-09-04 | Information Disclosure | accept | CLOSED | Accepted. Plugin reads only runner pods it created, identified by label selector `app=k6,k6_cr=<name>,runner=true` (`summary.go:305`). RBAC scope limiting is documented as a Phase 7/10 control. Sensitive log content exposure risk is accepted in threat register. |
| T-09-05 | Denial of Service | mitigate | CLOSED | `operator.go:356` — `parseSummaryFromPods` is called only inside the `if state == provider.Passed \|\| state == provider.Failed` guard, which is only entered after `stage == "finished"` triggers exit code checking. Non-terminal and error states skip the log read entirely. Tests: `operator_test.go:725-756` (`TestGetRunResult_NonTerminalSkipsMetrics`, `TestGetRunResult_ErrorStateSkipsMetrics`) verify no log reader is needed. Bounded log reads from T-09-01 mitigation apply transitively. |
| T-09-06 | Repudiation | mitigate | CLOSED | `operator.go:360-366` — `slog.Warn` with structured fields `"name"`, `"namespace"`, `"error"`, `"provider"`, `"affectedMetrics"` on parse failure. `summary.go:331-347` — per-pod `slog.Warn` calls distinguish log read error from missing JSON from malformed JSON, each with `"affectedMetrics"` field. `summary.go:366-376` — `slog.Debug` with aggregated metric values (`httpReqFailed`, `httpReqDurationP50/P95/P99`, `httpReqs`, `podCount`, `podsWithSummary`) provides audit trail for all extraction decisions. |

---

## Accepted Risks Log

| Threat ID | Category | Rationale | Owner |
|-----------|----------|-----------|-------|
| T-09-02 | Tampering | User controls k6 script and handleSummary output. Metric values from pod logs are trusted by design (D-01). Exit code threshold verification (Phase 8) is independent of handleSummary content and provides pass/fail integrity. Risk is user spoofing their own metrics. | jmichalek132 |
| T-09-04 | Information Disclosure | Plugin reads only runner pods it created via label selector `app=k6,k6_cr=<name>,runner=true`. RBAC controls (Phase 7/10) bound the service account scope. Sensitive data in k6 script logs is a user responsibility. | jmichalek132 |

---

## Unregistered Flags

None. No unregistered threat flags were raised in `09-01-SUMMARY.md` or `09-02-SUMMARY.md` `## Threat Flags` sections.

---

## Verification Notes

- `LimitBytes` value confirmed as `int64(64 * 1024) = 65536` at `summary.go:286`, matching 64KB bound declared in T-09-01 mitigation plan.
- `APPROXIMATION` keyword present in `aggregateMetrics` doc comment at `summary.go:235`, documenting weighted-average percentile limitation per plan requirement.
- Divide-by-zero guard confirmed at `summary.go:265`: `if totalCount == 0 { return summaryMetrics{} }`.
- Compile-time interface check at `summary.go:32`: `var _ PodLogReader = (*k8sPodLogReader)(nil)`.
- All 6 new operator integration tests present at `operator_test.go:602-756`.
- 26 summary unit tests present in `summary_test.go`.
