//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// TestK6OperatorStepPass validates the step plugin with the k6-operator provider.
// It proves the full path: plugin creates a TestRun CR, k6-operator runs the real
// k6 binary against the in-cluster mock server, and the Rollout advances to Healthy.
//
// This test does NOT use the mock k6 Cloud API -- it exercises the real k6-operator
// controller installed in kind. No K6_LIVE_TEST skip guard.
func TestK6OperatorStepPass(t *testing.T) {
	f := features.New("k6-operator step pass").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Create a minimal Service for the Rollout target.
			svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-step-k6op-e2e
  namespace: %s
spec:
  selector:
    app: k6-step-k6op-e2e
  ports:
    - port: 80
      targetPort: 80
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
				t.Fatalf("create Service: %v", err)
			}

			// Create the k6 script ConfigMap in the test namespace.
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/k6-script-configmap.yaml"); err != nil {
				t.Fatalf("apply k6 script ConfigMap: %v", err)
			}

			// Apply the Rollout for the first time (no canary steps on initial deploy).
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/rollout-step-k6op.yaml"); err != nil {
				t.Fatalf("apply Rollout: %v", err)
			}

			// Wait for initial deployment to become Healthy.
			if _, err := waitForRolloutPhase(cfg, "k6-step-k6op-e2e", cfg.Namespace(), "Healthy", 2*time.Minute); err != nil {
				t.Fatalf("initial rollout did not become Healthy: %v", err)
			}

			// Clear any TestRun CRs that may have leaked from transient reconciles
			// or prior test runs. This ensures the TestRun count asserted in Assess
			// reflects ONLY the canary step triggered by the patch below -- proving
			// the step plugin actually executed, not that a pre-existing TestRun
			// happened to be lying around.
			_ = runKubectl(cfg, "delete", "testruns", "--all", "-n", cfg.Namespace(), "--ignore-not-found")

			// Trigger canary via annotation patch so the step plugin executes.
			if err := runKubectl(cfg, "patch", "rollout", "k6-step-k6op-e2e",
				"-n", cfg.Namespace(), "--type=merge",
				"-p", `{"spec":{"template":{"metadata":{"annotations":{"test/run":"2"}}}}}`); err != nil {
				t.Fatalf("patch rollout to trigger update: %v", err)
			}

			return ctx
		}).
		Assess("rollout advances past k6-operator step and TestRun was created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Poll for TestRun creation BEFORE waiting for Healthy. This avoids a race
			// where the TestRun is garbage-collected (or cleaned up by a future
			// Terminate hook) between Rollout reaching Healthy and the assertion
			// reading the list. Observing the TestRun first also proves the step
			// plugin executed -- not just that the Rollout happens to be Healthy.
			testRunDeadline := time.Now().Add(5 * time.Minute)
			var seenTestRuns int
			for time.Now().Before(testRunDeadline) {
				n, err := countTestRuns(cfg, cfg.Namespace())
				if err == nil && n >= 1 {
					seenTestRuns = n
					break
				}
				time.Sleep(3 * time.Second)
			}
			if seenTestRuns < 1 {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("timed out waiting for step plugin to create a TestRun CR")
			}

			// Phase 08.2 regression: the fixture omits `parallelism` from the
			// plugin step config; the plugin must default Parallelism=1 at the
			// buildTestRun site so the TestRun is not paused. Read the CR back
			// directly -- Rollout Healthy alone is weaker signal.
			par, err := getTestRunParallelism(cfg, cfg.Namespace())
			if err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("read TestRun.spec.parallelism: %v", err)
			}
			if par != "1" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("expected TestRun.spec.parallelism=1 (defaulted from unset cfg.Parallelism), got %q", par)
			}

			// 5-minute timeout: real k6 execution + pod scheduling takes longer than mocked tests.
			phase, err := waitForRolloutPhase(cfg, "k6-step-k6op-e2e", cfg.Namespace(), "Healthy", 5*time.Minute)
			if err != nil {
				// Diagnostic dumps on timeout/failure.
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("wait for Rollout: %v", err)
			}
			if phase != "Healthy" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Errorf("expected Rollout phase Healthy, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "rollout", "k6-step-k6op-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "service", "k6-step-k6op-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "configmap", "k6-e2e-script", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "testruns", "--all", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// TestK6OperatorMetricPass validates the metric plugin with the k6-operator provider
// AND full-path metric extraction. Asserts the actual metric value from
// AnalysisRun metricResults, proving:
//   - A TestRun CR was created by the metric plugin
//   - k6-operator ran the k6 binary
//   - k6 produced handleSummary JSON to stdout
//   - The plugin read pod logs and extracted handleSummary
//   - The plugin evaluated thresholds and returned result=1
func TestK6OperatorMetricPass(t *testing.T) {
	f := features.New("k6-operator metric pass").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Create the k6 script ConfigMap in the test namespace.
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/k6-script-configmap.yaml"); err != nil {
				t.Fatalf("apply k6 script ConfigMap: %v", err)
			}

			// Apply the AnalysisTemplate (for reference; AnalysisRun below is self-contained).
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/analysistemplate-k6op.yaml"); err != nil {
				t.Fatalf("apply AnalysisTemplate: %v", err)
			}

			// Create an inline AnalysisRun targeting the k6-operator provider.
			arYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: k6op-metric-pass-test
  namespace: %s
