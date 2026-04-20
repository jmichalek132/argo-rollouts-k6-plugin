//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// dumpK6OperatorDiagnostics prints TestRun, pod, AR/Rollout status, and controller
// logs on failure to aid debugging. Mirrors the timeout-dump pattern used by
// waitForAnalysisRun.
func dumpK6OperatorDiagnostics(cfg *envconf.Config, namespace string) {
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "testruns", "-n", namespace, "-o", "yaml").Output(); err == nil {
		fmt.Printf("=== TestRuns in %s (diagnostic dump) ===\n%s\n", namespace, string(out))
	}
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "pods", "-n", namespace, "-l", "app=k6", "-o", "wide").Output(); err == nil {
		fmt.Printf("=== k6 runner pods in %s (diagnostic dump) ===\n%s\n", namespace, string(out))
	}
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "pods", "-n", namespace, "-o", "wide").Output(); err == nil {
		fmt.Printf("=== All pods in %s (diagnostic dump) ===\n%s\n", namespace, string(out))
	}
	// AnalysisRun yaml -- exposes status.message where RpcError propagates.
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "analysisruns", "-n", namespace, "-o", "yaml").Output(); err == nil {
		fmt.Printf("=== AnalysisRuns in %s (diagnostic dump) ===\n%s\n", namespace, string(out))
	}
	// Rollout yaml -- exposes status.message/conditions.
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "rollouts", "-n", namespace, "-o", "yaml").Output(); err == nil {
		fmt.Printf("=== Rollouts in %s (diagnostic dump) ===\n%s\n", namespace, string(out))
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
