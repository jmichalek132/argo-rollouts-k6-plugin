---
phase: 02-metric-plugin
plan: 02
subsystem: metric-plugin
tags: [argo-rollouts, k6, metric-provider, go-plugin, rpc, binary-wiring]

# Dependency graph
requires:
  - phase: 02-metric-plugin-plan-01
    provides: K6MetricProvider implementation, GrafanaCloudProvider, metric.New() constructor
  - phase: 01-foundation-provider
    provides: go-plugin Serve() stub in cmd/metric-plugin/main.go, handshakeConfig, setupLogging
provides:
  - Fully wired metric-plugin binary with RpcMetricProviderPlugin registered
  - Complete metric plugin ready for e2e testing against Argo Rollouts controller
affects: [04-e2e-testing, 03-step-plugin]

# Tech tracking
tech-stack:
  added: []
  patterns: [provider-to-plugin-wiring, root-anchored-gitignore]

key-files:
  modified:
    - cmd/metric-plugin/main.go
    - .gitignore

key-decisions:
  - "Provider instantiated at binary startup (not per-RPC-call) -- GrafanaCloudProvider is stateless so safe to share"
  - "Fixed .gitignore to use root-anchored patterns (/metric-plugin instead of metric-plugin) to avoid matching cmd/ subdirectories"

patterns-established:
  - "Binary wiring pattern: cloud.NewGrafanaCloudProvider() -> metric.New(p) -> RpcMetricProviderPlugin{Impl: impl} in Serve()"

requirements-completed: [PLUG-01]

# Metrics
duration: 2min
completed: 2026-04-10
---

# Phase 02 Plan 02: Wire K6MetricProvider into metric-plugin binary

**Metric-plugin binary fully wired with GrafanaCloudProvider -> K6MetricProvider -> RpcMetricProviderPlugin, both binaries compile statically, 91.7% test coverage**

## Performance

- **Duration:** 2 min
- **Started:** 2026-04-10T08:16:44Z
- **Completed:** 2026-04-10T08:18:19Z
- **Tasks:** 1
- **Files modified:** 2

## Accomplishments
- cmd/metric-plugin/main.go now creates GrafanaCloudProvider, wraps it in K6MetricProvider, and registers as "RpcMetricProviderPlugin" in go-plugin Serve()
- Both binaries (metric-plugin and step-plugin) compile with CGO_ENABLED=0
- make lint passes clean (0 issues)
- All 53 tests pass with -race flag (27 metric + 26 cloud)
- internal/metric/ test coverage at 91.7%

## Task Commits

Each task was committed atomically:

1. **Task 1: Wire K6MetricProvider into metric-plugin binary and verify full pipeline** - `fddb2fe` (feat)

## Files Created/Modified
- `cmd/metric-plugin/main.go` - Added imports for rolloutsPlugin, metric, cloud; created provider chain; registered RpcMetricProviderPlugin with non-empty Impl
- `.gitignore` - Fixed root-anchored patterns to avoid matching cmd/ subdirectory paths

## Decisions Made
- Provider instantiated once at binary startup rather than per-RPC-call. GrafanaCloudProvider is stateless (creates k6 API client per call internally), so sharing the instance is safe and avoids unnecessary allocations.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed .gitignore patterns matching cmd/ subdirectories**
- **Found during:** Task 1 (git add failed for cmd/metric-plugin/main.go)
- **Issue:** `.gitignore` entry `metric-plugin` matched both the root binary AND the `cmd/metric-plugin/` directory, preventing `git add cmd/metric-plugin/main.go`
- **Fix:** Changed to root-anchored patterns: `/metric-plugin` and `/step-plugin`
- **Files modified:** .gitignore
- **Verification:** `git add cmd/metric-plugin/main.go` succeeds
- **Committed in:** fddb2fe (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary to commit the main change. No scope creep.

## Issues Encountered
None beyond the .gitignore fix documented above.

## User Setup Required

None - no external service configuration required.

## Known Stubs

None. The metric-plugin binary is fully wired with real provider and implementation.

## Next Phase Readiness
- Metric plugin binary is complete and ready for Phase 4 e2e testing against a real Argo Rollouts controller
- Provider interface unchanged -- step plugin (Phase 3) can use the same GrafanaCloudProvider
- All existing tests still pass (backward compatible)

## Self-Check: PASSED

- [x] cmd/metric-plugin/main.go exists
- [x] Commit fddb2fe exists
- [x] RpcMetricProviderPlugin registration present in main.go

---
*Phase: 02-metric-plugin*
*Completed: 2026-04-10*
