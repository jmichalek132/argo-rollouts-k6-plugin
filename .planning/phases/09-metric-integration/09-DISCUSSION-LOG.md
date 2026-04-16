# Phase 9: Metric Integration - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md -- this log preserves the alternatives considered.

**Date:** 2026-04-16
**Phase:** 09-metric-integration
**Areas discussed:** handleSummary delivery, Log parsing strategy, Fallback behavior, Metric parity, Multi-pod aggregation, Pod log retrieval limits, GetRunResult integration

---

## handleSummary Delivery

| Option | Description | Selected |
|--------|-------------|----------|
| Require in user's script | User adds handleSummary() to their k6 script. Standard k6 pattern, zero magic, well-documented. | ✓ |
| Auto-inject wrapper script | Plugin generates wrapper script importing user's script + handleSummary. No script changes, but complex/fragile. | |
| Use --summary-export flag | Auto-append --summary-export /dev/stdout. Simple but deprecated in k6 v0.47.0. | |

**User's choice:** Require in user's script
**Notes:** Standard k6 pattern, zero version dependencies

---

## Log Parsing Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| JSON structure detection | Scan pod logs from end for JSON with expected keys ("metrics", "root_group"). No markers needed. | ✓ |
| Marker-based delimiters | Require K6_SUMMARY_START/END markers. 100% reliable but adds friction. | |
| Separate stderr stream | Output JSON to stderr. Clean separation but k6 logs warnings to stderr too. | |

**User's choice:** JSON structure detection
**Notes:** handleSummary runs last, JSON near tail of output

---

## Fallback Behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Graceful per-metric | Thresholds always work (exit codes). Other metrics return zero + warning. No error. | ✓ |
| Error for detailed metrics | Thresholds work. Other metrics error if handleSummary missing. | |
| Error for all metrics | All metrics error if handleSummary missing. | |

**User's choice:** Graceful per-metric
**Notes:** Users can start with thresholds only, add handleSummary later for richer metrics

---

## Metric Parity

| Option | Description | Selected |
|--------|-------------|----------|
| Strict parity | Same 4 metrics as Grafana Cloud. Provider-switching without AnalysisTemplate changes. | ✓ |
| Extended set now | Add more handleSummary metrics (iteration_duration, vus_max, etc.). Breaks parity. | |
| Parity + escape hatch | 4 standard + generic custom metric type for any handleSummary path. | |

**User's choice:** Strict parity
**Notes:** Extended metrics are a future phase

---

## Multi-Pod Aggregation

| Option | Description | Selected |
|--------|-------------|----------|
| Aggregate across all pods | Read handleSummary from every pod, merge metrics. Accurate for distributed runs. | ✓ |
| Use first pod only | Read from index-0 pod. Simpler but inaccurate for distributed runs. | |
| Error for parallelism > 1 | Detailed metrics only for single-pod. Zero + warning for parallel. | |

**User's choice:** Aggregate across all pods
**Notes:** Matches Grafana Cloud's distributed aggregation behavior

---

## Pod Log Retrieval Limits

| Option | Description | Selected |
|--------|-------------|----------|
| Tail with byte limit | TailLines + LimitBytes on pod log request. ~100 lines or 64KB per pod. | ✓ |
| Full logs, parse in-memory | Read entire logs. Unbounded memory for verbose tests. | |
| You decide | Claude picks based on API and typical output sizes. | |

**User's choice:** Tail with byte limit
**Notes:** Bounded memory, handleSummary is at the end of output

---

## GetRunResult Integration

| Option | Description | Selected |
|--------|-------------|----------|
| Separate file (summary.go) | New file for parsing. Called from GetRunResult after exit code check. Follows exitcode.go pattern. | ✓ |
| Inline in operator.go | Add parsing directly in GetRunResult. One place but mixes concerns. | |
| You decide | Claude picks based on codebase patterns. | |

**User's choice:** Separate file (summary.go)
**Notes:** Follows existing pattern: exitcode.go for pass/fail, summary.go for detailed metrics

---

## Claude's Discretion

- JSON detection heuristic implementation details
- Aggregation math for multi-pod percentile merging
- Pod log reader helper function signature
- handleSummary JSON struct definition (typed vs. map)
- slog field names and warning messages

## Deferred Ideas

None -- discussion stayed within phase scope
