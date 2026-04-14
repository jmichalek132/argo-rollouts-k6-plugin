# Requirements: argo-rollouts-k6-plugin

**Defined:** 2026-04-15
**Core Value:** Rollouts automatically pass or roll back based on real load test results -- no manual gates, no guesswork.

## v0.2.0 Requirements

### CI/CD

- [ ] **CI-01**: e2e GitHub Actions workflow installs `kind` binary before running tests
- [ ] **CI-02**: e2e GitHub Actions workflow uses `-timeout=15m` matching Makefile's `test-e2e` target

### Dependency Management

- [ ] **DEPS-01**: Renovate bot configured with `renovate.json` for automated Go module dependency updates
- [ ] **DEPS-02**: Renovate bot configured for automated GitHub Actions dependency updates (actions/checkout, actions/setup-go, goreleaser, golangci-lint)

## Future Requirements

Deferred to later milestones. Tracked but not in current roadmap.

### Provider Extensions

- **PROV2-01**: In-cluster k6 Job execution via KubernetesJobProvider
- **PROV2-02**: ConfigMap-based k6 script sourcing
- **PROV2-03**: Direct k6 binary execution via LocalBinaryProvider

### Metric Enhancements

- **METR2-01**: Custom k6 metric support (user-defined Counter/Gauge/Rate/Trend)
- **METR2-02**: Structured multi-metric result object
- **METR2-03**: Automatic test run ID resolution

### Step Enhancements

- **STEP2-01**: Combined trigger+monitor workflow (step plugin passes runId to metric plugin)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Test script sourcing from URL or git ref | Network reliability concerns, git auth complexity |
| Grafana dashboards or alerting configuration | Separate concern |
| Non-Grafana k6 execution backends | Different auth and endpoint structure |
| Helm chart or Kubernetes operator | Plugins are binaries registered via ConfigMap |
| Step plugin secretKeyRef support | Upstream Argo Rollouts limitation -- not addressable in plugin code |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| CI-01 | - | Pending |
| CI-02 | - | Pending |
| DEPS-01 | - | Pending |
| DEPS-02 | - | Pending |

**Coverage:**
- v0.2.0 requirements: 4 total
- Mapped to phases: 0
- Unmapped: 4

---
*Requirements defined: 2026-04-15*
*Last updated: 2026-04-15 after initial definition*
