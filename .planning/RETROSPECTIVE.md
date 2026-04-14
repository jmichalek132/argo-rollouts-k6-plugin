# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.0 — MVP

**Shipped:** 2026-04-14
**Phases:** 4 | **Plans:** 9 | **Tasks:** 16

### What Was Built
- Provider abstraction layer with Grafana Cloud k6 implementation (4-method interface, 17 unit tests)
- Metric plugin: async Run/Resume lifecycle with 4 metric types, 91.7% coverage
- Step plugin: fire-and-wait lifecycle with timeout management, graceful termination, 89.1% coverage
- GoReleaser multi-arch CI/CD (8 binaries), e2e test suite (4 mock scenarios on kind), 3 example patterns, README + CONTRIBUTING

### What Worked
- **Provider-first architecture**: Building the provider interface before either plugin meant both plugins shared the same tested foundation — zero rework
- **Metric-before-step ordering**: Solving the harder Run/Resume async pattern first made the step plugin straightforward (Phase 3 was fastest)
- **Stateless provider pattern**: Credentials via PluginConfig per call, client created per call — enabled concurrent safety by design, no mutex complexity
- **TDD with mock HTTP server**: httptest.NewServer for provider tests caught API contract issues early; same pattern scaled to e2e mock server
- **Phase execution velocity**: 9 plans in ~33 min total execution — coarse granularity with 2-task plans kept overhead minimal

### What Was Inefficient
- **k6 v5 aggregate API discovery**: Discovered late that k6-cloud-openapi-client-go v6 doesn't expose aggregate metric endpoints — required hand-rolled net/http client for v5 API (Phase 2 research should have caught this earlier)
- **e2e CI workflow gaps**: Missing `kind` install step and `-timeout=15m` — infrastructure assumptions not validated during Phase 4 planning
- **Nyquist validation for Phases 3-4**: Left in draft status — validation contracts written but not fully approved

### Patterns Established
- `internal/provider/provider.go` as the extensibility point — new backends implement 4 methods
- Stateless plugin pattern: all per-request state in Measurement.Metadata (metric) or RpcStepResult.Status (step)
- slog JSON to stderr before Serve() — zero stdout before go-plugin handshake
- golangci-lint v2 with forbidigo catching stdout writes as compile-time enforcement
- `K6_BASE_URL` env var for routing to mock servers in e2e tests
- CGO_ENABLED=0 on build only (not test — race detector needs CGO)

### Key Lessons
1. **Research provider client capabilities before planning phases** — the v5/v6 API gap cost extra implementation in Phase 2 that should have been planned from the start
2. **CI infrastructure assumptions need explicit validation** — "ubuntu-latest has kind" was wrong; add install steps for all non-standard tools
3. **Stateless design pays off compound interest** — no mutable state meant no concurrency bugs, simpler tests, and trivial binary wiring
4. **Two binaries > one binary with dispatch** — separate MagicCookieValue per plugin type made the controller's trust model explicit and registration unambiguous

### Cost Observations
- Model mix: primarily opus for execution, sonnet for verification/integration checks
- Total execution: ~33 min across 9 plans (avg ~3.7 min/plan)
- Notable: coarse granularity (2-task plans) minimized overhead while keeping commits atomic

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Tasks | Key Pattern |
|-----------|--------|-------|-------|-------------|
| v1.0 | 4 | 9 | 16 | Provider-first architecture, TDD with mock HTTP |

### Cumulative Quality

| Milestone | Unit Tests | Coverage (metric/step) | e2e Scenarios |
|-----------|-----------|------------------------|---------------|
| v1.0 | 53+ | 91.7% / 89.1% | 4 mock |

### Top Lessons (Verified Across Milestones)

1. Research API client capabilities thoroughly before planning — hidden gaps create unplanned work
2. Stateless plugin design enables concurrent safety and simplifies testing
3. CI workflows need explicit tool installation — never assume runner images have what you need
