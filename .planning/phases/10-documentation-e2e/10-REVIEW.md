---
phase: 10-documentation-e2e
reviewed: 2026-04-16T12:00:00Z
depth: standard
files_reviewed: 14
files_reviewed_list:
  - README.md
  - e2e/k6_operator_test.go
  - e2e/main_test.go
  - e2e/mock/main.go
  - e2e/testdata/analysistemplate-k6op.yaml
  - e2e/testdata/k6-script-configmap.yaml
  - e2e/testdata/rollout-step-k6op.yaml
  - examples/k6-operator/README.md
  - examples/k6-operator/analysistemplate.yaml
  - examples/k6-operator/clusterrole.yaml
  - examples/k6-operator/clusterrolebinding.yaml
  - examples/k6-operator/configmap-script.yaml
  - examples/k6-operator/rollout-metric.yaml
  - examples/k6-operator/rollout-step.yaml
findings:
  critical: 0
  warning: 4
  info: 6
  total: 10
status: issues_found
---

# Phase 10: Code Review Report

**Reviewed:** 2026-04-16T12:00:00Z
**Depth:** standard
**Files Reviewed:** 14
**Status:** issues_found

## Summary

The phase delivers documentation (top-level README, examples README) and a real
k6-operator e2e test suite (`e2e/k6_operator_test.go`) plus supporting mock
HTTP server changes (`/health` endpoint) and RBAC manifests. The work is
generally sound — RBAC follows least-privilege and is well-commented, the mock
server correctly handles shared atomic counters, and e2e tests use sensible
timeouts with diagnostic dumps on failure.

No Critical issues found. Four Warning-level issues cover documentation
inaccuracies that users will hit on their first run, a doc claim that
contradicts the actual example's recommendation (pinning runner image), and
two e2e resilience gaps (TestRun count race, initial-rollout trigger is a
no-op on rollout creation rather than forcing a template change). Info items
cover style/consistency.

## Warnings

### WR-01: Top-level README misrepresents k6-operator example contents

**File:** `README.md:148-151`
**Issue:** The README states under "Examples":
> Each example directory contains:
> - `analysistemplate.yaml` (or `rollout.yaml`) -- the main resource
> - `secret.yaml` -- credential Secret with placeholder values
> - `configmap-snippet.yaml` -- ConfigMap snippet to register the plugin(s)

The `examples/k6-operator/` directory intentionally has no `secret.yaml`
(in-cluster execution needs no Grafana Cloud credentials) and no
`configmap-snippet.yaml`. It has `clusterrole.yaml`, `clusterrolebinding.yaml`,
`configmap-script.yaml`, `rollout-step.yaml`, `rollout-metric.yaml`, and
`analysistemplate.yaml`. Users following the README will look for files that
don't exist.

**Fix:** Reword the "Each example directory contains" block to say it
describes only the cloud-mode examples, or add a per-example bullet list.
E.g.:
```markdown
Cloud-mode examples (`threshold-gate`, `error-rate-latency`, `canary-full`) contain:
- `analysistemplate.yaml` (or `rollout.yaml`) -- the main resource
- `secret.yaml` -- credential Secret with placeholder values
- `configmap-snippet.yaml` -- ConfigMap snippet to register the plugin(s)

The `k6-operator` example runs in-cluster and ships with a ClusterRole,
ClusterRoleBinding, and script ConfigMap instead of a credential Secret.
See `examples/k6-operator/README.md`.
```

### WR-02: examples/k6-operator/README.md contradicts pinned-version recommendation

**File:** `examples/k6-operator/README.md:127-129`
**Issue:** The "Notes" section says:
> The k6 runner image defaults to `grafana/k6:latest`. For production, pin a
> specific version by setting the `runnerImage` config field in your plugin
> config (for example `runnerImage: grafana/k6:1.0.0`).

This contradicts the actual recommendation from the e2e tests, which pin
`grafana/k6:0.56.0` (see `e2e/main_test.go:107` — `k6RunnerImage = "grafana/k6:0.56.0"`
— with a comment stating "grafana/k6:latest is non-deterministic"). More
importantly, neither `rollout-step.yaml` nor `rollout-metric.yaml` in the
examples directory sets `runnerImage`, so users copy-pasting the examples
will silently use `:latest`, reproducing the exact non-determinism the e2e
test suite went out of its way to avoid. The `analysistemplate.yaml` example
also omits `runnerImage`, while the e2e equivalent (`testdata/analysistemplate-k6op.yaml:19`)
pins `grafana/k6:0.56.0`.

