---
phase: "04"
slug: release-examples
status: secured
threats_open: 0
asvs_level: 1
created: 2026-04-14
---

# Phase 04 Security Audit — release-examples

## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Plugin binary ↔ Controller | go-plugin net/rpc over localhost stdio; handshake cookie enforced |
| Plugin binary ↔ Grafana Cloud k6 API | HTTPS; Bearer token from K8s Secret (metric plugin) or inline config (step plugin) |
| CI/CD ↔ GitHub | GITHUB_TOKEN with minimal scope (contents: write for release, contents: read for CI/e2e) |
| Release binary ↔ User | SHA256 checksums published alongside binaries in GitHub Release |
| K6_BASE_URL override ↔ mock server | Test-only env var; no validation of the URL value itself (accepted risk, see AR-02) |

## Threat Register

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-04-01 | Binary integrity (supply-chain) | mitigate | CLOSED | `.goreleaser.yaml:40-42` — `algorithm: sha256`, produces `checksums.txt` alongside every release binary |
| T-04-02 | Credential exposure in examples | mitigate | CLOSED | `examples/threshold-gate/secret.yaml:13-14` — placeholder `<YOUR_API_TOKEN>` / `<YOUR_STACK_ID>`; `examples/canary-full/rollout.yaml:44-46` — placeholders with production warning comment at line 12-14 |
| T-04-03 | GitHub Actions token scope | mitigate | CLOSED | `.github/workflows/ci.yml:9` — `permissions: contents: read`; `.github/workflows/e2e.yml:9` — `permissions: contents: read`; `.github/workflows/release.yml:8-9` — `permissions: contents: write` (minimum required for goreleaser upload) |
| T-04-04 | K6_BASE_URL override redirecting API calls | accept | CLOSED | See AR-02 in Accepted Risks Log |
| T-04-05 | URL injection via unsanitized metric params | mitigate | CLOSED | `internal/provider/cloud/metrics.go:31-44` — `isValidMetricParam()` rejects any character outside `[a-zA-Z0-9_.]()` before URL interpolation; validation at lines 54-59 |
| T-04-06 | Unbounded response body DoS | mitigate | CLOSED | `internal/provider/cloud/metrics.go:19-20` — `maxResponseBodySize = 1 << 20`; applied via `io.LimitReader` at lines 91 and 95 |
| T-04-07 | API token visible in step plugin config (no secretKeyRef) | accept | CLOSED | See AR-01 in Accepted Risks Log |
| T-04-08 | API token logging / metadata leakage | mitigate | CLOSED | `internal/provider/cloud/cloud.go:58` — token set as context value only, never passed to slog; `internal/provider/cloud/metrics.go:80` — token in Authorization header only; no slog call in the codebase references `cfg.APIToken`; 04-REVIEW.md §"Items verified as correct" item 1 confirms this |

## Unregistered Flags

None. No `## Threat Flags` section present in 04-01-SUMMARY.md, 04-02-SUMMARY.md, or 04-03-SUMMARY.md.

## Accepted Risks Log

### AR-01: API token visible in step plugin config (T-04-07)

- **Risk:** The step plugin reads credentials directly from the Rollout spec `config` JSON field (no `secretKeyRef` support). Tokens appear in plaintext in the Rollout object, Argo Rollouts dashboard UI, and any kubectl output of the Rollout.
- **Justification:** The Argo Rollouts step plugin RPC interface passes `context.Config` as a raw JSON blob with no mechanism for secret reference resolution at the RPC layer. Implementing secretKeyRef would require in-cluster Kubernetes API access that the plugin binary does not have (D-07 constraint). This is a structural limitation of the plugin protocol, not an implementation gap.
- **Mitigations in place:** `examples/canary-full/rollout.yaml` lines 12-14 include a prominent NOTE warning users to store credentials in a Kubernetes Secret and use environment variable substitution or an init container to inject them.
- **Residual risk:** Low-medium. Requires cluster access to read Rollout objects. RBAC controls on Rollout resources limit exposure to cluster-authenticated users.
- **Owner:** User (documented as known limitation in MEMORY.md and in rollout.yaml example comment)
- **Review date:** 2026-04-14

### AR-02: K6_BASE_URL accepts arbitrary URLs without validation (T-04-04)

- **Risk:** The `K6_BASE_URL` environment variable in both plugin binaries is passed directly to the k6 OpenAPI client's `Servers` configuration (`cloud.go:54`) and to the v5 HTTP client (`metrics.go:61-64`) with no URL validation. A misconfigured or malicious value could redirect authenticated API calls to an unintended host.
- **Justification:** `K6_BASE_URL` is an operator-controlled environment variable set in the Argo Rollouts controller deployment. It is not user-sourced or controllable from AnalysisTemplate/Rollout config. Its sole purpose is to redirect API calls to a mock server during e2e testing. Production deployments do not set this variable.
- **Mitigations in place:** The variable is consumed only in `main.go` at startup and not exposed in any API surface. The controller's environment is controlled by cluster operators with elevated RBAC.
- **Residual risk:** Low. Requires ability to modify the controller Deployment environment, which implies cluster-admin access.
- **Owner:** Operator
- **Review date:** 2026-04-14

## Security Audit Trail

| Date | Auditor | Phase | Action |
|------|---------|-------|--------|
| 2026-04-14 | gsd-security-auditor (claude-sonnet-4-6) | 04-release-examples | Initial audit against derived threat register; all 8 threats verified |

## Sign-Off

- [x] All threats in register verified by disposition
- [x] All `mitigate` threats have code evidence (file:line)
- [x] All `accept` threats documented in Accepted Risks Log
- [x] No `transfer` threats in this phase
- [x] No unregistered threat flags to resolve
- [x] Implementation files not modified
- [x] ASVS Level 1 criteria met (no critical open threats)
