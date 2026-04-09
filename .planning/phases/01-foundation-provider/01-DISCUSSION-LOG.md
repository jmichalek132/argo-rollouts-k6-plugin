# Phase 1: Foundation & Provider - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-09
**Phase:** 01-foundation-provider
**Areas discussed:** Provider interface shape, Credential passing, k6 Cloud client, Logging library, Go version, Build tooling

---

## Provider Interface Shape

| Option | Description | Selected |
|--------|-------------|----------|
| Unified | GetRunResult returns all data (state + all metrics) in one struct. Single API call per poll. Simpler for future backends. | ✓ |
| Granular | Separate GetRunStatus + GetMetric(name, aggregation). Multiple API calls, larger interface surface. | |

**User's choice:** Unified

**Follow-up — mid-run metrics:**

| Option | Description | Selected |
|--------|-------------|----------|
| Terminal only | Return metrics only when run is in terminal state. Metric plugin returns Running while active. | |
| Partial metrics live | Return current metric values during active runs. Enables early abort if thresholds breach mid-run. | ✓ |

**User's choice:** Partial metrics live

---

## Credential Passing

| Option | Description | Selected |
|--------|-------------|----------|
| Config struct per call | Stateless provider — PluginConfig (with APIToken, StackID) parsed from AnalysisTemplate JSON each Run call. | ✓ |
| Constructor injection | Provider initialized once with cluster-wide credentials from dedicated Secret. | |

**User's choice:** Config struct per call (stateless)

---

## k6 Cloud Client

| Option | Description | Selected |
|--------|-------------|----------|
| Official Go client | k6-cloud-openapi-client-go v1.7.0-0.1.0, fallback to net/http for uncovered endpoints. | ✓ |
| Hand-rolled HTTP | Thin net/http wrapper over k6 API v2/v5. Full control, more code. | |

**User's choice:** Official Go client with hand-rolled fallback

---

## Logging Library

| Option | Description | Selected |
|--------|-------------|----------|
| log/slog | Go stdlib since 1.21, zero deps, stderr default, JSON output. | ✓ |
| logrus | Battle-tested, k8s ecosystem familiar. Extra dep. | |
| zap | High-performance, used by Argo Rollouts controller itself. More setup. | |

**User's choice:** log/slog

---

## Go Version

| Option | Description | Selected |
|--------|-------------|----------|
| Go 1.22 | Stable, widely available, matches Argo Rollouts v1.9.x toolchain. | ✓ |
| Go 1.23 | Latest stable, may lag CI images. | |
| Go 1.21 | Conservative, older toolchain. | |

**User's choice:** Go 1.22

---

## Build Tooling

| Option | Description | Selected |
|--------|-------------|----------|
| Makefile | Standard in Go/k8s ecosystem. make build/test/lint. | ✓ |
| Taskfile | Modern YAML-based alternative. Less familiar in k8s ecosystem. | |
| Just go commands | No wrapper. Simple, hard to document multi-step workflows. | |

**User's choice:** Makefile

---

## Claude's Discretion

- Mock HTTP server approach for provider unit tests
- Internal error type design
- go.mod dependency management
- Exact Makefile structure beyond required targets
- Context propagation within provider methods

## Deferred Ideas

None.