**Fix:** Add `runnerImage: grafana/k6:0.56.0` (or a current stable tag) to
all three example manifests:
- `examples/k6-operator/rollout-step.yaml` (inside `config:`)
- `examples/k6-operator/rollout-metric.yaml` — does not set it directly, but
  `analysistemplate.yaml` should
- `examples/k6-operator/analysistemplate.yaml` (inside `jmichalek132/k6:`)

Also strengthen the README note from "For production, pin..." to "These
examples pin `grafana/k6:0.56.0`; update to a newer tag as needed. Avoid
`:latest` — non-deterministic across runs."

### WR-03: countTestRuns race — TestRun may be garbage-collected before the assertion reads it

**File:** `e2e/k6_operator_test.go:82-91`
**Issue:** `TestK6OperatorStepPass` waits for `phase == "Healthy"` and then
asserts `testRunCount >= 1`. But once the step plugin's work is done, nothing
in the assertion prevents a `Terminate()` call (or a future cleanup hook)
from deleting the TestRun CR. If Rollout progression reaches Healthy on the
very last poll iteration and TestRun deletion races against
`countTestRuns`, this assertion can false-fail intermittently. The current
implementation probably works today because the step plugin does not
garbage-collect on success, but relying on that is fragile.

**Fix:** Make the ordering explicit. Option A — list TestRuns *before* waiting
for Healthy:
```go
// Wait until at least one TestRun has been observed, then let the Rollout finish.
if err := waitUntil(5*time.Minute, func() (bool, error) {
    n, err := countTestRuns(cfg, cfg.Namespace())
    return n >= 1, err
}); err != nil {
    dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
    t.Fatalf("wait for TestRun creation: %v", err)
}
phase, err := waitForRolloutPhase(cfg, "k6-step-k6op-e2e", cfg.Namespace(), "Healthy", 5*time.Minute)
```
Option B — add `Rollout.status.currentStepIndex` inspection so the test waits
for "canary step advanced past k6 step" rather than "Healthy". This is more
precise about what the step plugin actually did.

### WR-04: Step plugin rollout trigger uses pod-template annotation; no guarantee k6 step ever executes

**File:** `e2e/k6_operator_test.go:60-65`
**Issue:** The trigger patch sets
`spec.template.metadata.annotations["test/run"] = "2"`. This forces a new
ReplicaSet and therefore a canary progression — good. But the test never
verifies that the canary actually *entered* the `jmichalek132/k6-step` step.
It only verifies the rollout reached Healthy and that *at least one* TestRun
exists. If the canary step was skipped (e.g., because of a future
`DisableCanarySteps` annotation, or because `spec.strategy.canary.steps`
was misread), the test would pass even though the step plugin was never
invoked — as long as any TestRun was created earlier in the test lifecycle.

**Fix:** Before the Healthy assertion, poll for a condition that proves step
execution occurred. Two options, either alone is sufficient:
```go
// Option A: verify Rollout.status.currentStepIndex advanced past 1 (the k6 step)
// Option B: verify at least one TestRun owner-ref matches the canary ReplicaSet
```
Minimum viable: assert TestRun creation *after* the patch by clearing state
in Setup:
```go
// In Setup, before the patch:
_ = runKubectl(cfg, "delete", "testruns", "--all", "-n", cfg.Namespace(), "--ignore-not-found")
```
(currently Teardown deletes TestRuns, but within a single Feature run the
counter is cumulative and a TestRun created during a transient reconcile
could be counted even if the canary step later misfires.)

## Info

### IN-01: mock server swallows handler write errors silently

**File:** `e2e/mock/main.go:127, 143, 163, 221, 239`
**Issue:** All `json.NewEncoder(w).Encode(...)` and `w.Write([]byte(...))`
calls use `_ = ...` to discard errors. For a test mock this is generally
fine, but if the kind network gets flaky, a silent failure here will
cause downstream plugin errors that are hard to trace back to the mock.

**Fix:** At minimum log the error so it shows up in test output:
```go
if err := json.NewEncoder(w).Encode(body); err != nil {
    log.Printf("encode response: %v", err)
}
```

### IN-02: handleGetTestRun ignores Sscanf error

