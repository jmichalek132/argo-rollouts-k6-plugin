# Phase 8: k6-operator Provider - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-15
**Phase:** 08-k6-operator-provider
**Areas discussed:** TestRun CRD strategy, Polling & result detection, Config extension surface, Cleanup & abort handling

---

## TestRun CRD Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Import k6-operator types | Typed TestRun structs, compile-time safety, adds k6-operator dependency | ✓ |
| Unstructured client | No dependency, no type safety, string-based field names | |
| Handcrafted types | Local structs, no external dep, manual sync with CRD changes | |

**User's choice:** Import k6-operator types
**Notes:** None

### CRD Version

| Option | Description | Selected |
|--------|-------------|----------|
| v1alpha1 only | Current stable CRD, simple | |
| PrivateLoadZone CRD | Newer Grafana Cloud integration, requires cloud connectivity | |
| Both | Support both TestRun and PrivateLoadZone | ✓ |

**User's choice:** Both TestRun and PrivateLoadZone
**Notes:** User wants to support both CRDs

### CRD Selection Mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Config field selects CRD | Explicit crdKind field in PluginConfig | |
| Auto-detect from config | Infer from presence of Grafana Cloud credentials | ✓ |
| Just auto-detect, no override | Pure auto-detect, no override field | ✓ |

**User's choice:** Auto-detect with no override
**Notes:** User preferred simplicity — auto-detect is intuitive

---

## Polling & Result Detection

| Option | Description | Selected |
|--------|-------------|----------|
| Piggyback on controller polling | Single GET per call, no internal loop | ✓ |
| Watch/informer | Real-time updates via K8s watch | |

**User's choice:** Piggyback on controller polling
**Notes:** User asked for tradeoff analysis. Claude explained that the plugin is already poll-driven by the Argo Rollouts controller, making watches unnecessary complexity.

### Pass/Fail Detection

| Option | Description | Selected |
|--------|-------------|----------|
| Runner pod exit codes | Check exit codes after TestRun finishes, 0=pass, non-zero=fail | ✓ |
| Runner pod logs (handleSummary) | Parse k6 JSON output from logs | |
| Both: exit code now, logs in Phase 9 | Clean separation of concerns | |

**User's choice:** Runner pod exit codes
**Notes:** None

---

## Config Extension Surface

| Option | Description | Selected |
|--------|-------------|----------|
| Essential fields only | Parallelism, resources, runner image, env vars, arguments | ✓ |
| Full k6-operator surface | Most TestRunSpec fields exposed | |
| Minimal + passthrough | Essential fields + raw JSON override | |

**User's choice:** Essential fields only
**Notes:** Covers 90% of use cases

### Namespace Handling

| Option | Description | Selected |
|--------|-------------|----------|
| Auto-inject from rollout | Read from AnalysisRun/Rollout ObjectMeta, override via config | ✓ |
| Always explicit | User must set namespace in config | |

**User's choice:** Auto-inject from rollout
**Notes:** None

---

## Cleanup & Abort Handling

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit delete on abort/terminate | Plugin calls Delete on StopRun | ✓ |
| Owner references | AnalysisRun as owner, K8s GC auto-deletes | ✓ |
| TTL-based cleanup | Background TTL-based cleanup | |

**User's choice:** Explicit delete (primary) + owner references (safety net)
**Notes:** User wanted both — explicit delete as primary mechanism, owner refs as safety net for crash orphans. If AnalysisRun UID unavailable, fall back to label-based identification.

## Claude's Discretion

- TestRun spec construction helpers
- Pod label selector construction
- Error message wording
- Internal state management between TriggerRun/GetRunResult/StopRun calls

## Deferred Ideas

None
