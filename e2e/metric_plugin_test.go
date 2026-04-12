//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

func TestMetricPluginPass(t *testing.T) {
	if os.Getenv("K6_LIVE_TEST") == "true" {
		t.Skip("mock test skipped in live mode")
	}
	f := features.New("metric plugin pass").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Apply the AnalysisTemplate.
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/analysistemplate-thresholds.yaml"); err != nil {
				t.Fatalf("apply AnalysisTemplate: %v", err)
			}

			// Create an AnalysisRun targeting the static mock scenario metric-pass-1.
			arYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: metric-pass-test
  namespace: %s
spec:
  metrics:
    - name: k6-thresholds
      interval: 5s
      count: 2
      successCondition: "result == 1"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            testId: "100"
            apiToken: "test-token"
            stackId: "1"
            metric: thresholds
`, cfg.Namespace())

			if err := kubectlApplyStdin(cfg, arYAML); err != nil {
				t.Fatalf("create AnalysisRun: %v", err)
			}

			return ctx
		}).
		Assess("analysisrun succeeds", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForAnalysisRun(cfg, "metric-pass-test", cfg.Namespace(), 2*time.Minute)
			if err != nil {
				t.Fatalf("wait for AnalysisRun: %v", err)
			}
			if phase != "Successful" {
				t.Errorf("expected AnalysisRun phase Successful, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "analysisrun", "metric-pass-test", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "analysistemplate", "k6-threshold-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

func TestMetricPluginFail(t *testing.T) {
	if os.Getenv("K6_LIVE_TEST") == "true" {
		t.Skip("mock test skipped in live mode")
	}
	f := features.New("metric plugin fail").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Apply the AnalysisTemplate.
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "testdata/analysistemplate-thresholds.yaml"); err != nil {
				t.Fatalf("apply AnalysisTemplate: %v", err)
			}

			// Create an AnalysisRun targeting the static mock scenario metric-fail-1.
			arYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: metric-fail-test
  namespace: %s
spec:
  metrics:
    - name: k6-thresholds
      interval: 5s
      count: 2
      successCondition: "result == 1"
      failureLimit: 0
      provider:
        plugin:
          jmichalek132/k6:
            testId: "101"
            apiToken: "test-token"
            stackId: "1"
            metric: thresholds
`, cfg.Namespace())

			if err := kubectlApplyStdin(cfg, arYAML); err != nil {
				t.Fatalf("create AnalysisRun: %v", err)
			}

			return ctx
		}).
		Assess("analysisrun fails", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForAnalysisRun(cfg, "metric-fail-test", cfg.Namespace(), 2*time.Minute)
			if err != nil {
				t.Fatalf("wait for AnalysisRun: %v", err)
			}
			if phase != "Failed" {
				t.Errorf("expected AnalysisRun phase Failed, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "analysisrun", "metric-fail-test", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "analysistemplate", "k6-threshold-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// waitForAnalysisRun polls the AnalysisRun until it reaches a terminal phase
// (Successful, Failed, Error) or the timeout is exceeded.
// Returns the last observed phase and an error on timeout.
func waitForAnalysisRun(cfg *envconf.Config, name, namespace string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastPhase string
	for time.Now().Before(deadline) {
		phase, err := getAnalysisRunPhase(cfg, name, namespace)
		if err != nil {
			// AnalysisRun might not exist yet; retry.
			time.Sleep(2 * time.Second)
			continue
		}
		lastPhase = phase
		switch phase {
		case "Successful", "Failed", "Error", "Inconclusive":
			return phase, nil
		}
		time.Sleep(3 * time.Second)
	}
	// Dump full AnalysisRun JSON for post-mortem diagnostics.
	if out, err := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "analysisrun", name, "-n", namespace, "-o", "json").Output(); err == nil {
		fmt.Printf("=== AnalysisRun %s/%s (timeout dump) ===\n%s\n", namespace, name, string(out))
	}
	return lastPhase, fmt.Errorf("timed out waiting for AnalysisRun %s/%s (last phase: %s)", namespace, name, lastPhase)
}

// getAnalysisRunPhase retrieves the current phase of an AnalysisRun via kubectl.
func getAnalysisRunPhase(cfg *envconf.Config, name, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "analysisrun", name, "-n", namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var ar struct {
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &ar); err != nil {
		return "", err
	}
	return ar.Status.Phase, nil
}
