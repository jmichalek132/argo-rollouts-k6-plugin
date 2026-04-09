---
phase: 01-foundation-provider
plan: 02
subsystem: build
tags: [go, go-plugin, makefile, golangci-lint, static-binary, slog]

# Dependency graph
requires:
  - phase: 01-foundation-provider/01
    provides: Go module, Provider interface, GrafanaCloudProvider
provides:
  - Metric plugin binary entrypoint (cmd/metric-plugin/main.go) with go-plugin handshake
  - Step plugin binary entrypoint (cmd/step-plugin/main.go) with go-plugin handshake
  - Makefile with build/test/lint/lint-stdout/clean targets
  - golangci-lint v2 config with forbidigo stdout detection
  - Static CGO_ENABLED=0 binaries in bin/
affects: [02-metric-plugin, 03-step-plugin, 04-release]

# Tech tracking
tech-stack:
  added: [hashicorp/go-plugin v1.6.3, golangci-lint v2.1.6]
  patterns: [slog-json-stderr-logging, LOG_LEVEL-env-var, stdout-ok-exemption, CGO_ENABLED-0-static-build]

key-files:
  created:
    - cmd/metric-plugin/main.go
    - cmd/step-plugin/main.go
    - Makefile
    - .golangci.yml
    - .gitignore
  modified:
    - go.mod
    - go.sum
    - internal/provider/cloud/cloud.go
    - internal/provider/cloud/cloud_test.go

key-decisions:
  - "slog JSON handler to stderr before Serve() -- zero stdout before handshake (DIST-04)"
  - "LOG_LEVEL env var with debug/warn/error/info(default) per D-13"
  - "golangci-lint v2 with forbidigo pattern for fmt.Print and os.Stdout"
  - "// stdout-ok comment exemption for grep-based lint-stdout target"

patterns-established:
  - "slog stderr logging: setupLogging() in main() before plugin.Serve()"
  - "CGO_ENABLED=0 static builds with -ldflags '-s -w' for stripped binaries"
  - "lint-stdout Makefile target: grep-based stdout detection in non-test Go files"
  - "golangci-lint v2 config: version: '2', linters.default: standard, forbidigo for stdout"

requirements-completed: [DIST-01, DIST-04]

# Metrics
duration: 4min
completed: 2026-04-09
---

# Phase 1 Plan 2: Binary Entrypoints & Build Pipeline Summary

**Two static plugin binaries with go-plugin handshake (metricprovider/step), Makefile build pipeline, and golangci-lint v2 with forbidigo stdout detection**

## Performance

- **Duration:** 4 min
- **Started:** 2026-04-09T21:45:41Z
- **Completed:** 2026-04-09T21:50:19Z
- **Tasks:** 2
- **Files modified:** 9

## Accomplishments
- Two binary stubs compile with CGO_ENABLED=0: metric-plugin (MagicCookieValue "metricprovider") and step-plugin (MagicCookieValue "step")
- slog JSON handler configured to stderr in both binaries with LOG_LEVEL env var support (D-12, D-13)
- Makefile with all required targets: build, test (-race), lint (golangci-lint), lint-stdout (grep-based), clean
- golangci-lint v2 config with forbidigo catching fmt.Print and os.Stdout usage, plus bodyclose, errcheck, exhaustive, and other standard linters

## Task Commits

Each task was committed atomically:

1. **Task 1: Create binary stubs with go-plugin handshake and slog stderr logging** - `2971b04` (feat)
2. **Task 2: Create Makefile and golangci-lint configuration with stdout detection** - `c5dd88b` (chore)

## Files Created/Modified
- `cmd/metric-plugin/main.go` - Metric plugin binary entrypoint with go-plugin handshake (MagicCookieValue: "metricprovider")
- `cmd/step-plugin/main.go` - Step plugin binary entrypoint with go-plugin handshake (MagicCookieValue: "step")
- `Makefile` - Build pipeline with build/test/lint/lint-stdout/clean targets, CGO_ENABLED=0
- `.golangci.yml` - golangci-lint v2 configuration with forbidigo stdout detection
- `.gitignore` - Excludes bin/ directory from version control
- `go.mod` - Added hashicorp/go-plugin v1.6.3 dependency
- `go.sum` - Updated dependency checksums
- `internal/provider/cloud/cloud.go` - Fixed bodyclose: close HTTP response bodies from k6 API client
- `internal/provider/cloud/cloud_test.go` - Fixed errcheck: handle return values in test HTTP handlers

## Decisions Made
- slog JSON handler to stderr before Serve() -- ensures zero stdout output before go-plugin handshake (DIST-04)
- LOG_LEVEL env var supports debug/warn/warning/error with info as default (D-13)
- golangci-lint v2 format with `version: "2"` and `linters.default: standard` per research finding #10
- `// stdout-ok` comment marker for exempting documentation comments from lint-stdout grep check

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed bodyclose lint failures in cloud.go**
- **Found during:** Task 2 (make lint verification)
- **Issue:** golangci-lint bodyclose linter flagged 3 unclosed HTTP response bodies from k6 API client Execute() calls
- **Fix:** Captured response as named variable, added `if resp != nil && resp.Body != nil { _ = resp.Body.Close() }` before error check
- **Files modified:** internal/provider/cloud/cloud.go
- **Verification:** make lint passes with 0 issues
- **Committed in:** c5dd88b (Task 2 commit)

**2. [Rule 3 - Blocking] Fixed errcheck lint failures in cloud_test.go**
- **Found during:** Task 2 (make lint verification)
- **Issue:** golangci-lint errcheck linter flagged 5 unchecked return values from json.Encode() and w.Write() in test HTTP handlers
- **Fix:** Added `_ =` prefix to json.NewEncoder().Encode() calls and `_, _ =` to w.Write() call
- **Files modified:** internal/provider/cloud/cloud_test.go
- **Verification:** make lint passes with 0 issues
- **Committed in:** c5dd88b (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 3 - blocking lint failures from pre-existing code)
**Impact on plan:** Both fixes necessary for make lint to pass. No scope creep -- these are correctness improvements to Plan 01 code discovered by the new linter configuration.

## Issues Encountered
None beyond the lint fixes documented above.

## User Setup Required
None - no external service configuration required.

## Known Stubs
None - binary entrypoints are intentionally Phase 1 stubs (empty plugin map). Phase 2 will add RpcMetricProviderPlugin and Phase 3 will add RpcStepPlugin. This is by design, not a missing data source.

## Next Phase Readiness
- Both binaries compile and pass all lint checks, ready for Phase 2 (metric plugin implementation) and Phase 3 (step plugin)
- Makefile build pipeline established for all future development
- golangci-lint configured to catch stdout writes automatically in all future code

## Self-Check: PASSED

All 9 files verified present. Both commits (2971b04, c5dd88b) verified in git log.

---
*Phase: 01-foundation-provider*
*Completed: 2026-04-09*
