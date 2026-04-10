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

func TestStepPluginPass(t *testing.T) {
	f := features.New("step plugin pass").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Program mock: test 456 returns run 2001, which transitions Running -> Passed.
			mockServer.OnTriggerRun("456", "2001")
			mockServer.OnGetRunResult("2001",
				testRunResponse{StatusText: "running", ResultText: nil},
				testRunResponse{StatusText: "running", ResultText: nil},
				testRunResponse{StatusText: "finished", ResultText: strPtr("passed")},
			)
			mockServer.OnAggregateMetrics("2001", &aggregateMetrics{
				HTTPReqFailed:   0.0,
				HTTPReqDuration: 100.0,
				HTTPReqs:        1000.0,
			})

			// Create a minimal Service for the Rollout target.
			svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-step-e2e
  namespace: %s
spec:
  selector:
    app: k6-step-e2e
  ports:
    - port: 80
      targetPort: 80
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
				t.Fatalf("create Service: %v", err)
			}

			// Apply the Rollout with step plugin.
			if err := runKubectl(cfg, "apply", "-n", cfg.Namespace(),
				"-f", "e2e/testdata/rollout-step.yaml"); err != nil {
				t.Fatalf("apply Rollout: %v", err)
			}

			return ctx
		}).
		Assess("rollout advances past step plugin", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForRolloutPhase(cfg, "k6-step-e2e", cfg.Namespace(), "Healthy", 3*time.Minute)
			if err != nil {
				t.Fatalf("wait for Rollout: %v", err)
			}
			if phase != "Healthy" {
				t.Errorf("expected Rollout phase Healthy, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "rollout", "k6-step-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "service", "k6-step-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

func TestStepPluginFail(t *testing.T) {
	f := features.New("step plugin fail").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Program mock: test 789 returns run 2002, which transitions Running -> Failed.
			mockServer.OnTriggerRun("789", "2002")
			mockServer.OnGetRunResult("2002",
				testRunResponse{StatusText: "running", ResultText: nil},
				testRunResponse{StatusText: "finished", ResultText: strPtr("failed")},
			)

			// Create a minimal Service for the Rollout target.
			svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-step-fail-e2e
  namespace: %s
spec:
  selector:
    app: k6-step-fail-e2e
  ports:
    - port: 80
      targetPort: 80
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
				t.Fatalf("create Service: %v", err)
			}

			// Create a Rollout that will fail.
			rolloutYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: k6-step-fail-e2e
  namespace: %s
spec:
  replicas: 1
  revisionHistoryLimit: 1
  selector:
    matchLabels:
      app: k6-step-fail-e2e
  template:
    metadata:
      labels:
        app: k6-step-fail-e2e
    spec:
      containers:
        - name: nginx
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: jmichalek132/k6-step
            config:
              testId: "789"
              apiToken: "test-token"
              stackId: "1"
              timeout: "2m"
        - setWeight: 100
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, rolloutYAML); err != nil {
				t.Fatalf("apply Rollout: %v", err)
			}

			return ctx
		}).
		Assess("rollout rolls back on step failure", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForRolloutPhase(cfg, "k6-step-fail-e2e", cfg.Namespace(), "Degraded", 3*time.Minute)
			if err != nil {
				t.Fatalf("wait for Rollout: %v", err)
			}
			if phase != "Degraded" {
				t.Errorf("expected Rollout phase Degraded, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "rollout", "k6-step-fail-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "service", "k6-step-fail-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// waitForRolloutPhase polls the Rollout until it reaches the expected phase or times out.
// For pass scenarios, expectedPhase is "Healthy". For fail scenarios, it is "Degraded".
func waitForRolloutPhase(cfg *envconf.Config, name, namespace, expectedPhase string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		phase, err := getRolloutPhase(cfg, name, namespace)
		if err != nil {
			// Rollout might not exist yet; retry.
			time.Sleep(2 * time.Second)
			continue
		}
		if phase == expectedPhase {
			return phase, nil
		}
		// Also check for abort/error as terminal states.
		if expectedPhase == "Degraded" && phase == "Degraded" {
			return phase, nil
		}
		time.Sleep(3 * time.Second)
	}
	// Return the last known phase for diagnostic purposes.
	phase, _ := getRolloutPhase(cfg, name, namespace)
	return phase, fmt.Errorf("timed out waiting for Rollout %s/%s to reach %s (last phase: %s)",
		namespace, name, expectedPhase, phase)
}

// getRolloutPhase retrieves the current phase of a Rollout via kubectl.
func getRolloutPhase(cfg *envconf.Config, name, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(),
		"get", "rollout", name, "-n", namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var ro struct {
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &ro); err != nil {
		return "", err
	}
	return ro.Status.Phase, nil
}
