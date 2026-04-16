---
phase: 08-k6-operator-provider
asvs_level: 1
verified: "2026-04-16"
threats_total: 10
threats_closed: 10
threats_open: 0
---

# Phase 8 — k6-operator Provider: Security Verification

## Result: SECURED

All 10 threats closed. 6 mitigations verified in implementation. 4 accepted risks documented below.

---

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-08-01 | DoS | mitigate | CLOSED | `internal/provider/config.go:88-90` — `if c.Parallelism < 0 { return fmt.Errorf(...) }` in ValidateK6Operator |
| T-08-02 | EoP | accept | CLOSED | See accepted risks log below |
| T-08-03 | Tampering | accept | CLOSED | See accepted risks log below |
| T-08-04 | Info Disclosure | mitigate | CLOSED | `internal/provider/operator/testrun.go:54` — SHA256 hash input is `namespace/rolloutName/timestamp`, preventing cross-namespace name collision |
| T-08-05 | Spoofing | accept | CLOSED | See accepted risks log below |
| T-08-06 | Tampering | mitigate | CLOSED | `internal/provider/operator/operator.go:217,238` — `runtime.DefaultUnstructuredConverter.ToUnstructured` used for typed->unstructured conversion; TypeMeta set explicitly in `buildTestRun` (testrun.go:127-130) and `buildPrivateLoadZone` (testrun.go:169-172) before conversion |
| T-08-07 | DoS | mitigate | CLOSED | `internal/provider/operator/operator.go:215-252` — single `Create` call per TriggerRun invocation; parallelism validated non-negative at `config.go:88-90`; runner resources are user-configured |
| T-08-08 | Repudiation | mitigate | CLOSED | `internal/provider/operator/operator.go:226-230,246-250,350-356,361-367` — slog.Info on every CRD Create and Delete with name, namespace, resource, provider fields |
| T-08-09 | EoP | accept | CLOSED | See accepted risks log below |
| T-08-10 | Tampering | mitigate | CLOSED | `internal/provider/operator/testrun.go:94-108` — decodeRunID validates 3-part format via SplitN, allowlist check (`testruns` or `privateloadzones`), and slash-in-name rejection |

---

## Accepted Risks Log

### T-08-02 — Runner image provenance (EoP)

- **Component:** `internal/provider/operator/testrun.go` / `buildTestRun`
- **Risk:** Runner image is taken directly from user-supplied `runnerImage` config field in AnalysisTemplate YAML. The plugin does not validate image registry, tag, or digest. A user with write access to AnalysisTemplates could specify a malicious image.
- **Rationale:** Image provenance enforcement is the user's responsibility. Mitigations available at the cluster level include admission controllers (OPA/Gatekeeper policies, Kyverno), image pull policy enforcement, and private registry restrictions. These controls are outside the plugin's scope.
- **Residual risk:** Low. Exploitation requires write access to AnalysisTemplate resources, which is already a privileged RBAC operation.
- **Owner:** Cluster administrator / platform team.

### T-08-03 — Pod status data integrity (Tampering)

- **Component:** `internal/provider/operator/exitcode.go` / `checkRunnerExitCodes`
- **Risk:** Pass/fail determination relies on pod exit codes read from kube-apiserver. If the API server response were tampered with, incorrect pass/fail results could be injected.
- **Rationale:** The kube-apiserver is the authoritative trust boundary. The plugin authenticates via in-cluster service account token (mTLS to API server). Tampering with API server responses requires cluster-level compromise, which is out of scope. The label selector (`app=k6,k6_cr=<name>,runner=true`) is constructed deterministically from the CR name — not from user-controlled input.
- **Residual risk:** Negligible. Requires cluster compromise.
- **Owner:** Cluster administrator.

### T-08-05 — Dynamic client spoofing (Spoofing)

- **Component:** `internal/provider/operator/operator.go` / `GetRunResult`
- **Risk:** The dynamic client could be deceived into reading from a spoofed API server, returning falsified TestRun status.
- **Rationale:** The dynamic client is initialized via `rest.InClusterConfig()` which uses the pod's service account token and CA bundle from `/var/run/secrets/kubernetes.io/serviceaccount/`. mTLS with the CA bundle prevents MITM. Spoofing the kube-apiserver requires cluster compromise.
- **Residual risk:** Negligible. Requires cluster compromise.
- **Owner:** Cluster administrator.

### T-08-09 — Over-permissive delete (EoP)

- **Component:** `internal/provider/operator/operator.go` / `StopRun`
- **Risk:** `StopRun` deletes CRs via the dynamic client. If the service account RBAC grants delete on resources broader than `testruns` and `privateloadzones`, the plugin could be used to delete unintended resources.
- **Rationale:** RBAC scope is the cluster administrator's responsibility. The plugin's runID allowlist (enforced in `decodeRunID`) restricts delete calls to `testruns` and `privateloadzones` GVRs only — it cannot delete arbitrary resource types regardless of RBAC permissions. The minimum required RBAC is documented in the project's deployment instructions.
- **Residual risk:** Low. Plugin-side GVR restriction prevents worst-case abuse even with over-permissive RBAC.
- **Owner:** Cluster administrator.

---

## Unregistered Threat Flags

None. The Phase 8 Plan 02 SUMMARY.md Threat Surface Scan reports no new threat surface beyond the documented threat model.
