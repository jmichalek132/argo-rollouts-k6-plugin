# Phase 10: Documentation & E2E - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-16
**Phase:** 10-documentation-e2e
**Areas discussed:** RBAC & example YAML structure, k6 test script for examples, e2e test strategy, README updates

---

## RBAC & Example YAML Structure

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror canary-full structure | New examples/k6-operator/ with individual YAML files | |
| Single comprehensive example | Fewer files, combined YAML | |
| You decide | Claude picks best structure | ✓ |

**User's choice:** You decide
**Notes:** Claude recommended mirroring canary-full pattern

| Option | Description | Selected |
|--------|-------------|----------|
| Both TestRun + PrivateLoadZone | Complete ClusterRole covering all CRDs | ✓ |
| TestRun only, PLZ separate | Minimal ClusterRole | |
| You decide | Claude picks | |

**User's choice:** Both TestRun + PrivateLoadZone (after confirming PLZ is supported)

| Option | Description | Selected |
|--------|-------------|----------|
| Both step + metric examples | Two Rollout examples showing both patterns | ✓ |
| Step plugin only | Most common use case only | |
| You decide | Claude picks | |

**User's choice:** Both step + metric examples

---

## k6 Test Script for Examples

| Option | Description | Selected |
|--------|-------------|----------|
| Simple script with handleSummary | One script: HTTP GET, thresholds, handleSummary JSON to stdout | ✓ |
| Two variants (simple + advanced) | Simple + multi-scenario with custom summaryTrendStats | |
| You decide | Claude picks | |

**User's choice:** Simple script with handleSummary

---

## e2e Test Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Install k6-operator controller | Real controller in kind, full lifecycle | ✓ |
| CRDs only + status patching | Lighter, deterministic, no real k6 execution | |
| You decide | Claude picks | |

**User's choice:** Install k6-operator controller

| Option | Description | Selected |
|--------|-------------|----------|
| Full path: TestRun → metrics | Validate complete metric extraction path | |
| TestRun lifecycle only | Just verify pass/fail via exit codes | |
| You decide | Claude picks | ✓ |

**User's choice:** You decide
**Notes:** Claude recommended full path validation as milestone capstone

---

## README Updates

| Option | Description | Selected |
|--------|-------------|----------|
| Add k6-operator section to README | Quick-start in main README | |
| Keep docs in examples/ only | Directory-level README, main README links to it | ✓ |
| You decide | Claude picks | |

**User's choice:** Keep docs in examples/ only

## Claude's Discretion

- Example directory structure (recommended: mirror canary-full)
- e2e test depth (recommended: full path validation)
- e2e framework details (k6-operator install, ConfigMap creation, CI timeout)
- Mock target service reuse
- Example YAML annotations style

## Deferred Ideas

None