spec:
  metrics:
    - name: k6-thresholds
      interval: 10s
      count: 1
      successCondition: "result == 1"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            provider: k6-operator
            configMapRef:
              name: k6-e2e-script
              key: test.js
            runnerImage: "grafana/k6:0.56.0"
            metric: thresholds
`, cfg.Namespace())

			if err := kubectlApplyStdin(cfg, arYAML); err != nil {
				t.Fatalf("create AnalysisRun: %v", err)
			}
			return ctx
		}).
		Assess("analysisrun succeeds with extracted metric value", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// 5-minute timeout: real k6 execution + pod scheduling takes longer than mocked tests.
			phase, err := waitForAnalysisRun(cfg, "k6op-metric-pass-test", cfg.Namespace(), 5*time.Minute)
			if err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("wait for AnalysisRun: %v", err)
			}
			if phase != "Successful" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Errorf("expected AnalysisRun phase Successful, got %s", phase)
			}

			// Full-path validation: read the actual metric value from metricResults.
			// Proves the plugin read pod logs, parsed handleSummary, and evaluated thresholds.
			value, err := getAnalysisRunMetricValue(cfg, "k6op-metric-pass-test", cfg.Namespace(), "k6-thresholds")
			if err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("read metric value from AnalysisRun: %v", err)
			}
			// Argo Rollouts stores metric values as strings. thresholds==pass -> "1".
			if value != "1" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Errorf("expected metric value %q, got %q", "1", value)
			}

			// Phase 08.2 regression (mirror of TestK6OperatorStepPass):
			// the AnalysisTemplate-driven path also omits `parallelism`.
			// The plugin must default Parallelism=1 at buildTestRun. Read back
			// directly; AnalysisRun Successful alone is weaker signal.
			par, err := getTestRunParallelism(cfg, cfg.Namespace())
			if err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("read TestRun.spec.parallelism: %v", err)
			}
			if par != "1" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("expected TestRun.spec.parallelism=1 (defaulted from unset cfg.Parallelism), got %q", par)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "analysisrun", "k6op-metric-pass-test", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "analysistemplate", "k6-operator-threshold-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "configmap", "k6-e2e-script", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "testruns", "--all", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// getAnalysisRunMetricValue reads the first measurement value for the named metric
// from an AnalysisRun's status.metricResults. Returns the string value as stored
// by Argo Rollouts (result=1 for passed thresholds).
func getAnalysisRunMetricValue(cfg *envconf.Config, name, namespace, metricName string) (string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "analysisrun", name, "-n", namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var ar struct {
		Status struct {
			MetricResults []struct {
				Name         string `json:"name"`
				Measurements []struct {
					Value string `json:"value"`
				} `json:"measurements"`
			} `json:"metricResults"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &ar); err != nil {
		return "", err
	}
	for _, mr := range ar.Status.MetricResults {
		if mr.Name == metricName && len(mr.Measurements) > 0 {
			return mr.Measurements[0].Value, nil
		}
	}
	return "", fmt.Errorf("metric %q not found in AnalysisRun %s/%s", metricName, namespace, name)
}

// countTestRuns returns the number of TestRun CRs in the given namespace.
// Used by step tests to verify the plugin actually created a CR.
func countTestRuns(cfg *envconf.Config, namespace string) (int, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "testruns", "-n", namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return 0, err
	}
	return len(list.Items), nil
}

