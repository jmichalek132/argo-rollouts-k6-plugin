# Pitfalls Research

**Domain:** Argo Rollouts plugin (metric + step) for Grafana Cloud k6
**Researched:** 2026-04-09
**Confidence:** HIGH (based on official Argo Rollouts docs, go-plugin internals, Grafana Cloud k6 API reference, and community issue trackers)

## Critical Pitfalls

### Pitfall 1: stdout Pollution Breaks go-plugin Handshake

**What goes wrong:**
The HashiCorp go-plugin library uses stdout for the initial handshake between the controller (host) and the plugin (child process). Any output to stdout before the plugin's `plugin.Serve()` call -- from imported packages, `init()` functions, or stray `fmt.Println` -- corrupts the handshake protocol. The controller sees unexpected data on stdout, fails to parse the connection info, and the plugin process is killed. The controller then refuses to start entirely because plugin download/init failed.

**Why it happens:**
Go libraries commonly write to stdout during initialization (logging defaults, version banners). Developers add debug prints during development and forget to remove them. Third-party dependencies may write to stdout in `init()` functions. The go-plugin handshake protocol is undocumented in most plugin guides -- it's an implicit requirement that stdout must be clean.

**How to avoid:**
- Never use `fmt.Print*` to stdout anywhere in plugin code. Use `os.Stderr` or a structured logger writing to stderr exclusively.
- In `main()`, call `plugin.Serve()` as early as possible -- before any initialization that might touch stdout.
- Set `hclog` (HashiCorp's logger) as the default logger pointing to stderr.
- Add a CI check that greps for `fmt.Print` / `os.Stdout` in the codebase and fails the build.
- Test the compiled binary manually: run it standalone and verify stdout contains only the go-plugin handshake line (`CORE_PROTOCOL_VERSION|APP_PROTOCOL_VERSION|NETWORK_TYPE|NETWORK_ADDR|PROTOCOL`).

**Warning signs:**
- Plugin works in unit tests but fails when loaded by the controller.
- Controller logs show "error starting plugin" or "failed to read plugin address" or "Unrecognized remote plugin message".
- Plugin binary, when run standalone, prints anything before the handshake line.

**Phase to address:**
Phase 1 (scaffold) -- establish logging conventions and a linter rule from day one.

---

### Pitfall 2: Controller Blocks Startup on Plugin Download Failure

**What goes wrong:**
The Argo Rollouts controller downloads plugin binaries at startup from the URL specified in `argo-rollouts-config` ConfigMap. If the hosting server (e.g., GitHub Releases) is unreachable, returns a non-200 status, or the SHA256 checksum doesn't match, the controller will not start. This blocks ALL rollouts across ALL namespaces, not just those using the plugin.

**Why it happens:**
Plugin download is a startup dependency, not a lazy-load. This is by design in Argo Rollouts -- plugins are loaded eagerly. GitHub Releases can have intermittent availability. Checksum mismatches happen when the release binary is rebuilt without updating the SHA256 in the ConfigMap, or when platform-specific binaries are mixed up (linux-amd64 vs linux-arm64).

**How to avoid:**
- Publish binaries to GitHub Releases with a deterministic build process (`goreleaser` with reproducible builds).
- Always generate and publish SHA256 checksums as part of the release CI pipeline -- never manually.
- Document the init-container installation method as a fallback (mount binary via volume instead of HTTP download).
- In the release CI, verify that the published binary's SHA256 matches the checksum file before finalizing the release.
- Build for all target architectures (linux/amd64, linux/arm64) and clearly name binaries with platform suffixes.

**Warning signs:**
- Controller pods in CrashLoopBackOff after a plugin release.
- Controller logs: "failed to download plugin", "sha256 mismatch".
- Users report ALL rollouts stalled (not just k6-related ones).

**Phase to address:**
Phase 1 (scaffold, build pipeline) -- set up goreleaser and checksum automation immediately.

---

### Pitfall 3: Protocol Version / Handshake Config Mismatch Between Plugin and Controller

**What goes wrong:**
go-plugin requires the plugin and host to agree on `ProtocolVersion`, `MagicCookieKey`, and `MagicCookieValue` in the `plugin.HandshakeConfig`. If the plugin uses a different protocol version than what the Argo Rollouts controller expects, the plugin silently fails to load. The controller treats this as a fatal error.

**Why it happens:**
Argo Rollouts pins specific `HandshakeConfig` values in its codebase. Plugin authors must import the correct version of `github.com/argoproj/argo-rollouts` in their `go.mod` to get the matching handshake constants. If the plugin is built against a different version of the argo-rollouts module than the controller running in the cluster, the handshake values may diverge. This is especially treacherous during Argo Rollouts version upgrades.

**How to avoid:**
- Pin the `github.com/argoproj/argo-rollouts` dependency in `go.mod` to the same minor version as the target controller.
- Document a compatibility matrix: plugin version X works with controller versions Y-Z.
- Use the handshake constants directly from the imported argo-rollouts package (e.g., `rolloutsPlugin.HandshakeConfig`) rather than hardcoding them.
- Add an integration test that loads the plugin binary with the target controller version.

**Warning signs:**
- Plugin binary executes fine standalone but controller logs show "incompatible plugin version" or "magic cookie mismatch".
- After controller upgrade, previously working plugin stops loading.

**Phase to address:**
Phase 1 (scaffold) -- import the correct argo-rollouts module version. Phase for integration tests -- verify compatibility.

---

### Pitfall 4: Metric Plugin Run/Resume Misunderstanding Causes Stale or Duplicate Measurements

**What goes wrong:**
The `RpcMetricProvider` interface has a two-phase measurement pattern: `Run` starts an external call (must be idempotent), and `Resume` checks if the call is finished. If `Run` is not truly idempotent -- e.g., it triggers a new k6 test run on every invocation -- you get duplicate k6 runs. If `Resume` doesn't properly track in-flight measurements, it returns stale data from a previous run.

**Why it happens:**
The two-phase pattern is non-obvious. The controller calls `Run` first, and if the measurement is not immediately complete (the `Measurement.Phase` is `Running`), it later calls `Resume` to poll. But if the controller reconciles again, it may call `Run` again. If `Run` doesn't check whether a measurement is already in-flight, it fires another k6 API call. For k6 specifically, this means triggering duplicate test runs against Grafana Cloud.

**How to avoid:**
- In `Run`: check if a measurement is already in progress (e.g., by checking the returned `Measurement.StartedAt` or storing the k6 run ID in `Measurement.Metadata`). If a run is already active, return the existing measurement without triggering a new k6 run.
- In `Resume`: use the k6 run ID stored in `Measurement.Metadata` to poll the specific run's status and metrics. Never query "latest run" -- always query by the specific run ID.
- Store the k6 test run ID in `Measurement.Metadata` map so it persists across Run/Resume cycles.
- Write unit tests that call `Run` twice and verify only one k6 API call was made.

**Warning signs:**
- Multiple k6 test runs appear in Grafana Cloud for a single AnalysisRun.
- Metrics returned by the plugin don't correspond to the current deployment's test run.
- Race conditions in concurrent rollouts where measurements cross-contaminate.

**Phase to address:**
Phase 2 (metric plugin implementation) -- core correctness requirement.

---

### Pitfall 5: Step Plugin Run Not Idempotent -- Duplicate k6 Test Runs on Requeue

**What goes wrong:**
The step plugin's `Run` method is called multiple times by the controller. When the plugin returns `PhaseRunning` with a `RequeueAfter`, the controller calls `Run` again after that duration. If `Run` doesn't check whether it already triggered a k6 test run (by inspecting `RpcStepContext.Status`), it triggers a new k6 run on every requeue cycle. This wastes Grafana Cloud quota and produces confusing results.

**Why it happens:**
The step plugin API explicitly documents that "the operation can be called multiple times" and "it is the responsibility of the plugin's implementation to validate if the desired plugin actions were already taken." But developers often implement `Run` as a simple "trigger and poll" without persisting state. The `RpcStepContext.Status` field (a `json.RawMessage`) exists specifically for persisting state between calls, but it's easy to overlook.

**How to avoid:**
- On first `Run` call (when `RpcStepContext.Status` is nil or empty): trigger the k6 test run, store the run ID in the `Status` field of `RpcStepResult`, return `PhaseRunning` with appropriate `RequeueAfter`.
- On subsequent `Run` calls (when `Status` contains a run ID): unmarshal the status, poll the k6 run by its ID, return `PhaseRunning`/`PhaseSuccessful`/`PhaseFailed` based on the k6 run state.
- Never trigger a new k6 run if `Status` already contains a run ID that is not in a terminal state.

**Warning signs:**
- Each requeue cycle creates a new k6 test run in Grafana Cloud.
- Plugin logs show "starting test run" on every poll cycle.
- k6 Cloud quota is consumed much faster than expected.

**Phase to address:**
Phase 3 (step plugin implementation) -- core correctness requirement.

---

### Pitfall 6: k6 Test Run Finishes Between Polls -- Missing Terminal State

**What goes wrong:**
The step plugin polls the k6 API at a fixed `RequeueAfter` interval. If the k6 test run finishes (or errors) and the plugin only checks for `status == "running"` to decide whether to keep polling, it may miss the terminal state details. Worse, if the k6 API returns intermediate states during data aggregation (test finished but metrics not yet available), the plugin may read incomplete metrics and make a wrong pass/fail decision.

**Why it happens:**
The Grafana Cloud k6 API has multiple terminal states: finished, timed_out, aborted_by_user, aborted_by_system, aborted_by_script_error, aborted_by_threshold. Developers often only check for "finished" and treat everything else as "still running," causing the plugin to poll indefinitely for aborted or timed-out runs. Additionally, there's a brief window after a test finishes where metrics may not be fully aggregated.

**How to avoid:**
- Map ALL k6 test run status codes to plugin outcomes. Terminal statuses: finished (check thresholds), timed_out (fail), all aborted variants (fail with descriptive message).
- After detecting a terminal state, add a brief delay or retry before reading final metrics to allow Grafana Cloud to finish metric aggregation.
- Set a hard timeout in the step plugin that exceeds the expected k6 test duration by a margin (e.g., k6 test duration + 5 minutes). If exceeded, return `PhaseFailed` with a timeout message rather than polling forever.
- Handle the k6 API returning 404 or unexpected states gracefully (the test may have been deleted or the API may be temporarily unavailable).

**Warning signs:**
- Step plugin hangs in `Running` phase indefinitely for aborted k6 tests.
- Plugin reports success for tests that actually timed out.
- Metrics show 0 or NaN values because they were queried before aggregation completed.

**Phase to address:**
Phase 3 (step plugin) -- must handle all terminal states.

---

### Pitfall 7: Concurrent AnalysisRuns Share Plugin Process State

**What goes wrong:**
The plugin runs as a single long-lived process for the entire Argo Rollouts controller. Multiple AnalysisRuns (from concurrent rollouts) share the same plugin process. If the plugin stores state in global variables or package-level maps without proper synchronization, concurrent AnalysisRuns can read/write each other's state. This causes measurement cross-contamination, data races, and incorrect pass/fail decisions.

**Why it happens:**
The go-plugin architecture starts one plugin process that serves all requests. Each RPC call from the controller is a separate goroutine in the plugin process. Developers coming from per-request architectures (HTTP handlers) may not realize that all AnalysisRuns hit the same process. Using a package-level `map[string]RunStatus` without a mutex is a classic Go data race.

**How to avoid:**
- Never use global mutable state. If state is needed (e.g., tracking in-flight k6 runs), use a `sync.Map` or a mutex-protected map keyed by AnalysisRun name/UID.
- For the metric plugin, use `Measurement.Metadata` to pass state between `Run` and `Resume` -- this is per-measurement state managed by the controller.
- For the step plugin, use `RpcStepContext.Status` (json.RawMessage) to persist state -- this is per-step state managed by the controller.
- Run tests with `-race` flag: `go test -race ./...`
- Write concurrent integration tests that simulate multiple AnalysisRuns hitting the plugin simultaneously.

**Warning signs:**
- Flaky test results that only appear under concurrent rollouts.
- Data race warnings from `go test -race`.
- AnalysisRun A reports metrics from AnalysisRun B's k6 test.

**Phase to address:**
Phase 2/3 (plugin implementation) -- design for concurrency from the start. Phase for integration tests -- concurrent rollout test scenario.

---

### Pitfall 8: Grafana Cloud k6 API Authentication Fails Silently

**What goes wrong:**
The k6 Cloud REST API requires two headers: `Authorization: Token <api-token>` and `X-Stack-Id: <stack-id>`. If either is wrong or missing, the API may return a generic 401/403 without a descriptive error. The plugin surfaces this as a vague "measurement failed" to the AnalysisRun, leaving users with no idea what went wrong. Worse, if the token format is wrong (e.g., using `Bearer` instead of `Token`), some endpoints may return 200 with empty data instead of an auth error.

**Why it happens:**
The k6 API uses `Token` prefix (not `Bearer`), which is unusual. The `X-Stack-Id` header is specific to Grafana Cloud and not standard REST convention. Users may configure a Grafana API token instead of a k6 API token. The deprecated API endpoints at `api.k6.io` use different auth than the current v5 endpoints.

**How to avoid:**
- Validate auth credentials in `InitPlugin`: make a lightweight API call (e.g., list projects) and fail fast with a descriptive error if auth fails.
- Document the exact auth header format: `Authorization: Token <token>` (not `Bearer`).
- Document which token type is needed: Grafana Cloud Stack API token, not a Grafana instance API key.
- Surface the exact HTTP status code and response body in plugin error messages.
- Distinguish between "auth failed" (401/403) and "test not found" (404) in error handling.

**Warning signs:**
- Plugin returns `Error` phase with generic message.
- k6 API returns empty results (no metrics) but no HTTP error.
- Users report "works in curl but not in the plugin" -- likely a token format issue.

**Phase to address:**
Phase 2 (metric plugin, first API integration) -- validate auth on InitPlugin.

---

### Pitfall 9: AnalysisTemplate Secret References Silently Fail

**What goes wrong:**
AnalysisTemplates reference secrets via `valueFrom.secretKeyRef` for API tokens. But the secret resolution only happens when the AnalysisRun is created, not when the AnalysisTemplate is applied. If the secret doesn't exist, the AnalysisRun fails at runtime. Additionally, the AnalysisRun can only reference secrets in its own namespace, and the Argo Rollouts controller needs cluster-scoped secret read permissions.

**Why it happens:**
AnalysisTemplates are namespace-scoped or cluster-scoped templates. The secret resolution is deferred to AnalysisRun creation time. If a ClusterAnalysisTemplate references a secret, that secret must exist in the namespace where the Rollout (and thus the AnalysisRun) lives. This cross-namespace requirement is non-obvious. The controller service account may not have permissions to read secrets in all namespaces.

**How to avoid:**
- Document the exact RBAC requirements: the argo-rollouts service account needs `get` and `list` on secrets in namespaces where rollouts will run.
- Provide example AnalysisTemplate YAML with correct `valueFrom.secretKeyRef` syntax and a companion Secret manifest.
- Validate in the plugin (in `Run` or `InitPlugin`) that required config fields are non-empty. If an arg was supposed to come from a secret but resolved to empty string, surface a clear error: "API token is empty -- check that the secret exists in the AnalysisRun namespace."
- Test with both namespace-scoped AnalysisTemplate and ClusterAnalysisTemplate to verify secret resolution works in both cases.

**Warning signs:**
- AnalysisRun fails with "secret not found" only when running in a different namespace than expected.
- Plugin receives empty string for API token.
- Works in the `argo-rollouts` namespace but fails in application namespaces.

**Phase to address:**
Phase 2 (metric plugin) -- include example manifests and validation. Phase for integration tests -- test cross-namespace secret resolution.

---

### Pitfall 10: Static Binary with CGO_ENABLED=0 Breaks DNS in Some Environments

**What goes wrong:**
Building with `CGO_ENABLED=0` produces a fully static binary (no libc dependency), which is ideal for minimal containers. But Go's pure-Go DNS resolver (used when CGO is disabled) doesn't respect all `/etc/nsswitch.conf` directives, including the `myhostname` NSS plugin. In some Kubernetes environments, DNS resolution for the Grafana Cloud API may fail or behave differently than expected, particularly in clusters with custom DNS configurations.

**Why it happens:**
`CGO_ENABLED=0` forces Go's built-in DNS resolver instead of the system's libc resolver. The Go resolver reads `/etc/resolv.conf` directly but doesn't support NSS modules. In most Kubernetes clusters this works fine because CoreDNS handles resolution. But in environments with split-DNS, custom search domains, or VPN-based DNS (common in enterprise), the pure-Go resolver may fail to resolve `api.k6.io`.

**How to avoid:**
- Build with `CGO_ENABLED=0` (the standard for static Go binaries in Kubernetes) but document that DNS resolution uses Go's built-in resolver.
- Test the binary in the target cluster environment, not just locally.
- If DNS issues arise, document the `GODEBUG=netdns=cgo` workaround (requires a non-scratch base image with libc).
- Consider using the IP address as a fallback configuration option (not recommended for production, but useful for debugging).

**Warning signs:**
- Plugin works locally or in standard kind/minikube but fails in enterprise clusters.
- Error logs show "dial tcp: lookup api.k6.io: no such host" despite DNS working for other pods.
- Works with `CGO_ENABLED=1` but not with `CGO_ENABLED=0`.

**Phase to address:**
Phase 1 (scaffold, build pipeline) -- set up build flags. Phase for integration tests -- test in kind cluster.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Hardcode k6 API base URL | Faster initial development | Cannot switch to self-hosted k6 or different regions | Never -- use a configurable base URL from day one |
| Skip `Resume` implementation (do everything in `Run`) | Simpler metric plugin | Blocks the controller's reconciliation loop during long-running k6 queries; timeouts under load | Never for metric plugin -- the Run/Resume pattern exists to avoid blocking |
| Global `http.Client` without timeout | Works in happy path | Leaked connections, hung goroutines if k6 API is slow | Never -- always set `Timeout`, `IdleConnTimeout`, and use context cancellation |
| Test only with Grafana Cloud (no mock) | Real API validation | CI depends on external service; flaky tests; burns API quota | Mock for unit/CI, real API for manual integration testing only |
| Single binary for both metric + step plugins | Simpler release | Users who only need one plugin still download both; larger binary | Acceptable -- both plugins are lightweight and share the k6 client |
| Skip provider abstraction initially | Ship metric plugin faster | Coupling k6 Cloud API calls into plugin interface code; harder to add in-cluster k6 later | Never -- the interface is cheap to add upfront per PROJECT.md requirements |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Grafana Cloud k6 API | Using `Bearer` token format instead of `Token` prefix in Authorization header | Use `Authorization: Token <api-token>` with `X-Stack-Id` header |
| Grafana Cloud k6 API | Polling the deprecated `api.k6.io/v1` endpoints | Use the current v5 API: `api.k6.io/cloud/v5` endpoints |
| Grafana Cloud k6 API | Querying "latest test run" instead of a specific run ID | Always store and query by the specific `test_run_id` returned when starting a run |
| Grafana Cloud k6 API | Not handling pagination for metric results | Use `pageSize` and `pageCursor` parameters; check `metadata.pagination.nextPage` for more pages |
| Argo Rollouts controller | Assuming plugin is lazy-loaded | Plugin binary is downloaded and started at controller startup; unavailability blocks ALL rollouts |
| Argo Rollouts AnalysisRun | Assuming `successCondition`/`failureCondition` are optional for metric plugins | If your metric returns a numeric value, the AnalysisTemplate MUST define success/failure conditions or ALL measurements are Inconclusive |
| Argo Rollouts secrets | Expecting secret resolution in AnalysisTemplate | Secrets are only resolved at AnalysisRun creation time; AnalysisRun must be in the same namespace as the secret |
| HashiCorp go-plugin | Logging to stdout in plugin code | All logging must go to stderr; stdout is reserved for the go-plugin handshake protocol |
| Kubernetes RBAC | Assuming plugin has its own RBAC | Plugin inherits the controller's service account; if the plugin needs secret access, the controller SA needs secret read permissions |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Synchronous k6 API calls in metric `Run` | Controller reconciliation loop blocked; all rollouts slow down | Use the async Run/Resume pattern: `Run` triggers the API call and returns immediately with `Phase: Running`; `Resume` polls | With >3 concurrent AnalysisRuns polling k6 API simultaneously |
| No HTTP client connection pooling | New TCP + TLS handshake per API call; increased latency; possible connection exhaustion | Reuse a single `http.Client` with connection pooling (Go's default) across the plugin lifetime; set in `InitPlugin` | With frequent polling intervals (<30s) across many concurrent runs |
| Polling k6 API too frequently | Hit rate limits (600/hour for some endpoints); 429 responses cause measurement failures | Use reasonable poll intervals (30s-60s for metric plugin; 15-30s for step plugin); implement exponential backoff on 429 | At >10 concurrent AnalysisRuns with 10s poll intervals |
| Fetching all metrics when only a few are needed | Slow API responses; wasted bandwidth; higher latency per measurement | Use the specific metric query endpoints with filters for only the metrics defined in the AnalysisTemplate | With k6 tests that produce hundreds of custom metrics |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Logging the k6 API token in plugin output | Token leaked to controller logs, potentially visible to any user with log access | Never log auth headers or token values; log only that auth was attempted and the result (success/fail) |
| Storing API token directly in AnalysisTemplate | Token visible in plaintext in the Rollout/AnalysisTemplate spec | Always use `valueFrom.secretKeyRef` to reference a Kubernetes Secret; document this as the only supported method |
| Over-scoped controller RBAC for secrets | Controller can read ALL secrets in ALL namespaces, including unrelated sensitive data | Request minimum RBAC: `get` on secrets only in namespaces where rollouts run; consider namespace-scoped roles |
| Plugin binary distributed over HTTP (not HTTPS) | Binary tampered in transit; supply chain attack | Always distribute plugin binary over HTTPS; always configure SHA256 checksum in `argo-rollouts-config` |
| Not verifying k6 API TLS certificates | Man-in-the-middle attack on API communication | Use Go's default TLS verification (do NOT set `InsecureSkipVerify`); if custom CA is needed, load it explicitly |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Vague error messages from the plugin ("measurement failed") | User has no idea if the problem is auth, network, wrong test ID, or a bug | Surface the HTTP status code, k6 API error message, and the failing endpoint in the error message |
| No validation of AnalysisTemplate config fields | User applies a template with a typo in the test ID field; only fails at AnalysisRun time | Validate all config fields in `Run`/`InitPlugin`; return clear error: "testId is required but was empty" |
| Requiring users to know k6 API metric names | Users need to read k6 API docs to know metric names like `http_req_duration` | Provide a set of well-known metric presets (error_rate, p95_latency, p99_latency, threshold_pass) with sensible defaults |
| No example AnalysisTemplates | Users start from scratch, make config mistakes | Ship example templates for common use cases: basic pass/fail, latency threshold, custom metric |
| Silent success when k6 test has no data | User gets a "Successful" AnalysisRun but the k6 test had 0 requests | Detect and warn when k6 metrics indicate 0 requests or 0 VUs; optionally treat this as a failure |

## "Looks Done But Isn't" Checklist

- [ ] **Metric plugin Run:** Does it actually check if a measurement is already in-flight before starting a new one? (Idempotency)
- [ ] **Step plugin Run:** Does it persist and check `RpcStepContext.Status` to avoid duplicate k6 triggers on requeue?
- [ ] **Error handling:** Does the plugin distinguish between k6 API errors (retry-able) and config errors (permanent failure)? Or does it treat all errors the same?
- [ ] **All k6 terminal states:** Does the step plugin handle `timed_out`, `aborted_by_user`, `aborted_by_system`, `aborted_by_script_error`, `aborted_by_threshold` -- not just `finished`?
- [ ] **Concurrent safety:** Does the plugin use `-race` in CI? Can two AnalysisRuns run simultaneously without state contamination?
- [ ] **Terminate/Abort:** Does the step plugin actually cancel in-flight k6 runs on Terminate, or does it just return without cleanup?
- [ ] **GarbageCollect:** Does the metric plugin implement `GarbageCollect` properly, or is it a no-op that leaks state?
- [ ] **Checksum published:** Does the release CI publish SHA256 checksums alongside binaries? Is the checksum referenced in example ConfigMap YAMLs?
- [ ] **Multi-arch binaries:** Are linux/amd64 AND linux/arm64 binaries published? Many clusters run on ARM now.
- [ ] **Empty metric response:** What happens when the k6 API returns no metrics (e.g., test run had 0 requests)? Is it treated as success, failure, or inconclusive?

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| stdout pollution breaks handshake | LOW | Fix the logging, rebuild, redeploy. No data loss. |
| Controller blocked by plugin download failure | MEDIUM | Apply ConfigMap with `file://` path or init-container mount. Restart controller. During downtime, all rollouts are paused. |
| Duplicate k6 runs from non-idempotent Run | LOW | Stop the extra runs in Grafana Cloud console. Fix the idempotency bug. No rollout data is lost since AnalysisRun can be restarted. |
| Stale measurements from wrong run ID | HIGH | Affected rollouts may have been promoted/rolled-back based on wrong data. Must manually verify the deployed version. Fix the measurement tracking. |
| Protocol version mismatch after controller upgrade | MEDIUM | Rebuild plugin against new argo-rollouts module version. Publish new binary. Update ConfigMap. Controller restart required. |
| API token leaked in logs | HIGH | Rotate the Grafana Cloud API token immediately. Audit log access. Fix the logging code. Redeploy. |
| Secret reference fails in different namespace | LOW | Create the secret in the correct namespace or fix RBAC. AnalysisRun can be retried. |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| stdout pollution | Phase 1: Scaffold | CI lint rule for `fmt.Print`/`os.Stdout`; manual binary handshake test |
| Controller startup block | Phase 1: Build pipeline | goreleaser config with SHA256; release smoke test |
| Protocol version mismatch | Phase 1: Scaffold | go.mod pins argo-rollouts to target version; integration test loads plugin |
| Non-idempotent metric Run | Phase 2: Metric plugin | Unit test calling Run twice verifies single API call |
| Non-idempotent step Run | Phase 3: Step plugin | Unit test calling Run with non-empty Status verifies no new k6 trigger |
| Missing k6 terminal states | Phase 3: Step plugin | Test coverage for all k6 status values including aborted variants |
| Concurrent AnalysisRun safety | Phase 2/3: Plugin implementation | `go test -race`; concurrent integration test |
| Auth header format | Phase 2: Metric plugin (first API call) | InitPlugin validation; unit test with mock server |
| Secret resolution failure | Phase 2: Metric plugin | Example manifests tested in integration; clear error messages |
| CGO/DNS issues | Phase 1: Build pipeline; Integration tests | kind cluster test with binary built via `CGO_ENABLED=0` |
| API rate limits | Phase 2: Metric plugin | Configurable poll interval; backoff on 429; documented defaults |
| Vague error messages | Phase 2/3: All plugin methods | Error message review; user testing |

## Sources

- [Argo Rollouts Plugin Documentation](https://argo-rollouts.readthedocs.io/en/stable/plugins/) -- plugin architecture, loading, lifecycle
- [Argo Rollouts Step Plugin Documentation](https://argoproj.github.io/argo-rollouts/features/canary/plugins/) -- Run/Terminate/Abort lifecycle
- [Argo Rollouts Analysis Overview](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/) -- AnalysisRun, successCondition, failureCondition
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin) -- handshake protocol, stdout/stderr, binary startup
- [go-plugin stdout pollution issue](https://github.com/hashicorp/go-plugin/issues/164) -- stdout handshake corruption
- [go-plugin broken state issue](https://github.com/hashicorp/go-plugin/issues/306) -- plugin stuck waiting for RPC address
- [Argo Rollouts Plugin Types (pkg.go.dev)](https://pkg.go.dev/github.com/argoproj/argo-rollouts/utils/plugin/types) -- RpcMetricProvider, RpcStep interfaces
- [Grafana Cloud k6 REST API](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/) -- authentication, endpoints, pagination
- [Grafana Cloud k6 Test Status Codes](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-test-status-codes/) -- terminal states
- [k6 Cloud Authorization](https://grafana.com/docs/grafana-cloud/testing/k6/reference/cloud-rest-api/authorization/) -- Token format, X-Stack-Id
- [Argo Rollouts Secret Discussion #863](https://github.com/argoproj/argo-rollouts/discussions/863) -- RBAC for secret access
- [Argo Rollouts Step Plugins Issue #2685](https://github.com/argoproj/argo-rollouts/issues/2685) -- design decisions, alpha status
- [rollouts-plugin-metric-sample-prometheus](https://github.com/argoproj-labs/rollouts-plugin-metric-sample-prometheus) -- reference implementation
- [Go DNS resolution with CGO_ENABLED=0](https://medium.com/box-tech-blog/a-trip-down-the-dns-rabbit-hole-understanding-the-role-of-kubernetes-golang-libc-systemd-41fd80ffd679) -- DNS pitfalls in static Go binaries
- [AnalysisRun Inconclusive Issue #3015](https://github.com/argoproj/argo-rollouts/issues/3015) -- stuck analysis after inconclusive result
- [Red Hat OpenShift Argo Rollouts Plugin Guide](https://docs.redhat.com/en/documentation/red_hat_openshift_gitops/1.15/html/argo_rollouts/configuring_traffic_management_and_metric_plugins_in_argo_rollouts) -- plugin configuration details

---
*Pitfalls research for: Argo Rollouts k6 Plugin (metric + step)*
*Researched: 2026-04-09*