**File:** `e2e/mock/main.go:147-148`
**Issue:** `_, _ = fmt.Sscanf(segmentAfter(path, "test_runs"), "%d", &runID)`
silently defaults `runID` to 0 on any parse error. `runConfigs` has no key
`0`, so the 404 branch catches it — but the 404 message will say "run 0 not
configured" rather than reporting the unparseable segment, making debugging
harder. Same pattern at line 201-202 in `handleAggregateQuery`.

**Fix:**
```go
seg := segmentAfter(path, "test_runs")
runID, err := strconv.Atoi(seg)
if err != nil {
    http.Error(w, fmt.Sprintf("invalid run id %q: %v", seg, err), http.StatusBadRequest)
    return
}
```

### IN-03: testRunBody uses a past fixed date that will look stale in logs

**File:** `e2e/mock/main.go:179, 182, 186`
**Issue:** Response bodies hardcode `"2026-01-01T00:00:00Z"` as the entered/
created timestamp. This isn't wrong — it's a mock — but logs during
debugging show "created 3+ months ago" when the mock server actually started
seconds ago. Minor cognitive overhead during triage.

**Fix:** Either use `time.Now().UTC().Format(time.RFC3339)` at handler time
(simple) or keep the constant but add a comment: `// fixed in the past so
bodies are deterministic across test runs`. Either is acceptable.

### IN-04: AnalysisTemplate in examples/k6-operator/ uses `namespace` arg but the in-cluster plugin ignores non-same-namespace targets for ConfigMaps

**File:** `examples/k6-operator/analysistemplate.yaml:43`
**Issue:** The template takes a `namespace` arg and plumbs it to
`jmichalek132/k6.namespace`. It's unclear from the README whether this is
for the k6-operator TestRun namespace, the ConfigMap-lookup namespace, or
both. Users copy-pasting this will likely assume "my Rollout's namespace";
in practice the ConfigMap must live where k6-operator can `Get` it, which
is typically the Rollout namespace. The README section "Setup" (line
33-39) tells users to apply the ConfigMap `-n <your-namespace>` but the
example rollout-metric.yaml uses `namespace: default` — users who followed
step 2 with a non-default namespace and then applied `rollout-metric.yaml`
verbatim will hit a "ConfigMap not found" error.

**Fix:** Either (a) document exactly what `namespace:` means on the plugin
config and pin it to `default` in the template with a comment pointing to
the ConfigMap install namespace; or (b) remove the `namespace` plumbing
from `analysistemplate.yaml` and let the plugin default to the AnalysisRun's
own namespace (simpler, and matches the e2e test's implicit behavior).

### IN-05: e2e mock AnalysisTemplate name differs from examples (only surfaces as cleanup pattern)

**File:** `e2e/testdata/analysistemplate-k6op.yaml:4` vs
`examples/k6-operator/analysistemplate.yaml:24`
**Issue:** The e2e AnalysisTemplate is named
`k6-operator-threshold-e2e`, while the example is named
`k6-operator-threshold-check`. The suffix convention ("-e2e" vs "-check")
is fine, but note that `e2e/k6_operator_test.go:186` deletes
`k6-operator-threshold-e2e` explicitly — if someone later renames the
template file but forgets to update the test teardown, cleanup will silently
leak. Not an issue today.

**Fix:** Consider defining the template name as a test-file const:
```go
const k6OperatorTemplateName = "k6-operator-threshold-e2e"
```
and using it both in the YAML generation (if inlined) and in the teardown
delete call. Low priority.

### IN-06: runKubectl hides stdout when stderr reports failure

**File:** `e2e/main_test.go:443-454`
**Issue:** `runKubectl` sets `cmd.Stderr = &stderr` and `cmd.Stdout = os.Stderr`.
On kubectl failure, the returned error string is only the captured stderr:
`fmt.Errorf("%s: %w", stderr.String(), err)`. If kubectl writes useful
context to stdout (e.g., `kubectl apply` sometimes does for diff-style
output), that context is routed to the test stderr stream but is not
included in the error chain, which complicates grepping CI logs for a
specific failed command.

**Fix:** Either capture both and include in the error, or keep as-is and
document the behavior. Minor:
```go
var stdout, stderr bytes.Buffer
cmd.Stderr = &stderr
cmd.Stdout = io.MultiWriter(os.Stderr, &stdout)
if err := cmd.Run(); err != nil {
    return fmt.Errorf("kubectl %v failed: %s\nstdout: %s\nstderr: %s",
        args, err, stdout.String(), stderr.String())
}
```

---

_Reviewed: 2026-04-16T12:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