// getTestRunParallelism returns the spec.parallelism of the first TestRun CR in
// the namespace as a string (Kubernetes JSON marshals int32 as a number; we
// extract via jsonpath and compare the string form "1"). Phase 08.2 regression
// guard: proves the plugin emitted the defaulted value when the plugin config
// omitted `parallelism`.
func getTestRunParallelism(cfg *envconf.Config, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "testruns", "-n", namespace,
		"-o", "jsonpath={.items[0].spec.parallelism}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl get testruns jsonpath: %w", err)
	}
	return string(out), nil
}

// TestK6OperatorCombinedCanaryARDeletion proves D-07 owner-reference precedence
// (AR > Rollout) under real Kubernetes cascading GC. It deploys a Rollout that
// runs the step plugin AND a background AnalysisTemplate (metric plugin) in
// parallel against the k6-operator provider. While both TestRun CRs are in
// stage "started" (k6-operator v1.3.x stage enum has no "running" literal), the
// test deletes the AnalysisRun; kube-apiserver's GC then cascades the AR-owned
// TestRun (AR ownerRef) but must leave the Rollout-owned step TestRun untouched.
//
// This complements the Phase 11 unit tests on Provider.Cleanup / GarbageCollect:
// those cover the plugin's application-layer cleanup path; this test covers the
// kube-apiserver owner-reference cascade path. Both must hold independently.
//
// Budget: ~10 min (k6 duration 120s + stage=started wait + up to 5 min async kube GC + teardown).
func TestK6OperatorCombinedCanaryARDeletion(t *testing.T) {
	f := features.New("k6-operator combined canary AR deletion GC cascade").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Service for the Rollout target.
			svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-combined-canary-k6op-e2e
  namespace: %s
spec:
  selector:
    app: k6-combined-canary-k6op-e2e
  ports:
    - port: 80
      targetPort: 80
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
				t.Fatalf("create Service: %v", err)
			}

			// Long-running k6 script ConfigMap (duration: 120s so both
			// TestRuns stay in stage=started long enough for the AR-delete
			// window -- the short k6-e2e-script fixture finishes in ~1s).
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/configmap-script-k6op-long.yaml"); err != nil {
				t.Fatalf("apply long-script ConfigMap: %v", err)
			}

			// Rollout (includes AnalysisTemplate doc in the same file).
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/rollout-combined-canary-k6op.yaml"); err != nil {
				t.Fatalf("apply Rollout+AnalysisTemplate: %v", err)
			}

			// Wait for initial deployment Healthy (canary steps are skipped
			// on the first deploy; background analysis does not run for the
			// initial rollout either).
			if _, err := waitForRolloutPhase(cfg, "k6-combined-canary-k6op-e2e",
				cfg.Namespace(), "Healthy", 2*time.Minute); err != nil {
				t.Fatalf("initial rollout did not become Healthy: %v", err)
			}

			// Clear any TestRun/AR leakage from prior tests. Keeps the
			// polling loops below unambiguous (they count only CRs from the
			// canary patched in this Setup).
			_ = runKubectl(cfg, "delete", "testruns", "--all",
				"-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "analysisruns", "--all",
				"-n", cfg.Namespace(), "--ignore-not-found")

			// Trigger canary via annotation patch. Both the step-plugin
			// step and the background AnalysisRun (startingStep: 1) start
			// concurrently once the canary reaches step index 1.
			if err := runKubectl(cfg, "patch", "rollout", "k6-combined-canary-k6op-e2e",
				"-n", cfg.Namespace(), "--type=merge",
				"-p", `{"spec":{"template":{"metadata":{"annotations":{"test/run":"2"}}}}}`); err != nil {
				t.Fatalf("patch rollout: %v", err)
			}

			return ctx
		}).
		Assess("AR-owned TestRun GC'd on AR delete; Rollout-owned TestRun survives", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// (1) Wait for BOTH TestRuns to exist -- AR-owned (from the
			//     background metric plugin) and Rollout-owned (from the
			//     step plugin). 4-minute deadline accommodates the canary
			//     reconciliation cycle on slower kind hosts.
			var arTRs, roTRs []string
			bothExistDeadline := time.Now().Add(4 * time.Minute)
			for time.Now().Before(bothExistDeadline) {
				a, errA := getTestRunsOwnedBy(cfg, cfg.Namespace(), "AnalysisRun", "")
				r, errR := getTestRunsOwnedBy(cfg, cfg.Namespace(), "Rollout", "")
				if errA == nil && errR == nil && len(a) >= 1 && len(r) >= 1 {
					arTRs, roTRs = a, r
					break
				}
				time.Sleep(3 * time.Second)
			}
			if len(arTRs) == 0 || len(roTRs) == 0 {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("timed out waiting for both AR-owned and Rollout-owned TestRuns to exist; arTRs=%v roTRs=%v", arTRs, roTRs)
			}

			// (2) Wait for both to reach stage "started" (k6-operator v1.3.x
			//     enum: initialization, initialized, created, started,
			//     stopped, finished, error -- there is no "running" literal,
			//     see k6-operator api/v1alpha1/testrun_types.go). Fatal on
			//     timeout: proceeding without both stages observed makes the
			//     subsequent cascade assertion inconclusive.
			startedDeadline := time.Now().Add(3 * time.Minute)
			var arStage, roStage string
			for time.Now().Before(startedDeadline) {
				arStage, _ = getTestRunStage(cfg, arTRs[0], cfg.Namespace())
				roStage, _ = getTestRunStage(cfg, roTRs[0], cfg.Namespace())
				if arStage == "started" && roStage == "started" {
					break
				}
				time.Sleep(3 * time.Second)
			}
			if arStage != "started" || roStage != "started" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("timed out waiting for both TestRuns to reach stage=started: arStage=%q roStage=%q", arStage, roStage)
			}

			// (3) Discover the AR name + UID and delete it. Both are
			//     captured BEFORE the delete:
			//       - Name is used for the kubectl delete call.
			//       - UID pins the cascade poll in (4) to THIS AR
			//         instance; argo-rollouts reconciles a replacement
			//         background AR under the SAME name shortly after
			//         our delete (see rollout/analysis.go
			//         reconcileBackgroundAnalysisRun +
			//         needsNewAnalysisRun(nil)==true in
			//         argo-rollouts@v1.9.0). Without UID filtering the
			//         poll would falsely count the new AR's TestRun.
			//
			//     Propagation policy note: the emitted AR->TestRun
			//     OwnerReference carries Controller=nil and
			//     BlockOwnerDeletion=nil (locked by Phase 13 IN-03 unit
			//     tests; best-effort cleanup contract). Under that shape
			//     --cascade=foreground is a NO-OP: the apiserver only
			//     synchronously blocks on dependents whose
			//     blockOwnerDeletion=true, and with none present the
			//     foregroundDeletion finalizer is removed immediately
			//     and dependents fall through to the async
			//     kube-controller-manager GC loop -- identical to
			//     --cascade=background. We therefore use the default
			//     (background) propagation and rely on a generous
			//     deadline in (4) to absorb GC-controller latency on
			//     resource-starved kind hosts. See
			//     .planning/debug/combined-canary-cascade-gc.md for the
			//     full reasoning (including two prior false-positive
			//     fixes: one that assumed --cascade=foreground was
			//     load-bearing, and one that merely extended the
			//     deadline without accounting for AR recreation).
			ars, err := listAnalysisRuns(cfg, cfg.Namespace())
			if err != nil || len(ars) == 0 {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("no AnalysisRun found to delete: err=%v ars=%v", err, ars)
			}
			arName := ars[0]
			arUID, err := getAnalysisRunUID(cfg, arName, cfg.Namespace())
			if err != nil || arUID == "" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("read AnalysisRun UID for %s: err=%v uid=%q", arName, err, arUID)
			}
			if err := runKubectl(cfg, "delete", "analysisrun", arName,
				"-n", cfg.Namespace(), "--ignore-not-found"); err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("kubectl delete analysisrun %s: %v", arName, err)
			}

			// (4) Assert AR-owned TestRun disappears within 5 minutes
			//     (async kube cascade GC). Filter cascade poll by the
			//     CAPTURED arName so a recreated background AR's
			//     TestRun is not counted as "still present". The 5m
			//     deadline is deliberately generous: on loaded kind the
			//     kube-controller-manager GC loop can take tens of
			//     seconds to enumerate dependents after the AR tombstone
			//     lands, and the TestRun itself owns runner Jobs/Pods
			//     that kube must remove before the TestRun's own delete
			//     completes. Poll interval 5s (not 3s) to cut apiserver
			//     chatter over the longer window.
			cascadeDeadline := time.Now().Add(5 * time.Minute)
			var arRemaining []string
			for time.Now().Before(cascadeDeadline) {
				// UID filter is load-bearing: argo-rollouts recreates a
				// deleted background AR with the SAME name but a NEW UID
				// (see rollout/analysis.go reconcileBackgroundAnalysisRun
				// + needsNewAnalysisRun(nil)==true in argo-rollouts
				// v1.9.0). Name-only filtering matches the new AR's
				// TestRun too, producing false "still present"
				// assertions. UID pins the poll to the exact AR we
				// deleted.
				arRemaining, err = getTestRunsOwnedByUID(cfg, cfg.Namespace(), "AnalysisRun", arName, arUID)
				if err == nil && len(arRemaining) == 0 {
					break
				}
				time.Sleep(5 * time.Second)
			}
			if len(arRemaining) != 0 {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("TestRun(s) owned by deleted AnalysisRun %q (uid=%s) still present 5m after delete; kube cascade GC did not fire: %v", arName, arUID, arRemaining)
			}

			// (5) Assert Rollout-owned TestRun survives. Short settle
			//     window catches any delayed ripple that would (wrongly)
			//     also remove the Rollout-owned TestRun.
			time.Sleep(5 * time.Second)
			roSurvivors, err := getTestRunsOwnedBy(cfg, cfg.Namespace(), "Rollout", "k6-combined-canary-k6op-e2e")
			if err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("list Rollout-owned TestRuns: %v", err)
			}
			if len(roSurvivors) == 0 {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("Rollout-owned TestRun was unexpectedly removed by AR cascade; D-07 precedence broken")
			}

			// (6) Verify the surviving TestRun still carries the managed-by label.
			labelValue, err := getTestRunLabel(cfg, roSurvivors[0], cfg.Namespace(), "app.kubernetes.io/managed-by")
			if err != nil {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Fatalf("read managed-by label on %s: %v", roSurvivors[0], err)
			}
			if labelValue != "argo-rollouts-k6-plugin" {
				dumpK6OperatorDiagnostics(cfg, cfg.Namespace())
				t.Errorf("expected managed-by=argo-rollouts-k6-plugin on surviving TestRun, got %q", labelValue)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "rollout", "k6-combined-canary-k6op-e2e",
				"-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "analysistemplate", "k6-operator-combined-e2e",
				"-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "analysisruns", "--all",
				"-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "service", "k6-combined-canary-k6op-e2e",
				"-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "configmap", "k6-e2e-script-long",
				"-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "testruns", "--all",
				"-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// getTestRunsOwnedBy returns the names of TestRun CRs in `namespace` whose
// metadata.ownerReferences contain an entry matching ownerKind (and, if
// ownerName is non-empty, also matching ownerName). The match is
// case-sensitive on ownerKind -- use "AnalysisRun" or "Rollout" to mirror
// the Kind strings parentOwnerRef emits in internal/provider/operator/testrun.go.
func getTestRunsOwnedBy(cfg *envconf.Config, namespace, ownerKind, ownerName string) ([]string, error) {
	return getTestRunsOwnedByUID(cfg, namespace, ownerKind, ownerName, "")
}

// getTestRunsOwnedByUID is the UID-aware variant of getTestRunsOwnedBy. When
// ownerUID is non-empty, only TestRuns whose ownerRef.UID matches exactly are
// returned. This is the correct filter for cascade-GC assertions: argo-rollouts
// recreates a deleted background AnalysisRun with the SAME name but a NEW UID
// (see reconcileBackgroundAnalysisRun + needsNewAnalysisRun(nil) => true in
// argo-rollouts@v1.9.0 rollout/analysis.go), so name-based filtering would
// match the new AR's TestRun and produce a false "still present" assertion.
// UID is stable across recreations; the old AR's UID never reappears.
func getTestRunsOwnedByUID(cfg *envconf.Config, namespace, ownerKind, ownerName, ownerUID string) ([]string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "testruns", "-n", namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name            string `json:"name"`
				OwnerReferences []struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
					UID  string `json:"uid"`
				} `json:"ownerReferences"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var names []string
	for _, item := range list.Items {
		for _, ref := range item.Metadata.OwnerReferences {
			if ref.Kind != ownerKind {
				continue
			}
			if ownerName != "" && ref.Name != ownerName {
				continue
			}
			if ownerUID != "" && ref.UID != ownerUID {
				continue
			}
			names = append(names, item.Metadata.Name)
			break
		}
	}
	return names, nil
}

// getTestRunStage returns status.stage of a named TestRun CR.
// k6-operator v1.3.x stage enum: "initialization", "initialized", "created",
// "started", "stopped", "finished", "error". There is no "running" literal.
// The combined-canary test waits for "started" on both TestRuns before
// issuing kubectl delete analysisrun.
func getTestRunStage(cfg *envconf.Config, name, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "testrun", name, "-n", namespace,
		"-o", "jsonpath={.status.stage}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// listAnalysisRuns returns the names of all AnalysisRun CRs in the namespace.
// Used to discover the Rollout-spawned AR name (Argo names it
// `<rollout>-<revision>-<template>` but listing is safer than predicting).
func listAnalysisRuns(cfg *envconf.Config, namespace string) ([]string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "analysisruns", "-n", namespace,
		"-o", "jsonpath={.items[*].metadata.name}")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// getAnalysisRunUID returns the metadata.uid of a named AnalysisRun. Used by
// TestK6OperatorCombinedCanaryARDeletion to pin the cascade poll to the exact
// AR instance being deleted, so a subsequently-recreated background AR (same
// name, new UID) cannot appear as a false "still present" TestRun.
func getAnalysisRunUID(cfg *envconf.Config, name, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "analysisrun", name, "-n", namespace,
		"-o", "jsonpath={.metadata.uid}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// getTestRunLabel returns the value of a named label on a TestRun CR.
// kubectl jsonpath requires escaping dots and slashes in label keys.
func getTestRunLabel(cfg *envconf.Config, name, namespace, label string) (string, error) {
	jp := fmt.Sprintf("jsonpath={.metadata.labels.%s}",
		strings.NewReplacer(".", `\.`, "/", `\/`).Replace(label))
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "testrun", name, "-n", namespace, "-o", jp)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// k6DiagDump describes a single kubectl-get diagnostic dump for
// dumpK6OperatorDiagnostics. label holds plain text (no format verbs); the fixed
// format string lives inside emitK6Dump so future contributors cannot accidentally
// break the output-format invariant.
type k6DiagDump struct {
	resource  string   // e.g., "testruns", "analysisruns", "rollouts", "pods"
	namespace string   // target namespace (substituted into the fixed header)
	format    string   // "yaml", "wide", "json"
	label     string   // plain-text section title, e.g., "TestRuns", "k6 runner pods"
	selectors []string // optional extra kubectl args, e.g., []string{"-l", "app=k6"}
}

// emitK6Dump runs one `kubectl get` and prints its output under a fixed header.
// The format literal is fixed and uses exactly TWO %s verbs (label, namespace) --
// adding a new dump entry only requires a plain label string.
func emitK6Dump(cfg *envconf.Config, d k6DiagDump) {
	args := []string{"--kubeconfig", cfg.KubeconfigFile(), "get", d.resource, "-n", d.namespace, "-o", d.format}
	args = append(args, d.selectors...)
	if out, err := exec.Command("kubectl", args...).Output(); err == nil {
		fmt.Printf("=== %s in %s (diagnostic dump) ===\n%s\n", d.label, d.namespace, string(out))
	}
}

// dumpK6OperatorDiagnostics prints TestRun, pod, AR/Rollout status, and controller
// logs on failure to aid debugging. Mirrors the timeout-dump pattern used by
// waitForAnalysisRun. The 5 kubectl-get dumps are driven by a declarative slice;
// controller-logs dumps use CombinedOutput + --tail and stay explicit.
func dumpK6OperatorDiagnostics(cfg *envconf.Config, namespace string) {
	dumps := []k6DiagDump{
		{resource: "testruns", namespace: namespace, format: "yaml", label: "TestRuns"},
		{resource: "pods", namespace: namespace, format: "wide", label: "k6 runner pods", selectors: []string{"-l", "app=k6"}},
		{resource: "pods", namespace: namespace, format: "wide", label: "All pods"},
		// AnalysisRun yaml -- exposes status.message where RpcError propagates.
		{resource: "analysisruns", namespace: namespace, format: "yaml", label: "AnalysisRuns"},
		// Rollout yaml -- exposes status.message/conditions.
		{resource: "rollouts", namespace: namespace, format: "yaml", label: "Rollouts"},
	}
	for _, d := range dumps {
		emitK6Dump(cfg, d)
	}
	// Argo-rollouts controller logs -- plugin stderr is piped here via go-plugin.
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"logs", "-n", "argo-rollouts", "deploy/argo-rollouts", "--tail=500").CombinedOutput(); err == nil {
		fmt.Printf("=== argo-rollouts controller logs (tail 500) ===\n%s\n", string(out))
	}
	// k6-operator controller logs -- in case k6-operator rejects TestRuns.
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"logs", "-n", "k6-operator-system", "deploy/k6-operator-controller-manager", "--tail=200").CombinedOutput(); err == nil {
		fmt.Printf("=== k6-operator controller logs (tail 200) ===\n%s\n", string(out))
	}
}
