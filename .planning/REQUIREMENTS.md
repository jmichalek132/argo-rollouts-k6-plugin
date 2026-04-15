# Requirements: argo-rollouts-k6-plugin

**Defined:** 2026-04-15
**Core Value:** Rollouts automatically pass or roll back based on real load test results — no manual gates, no guesswork.

## v0.3.0 Requirements

Requirements for in-cluster k6 execution via k6-operator. Each maps to roadmap phases.

### Foundation

- [ ] **FOUND-01**: Plugin config accepts a `provider` field that routes to the correct execution backend (grafana-cloud default for backward compat, k6-operator)
- [ ] **FOUND-02**: Plugin initializes a Kubernetes client via `rest.InClusterConfig()` in InitPlugin, promoted from indirect to direct dependency
- [ ] **FOUND-03**: Plugin reads k6 .js script content from a Kubernetes ConfigMap referenced by name and key in plugin config

### k6-operator Provider

- [ ] **K6OP-01**: Plugin creates a `TestRun` CR (k6.io/v1alpha1) from plugin config and polls stage until terminal state
- [ ] **K6OP-02**: Plugin extracts pass/fail result from runner pod exit codes after TestRun reaches `finished` stage (workaround for k6-operator issue #577)
- [ ] **K6OP-03**: Plugin supports namespace targeting for TestRun CR creation (defaults to rollout namespace)
- [ ] **K6OP-04**: Plugin supports parallelism configuration for distributed k6 execution
- [ ] **K6OP-05**: Plugin supports resource requests/limits on k6 runner pods
- [ ] **K6OP-06**: Plugin applies consistent naming (`k6-<rollout>-<hash>`) and labels (`app.kubernetes.io/managed-by`) to TestRun CRs
- [ ] **K6OP-07**: Plugin supports custom k6 runner image and environment variable injection
- [ ] **K6OP-08**: Plugin deletes TestRun CR on rollout abort/terminate to stop running k6 pods

### Metric Integration

- [ ] **METR-01**: Metric plugin extracts metric values (p95, error rate, throughput, threshold results) from k6 `handleSummary()` JSON in runner pod logs
- [ ] **METR-02**: Metric plugin works with k6-operator provider for `successCondition` evaluation, not just step plugin

### Documentation

- [ ] **DOCS-01**: RBAC example ClusterRole covering k6.io TestRun CRDs, pods, pods/log, and configmaps
- [ ] **DOCS-02**: Example AnalysisTemplate and Rollout YAML for k6-operator provider (step + metric)

### Testing

- [ ] **TEST-01**: e2e test suite on kind cluster with k6-operator CRDs installed, validating TestRun creation and result extraction against mock target service

## Future Requirements

Deferred to future releases. Tracked but not in current roadmap.

### Kubernetes Job Provider

- **JOB-01**: Plugin creates a `batch/v1` Job with k6 container and ConfigMap volume mount
- **JOB-02**: Plugin polls Job status and extracts result from container exit code
- **JOB-03**: Plugin cleans up completed Jobs with background propagation policy

### Other

- **SEC-01**: Step plugin secret handling via secretKeyRef (upstream Argo Rollouts limitation)
- **CUST-01**: Custom k6 metric support (user-defined Counter/Gauge/Rate/Trend)
- **SCRIPT-01**: PersistentVolume script sourcing for scripts > 1 MiB
- **SCRIPT-02**: Multi-file k6 module import support

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Local binary execution (subprocess) | Anti-feature: corrupts go-plugin stdout protocol (grafana/k6#3744), unbounded resource consumption in controller pod, no process isolation. k6-operator covers all in-cluster use cases safely. |
| Inline script in plugin config | YAML escaping nightmares, no syntax highlighting, no version control. ConfigMap is the right abstraction. |
| Real-time metric streaming from in-cluster k6 | End-of-test handleSummary JSON is sufficient. For real-time, users configure `k6 --out prometheus` and use Argo Rollouts' Prometheus metric provider. |
| VU/duration override in plugin config | Creates divergence from k6 script definition — same rationale as v1.0. |
| Automatic k6-operator installation | User's responsibility. Plugin fails gracefully with clear error if CRDs missing. |
| Distributed execution without k6-operator | Reimplementing execution segments is exactly what k6-operator does. Use k6-operator provider. |
| PersistentVolume script sourcing | ConfigMaps cover 95%+ of k6 scripts (< 1 MiB). Users needing PVC can use k6-operator directly. |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| FOUND-01 | Phase 7 | Pending |
| FOUND-02 | Phase 7 | Pending |
| FOUND-03 | Phase 7 | Pending |
| K6OP-01 | Phase 8 | Pending |
| K6OP-02 | Phase 8 | Pending |
| K6OP-03 | Phase 8 | Pending |
| K6OP-04 | Phase 8 | Pending |
| K6OP-05 | Phase 8 | Pending |
| K6OP-06 | Phase 8 | Pending |
| K6OP-07 | Phase 8 | Pending |
| K6OP-08 | Phase 8 | Pending |
| METR-01 | Phase 9 | Pending |
| METR-02 | Phase 9 | Pending |
| DOCS-01 | Phase 10 | Pending |
| DOCS-02 | Phase 10 | Pending |
| TEST-01 | Phase 10 | Pending |

**Coverage:**
- v0.3.0 requirements: 16 total
- Mapped to phases: 16
- Unmapped: 0

---
*Requirements defined: 2026-04-15*
*Last updated: 2026-04-15 after roadmap creation*
