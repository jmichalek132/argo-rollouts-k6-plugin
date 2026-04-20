---
phase: 9
reviewers: [codex]
reviewed_at: 2026-04-16
plans_reviewed: [09-01-PLAN.md, 09-02-PLAN.md]
---

# Cross-AI Plan Review — Phase 9

## Codex Review

### Plan 09-01: handleSummary JSON Parsing Engine

#### Summary
This plan is directionally solid and covers the core Phase 9 problem: extracting provider-neutral metrics from in-cluster k6 execution artifacts. It is well-scoped around a separable parsing/aggregation layer and correctly reflects the research constraints around `handleSummary()` shape and Kubernetes log testability. The main risks are around ambiguity in summary discovery, aggregation correctness, and whether the parser's contract is strict enough to preserve parity with the Grafana Cloud provider without introducing silent metric distortion.

#### Strengths
- Clean separation of concerns by isolating log parsing in `summary.go`, matching D-07.
- Good testability choice with `PodLogReader`, especially given the fake clientset `GetLogs()` limitation.
- Scope aligns with Phase 9 goals and does not appear to require unnecessary changes to metric plugin core code.
- Correctly accounts for k6 summary key realities like `p(95)` and `med`.
- Includes explicit multi-pod aggregation strategy, which is necessary for distributed runs.
- Log bounding via `TailLines` and `LimitBytes` is a practical safeguard.
- TDD approach is appropriate here because parsing behavior is edge-case-heavy.

#### Concerns
- **HIGH**: `findSummaryJSON` is described as validating on `"metrics"` only, but user decision D-02 says discovery should look for expected top-level keys including both `"metrics"` and `"root_group"`. Relaxing this may increase false positives from arbitrary logged JSON.
- **HIGH**: Mapping `p(99)` into `RunResult` seems inconsistent with the research note that `p(99)` is not present by default. If `RunResult` exposes P99, the plan needs explicit behavior for absent values, otherwise parity may be inconsistent or misleading.
- **HIGH**: Weighted-average aggregation is not obviously valid for percentile metrics like p95/p99. A weighted average of pod-level percentiles is generally not the true global percentile unless there is a very specific data model behind it.
- **MEDIUM**: The plan says "threshold results" are part of success criteria, but the deliverables do not clearly explain how threshold pass/fail data is extracted, normalized, and exposed into `RunResult`.
- **MEDIUM**: Pod discovery is underspecified. It does not say which pods are considered runner pods, how label selectors are built, or how stale/previous pods are excluded.
- **MEDIUM**: Scanning logs "from the end" is sensible, but the plan does not define behavior when multiple JSON objects exist near the tail, including non-summary JSON.
- **MEDIUM**: The threat model mentions JSON injection mitigation via `"metrics"` validation, but that is too weak if any application log line can emit a JSON object with that key.
- **LOW**: Typed structs may become brittle if k6 emits extra fields or partial objects; the plan should confirm tolerant decoding behavior.

#### Suggestions
- Tighten summary identification to require both `"metrics"` and `"root_group"` and preferably successful decoding into the expected typed shape before accepting a candidate object.
- Define explicit behavior for missing `p(99)` and any other absent trend stats: either zero, omitted, or "not available," and make that consistent with cloud-provider semantics.
- Revisit percentile aggregation. If exact recomputation is impossible from available summary data, document that distributed percentile aggregation is approximate and justify the chosen method, or constrain percentile support for distributed runs.
- Add explicit deliverables for threshold extraction and mapping, not just trend metrics.
- Specify runner pod discovery rules and filtering criteria, including namespace, labels, phase/state expectations, and handling of multiple generations.
- Add tests for multiple JSON blobs in logs, truncated JSON at tail, logs with structured JSON before the summary, and pods returning empty logs.
- Clarify whether parsing failure on one pod in a multi-pod run skips that pod, fails aggregation, or degrades individual metrics only.

#### Risk Assessment
**MEDIUM-HIGH**. The plan is well-structured, but correctness risk is meaningful, especially around percentile aggregation and summary detection.

---

### Plan 09-02: Wire into GetRunResult

#### Summary
This plan is appropriately narrow and probably the right second wave after the parser exists. It avoids unnecessary churn by reusing existing `RunResult` extraction paths and only populating fields for terminal states. The main weakness is that it assumes the semantics from Wave 1 are already correct.

#### Strengths
- Good dependency ordering: parser/aggregation first, wiring second.
- Minimal integration scope in `operator.go` reduces regression surface.
- `WithLogReader` is a strong injection seam for unit tests.
- Terminal-state-only parsing is a sensible performance guard.
- Reusing the existing `RunResult` path supports parity with the cloud provider and avoids metric plugin churn.
- Explicit graceful degradation matches D-05 and avoids hard-failing every rollout on missing summary output.

#### Concerns
- **HIGH**: "Calls `parseSummaryFromPods` for terminal (Passed/Failed) states" may be too narrow if other terminal/error-like states can still carry useful summary data or need explicit handling.
- **HIGH**: Graceful degradation to zero values can hide configuration errors such as missing `handleSummary()`. Without strong warning semantics or surfaced reason fields, users may silently evaluate bad success conditions.
- **MEDIUM**: Backward compatibility is mentioned, but the plan does not state whether existing step-plugin behavior changes for users who do not export `handleSummary()`.
- **MEDIUM**: It is unclear whether repeated `GetRunResult` polling after terminal state will re-read and re-parse logs every interval, which could be wasteful.
- **MEDIUM**: The plan says it mirrors `cloud.go populateAggregateMetrics`, but does not state whether all four metric families are populated with identical naming/semantics.
- **LOW**: Logging via `slog.Warn` is useful, but the plan does not define structured fields.

#### Suggestions
- Enumerate all provider states and define exactly which ones trigger summary parsing.
- Cache parsed summary results after terminal completion if `GetRunResult` may be called repeatedly for the same run.
- Make degraded-mode warnings highly structured: include run identifier, namespace, pod count, affected metric families, and parse error cause.
- Add tests for repeated `GetRunResult` calls after completion to ensure no excessive log reads or inconsistent outputs.
- Add an integration-focused test proving parity with Grafana Cloud semantics for the same `RunResult` fields.
- Consider whether zero-on-missing should be distinguishable from real zero values.

#### Risk Assessment
**MEDIUM**. The wiring itself is straightforward, but operational correctness depends heavily on Wave 1 behavior and on whether degraded mode is observable enough.

---

## Consensus Summary

### Agreed Strengths
- Clean two-wave decomposition with correct dependency ordering
- Strong testability design (PodLogReader interface, TDD approach)
- Minimal blast radius — no changes to metric plugin core (metric.go)
- Good alignment with user decisions (D-01 through D-07) and roadmap goals
- Practical log bounding via TailLines/LimitBytes

### Agreed Concerns
- **HIGH**: JSON detection should validate both `"metrics"` AND `"root_group"` keys per D-02 (currently only validates `"metrics"`)
- **HIGH**: Weighted-average percentile aggregation for distributed runs is an approximation that needs explicit documentation/justification
- **HIGH**: Graceful degradation to zero values can mask misconfiguration (missing handleSummary) — needs stronger observability
- **MEDIUM**: Threshold extraction/mapping not explicitly addressed in plan deliverables
- **MEDIUM**: Repeated GetRunResult polling may re-parse pod logs unnecessarily
- **MEDIUM**: Missing test cases for multiple JSON objects in logs, truncated JSON, empty logs

### Divergent Views
None — single reviewer.
