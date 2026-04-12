//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// TestLiveMetricPlugin tests the metric plugin against the real Grafana Cloud k6 API.
// Requires K6_LIVE_TEST=true, K6_CLOUD_TOKEN, and K6_TEST_ID to be set.
// Optionally reads K6_STACK_ID (defaults to "1313689" for the jurajm stack).
func TestLiveMetricPlugin(t *testing.T) {
	if os.Getenv("K6_LIVE_TEST") != "true" {
		t.Skip("live test disabled (set K6_LIVE_TEST=true to enable)")
	}

	apiToken := os.Getenv("K6_CLOUD_TOKEN")
	if apiToken == "" {
		t.Fatal("K6_CLOUD_TOKEN env var is required for live tests")
	}
	testID := os.Getenv("K6_TEST_ID")
	if testID == "" {
		t.Fatal("K6_TEST_ID env var is required for live tests")
	}
	stackID := os.Getenv("K6_STACK_ID")
	if stackID == "" {
		stackID = "1313689"
	}

	f := features.New("live metric plugin pass").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			arYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: live-metric-test
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
            testId: %q
            apiToken: %q
            stackId: %q
            metric: thresholds
`, cfg.Namespace(), testID, apiToken, stackID)

			if err := kubectlApplyStdin(cfg, arYAML); err != nil {
				t.Fatalf("create AnalysisRun: %v", err)
			}
			return ctx
		}).
		Assess("analysisrun succeeds against real k6 API", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForAnalysisRun(cfg, "live-metric-test", cfg.Namespace(), 10*time.Minute)
			if err != nil {
				t.Fatalf("wait for AnalysisRun: %v", err)
			}
			if phase != "Successful" {
				t.Errorf("expected AnalysisRun phase Successful, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "analysisrun", "live-metric-test", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// TestLiveStepPlugin tests the step plugin against the real Grafana Cloud k6 API.
// Requires K6_LIVE_TEST=true, K6_CLOUD_TOKEN, and K6_TEST_ID to be set.
func TestLiveStepPlugin(t *testing.T) {
	if os.Getenv("K6_LIVE_TEST") != "true" {
		t.Skip("live test disabled (set K6_LIVE_TEST=true to enable)")
	}

	apiToken := os.Getenv("K6_CLOUD_TOKEN")
	if apiToken == "" {
		t.Fatal("K6_CLOUD_TOKEN env var is required for live tests")
	}
	testID := os.Getenv("K6_TEST_ID")
	if testID == "" {
		t.Fatal("K6_TEST_ID env var is required for live tests")
	}
	stackID := os.Getenv("K6_STACK_ID")
	if stackID == "" {
		stackID = "1313689"
	}

	f := features.New("live step plugin pass").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-live-step-e2e
  namespace: %s
spec:
  selector:
    app: k6-live-step-e2e
  ports:
    - port: 80
      targetPort: 80
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
				t.Fatalf("create Service: %v", err)
			}

			rolloutYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: k6-live-step-e2e
  namespace: %s
spec:
  replicas: 1
  revisionHistoryLimit: 1
  selector:
    matchLabels:
      app: k6-live-step-e2e
  template:
    metadata:
      labels:
        app: k6-live-step-e2e
    spec:
      containers:
        - name: app
          image: k6-plugin-binaries:e2e
          imagePullPolicy: Never
          command: ["sh", "-c", "sleep 3600"]
          securityContext:
            runAsUser: 65534
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: jmichalek132/k6-step
            config:
              testId: %q
              apiToken: %q
              stackId: %q
              timeout: "10m"
        - setWeight: 100
`, cfg.Namespace(), testID, apiToken, stackID)
			if err := kubectlApplyStdin(cfg, rolloutYAML); err != nil {
				t.Fatalf("apply Rollout: %v", err)
			}

			// Wait for initial deploy (no canary steps on first deploy).
			if _, err := waitForRolloutPhase(cfg, "k6-live-step-e2e", cfg.Namespace(), "Healthy", 2*time.Minute); err != nil {
				t.Fatalf("initial rollout did not become Healthy: %v", err)
			}

			// Trigger update so canary steps execute.
			if err := runKubectl(cfg, "patch", "rollout", "k6-live-step-e2e",
				"-n", cfg.Namespace(), "--type=merge",
				"-p", `{"spec":{"template":{"metadata":{"annotations":{"test/run":"2"}}}}}`); err != nil {
				t.Fatalf("patch rollout to trigger update: %v", err)
			}
			return ctx
		}).
		Assess("rollout advances past step plugin against real k6 API", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForRolloutPhase(cfg, "k6-live-step-e2e", cfg.Namespace(), "Healthy", 10*time.Minute)
			if err != nil {
				t.Fatalf("wait for Rollout: %v", err)
			}
			if phase != "Healthy" {
				t.Errorf("expected Rollout phase Healthy, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "rollout", "k6-live-step-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "service", "k6-live-step-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// liveCredentials reads and validates the shared env vars for live tests.
func liveCredentials(t *testing.T, testIDVar string) (apiToken, stackID, testID string) {
	t.Helper()
	apiToken = os.Getenv("K6_CLOUD_TOKEN")
	if apiToken == "" {
		t.Fatal("K6_CLOUD_TOKEN env var is required for live tests")
	}
	testID = os.Getenv(testIDVar)
	if testID == "" {
		t.Fatalf("%s env var is required for live tests", testIDVar)
	}
	stackID = os.Getenv("K6_STACK_ID")
	if stackID == "" {
		stackID = "1313689"
	}
	return
}

// TestLiveMetricPluginFail verifies the metric plugin reports failure when k6 thresholds are breached.
// Requires K6_LIVE_TEST=true, K6_CLOUD_TOKEN, and K6_FAILING_TEST_ID.
func TestLiveMetricPluginFail(t *testing.T) {
	if os.Getenv("K6_LIVE_TEST") != "true" {
		t.Skip("live test disabled (set K6_LIVE_TEST=true to enable)")
	}
	apiToken, stackID, testID := liveCredentials(t, "K6_FAILING_TEST_ID")

	f := features.New("live metric plugin fail").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			arYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  name: live-metric-fail-test
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
            testId: %q
            apiToken: %q
            stackId: %q
            metric: thresholds
`, cfg.Namespace(), testID, apiToken, stackID)

			if err := kubectlApplyStdin(cfg, arYAML); err != nil {
				t.Fatalf("create AnalysisRun: %v", err)
			}
			return ctx
		}).
		Assess("analysisrun fails when k6 thresholds are breached", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForAnalysisRun(cfg, "live-metric-fail-test", cfg.Namespace(), 10*time.Minute)
			if err != nil {
				t.Fatalf("wait for AnalysisRun: %v", err)
			}
			if phase != "Failed" {
				t.Errorf("expected AnalysisRun phase Failed, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "analysisrun", "live-metric-fail-test", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}

// TestLiveStepPluginFail verifies the step plugin rolls back the Rollout when a k6 test fails.
// Requires K6_LIVE_TEST=true, K6_CLOUD_TOKEN, and K6_FAILING_TEST_ID.
func TestLiveStepPluginFail(t *testing.T) {
	if os.Getenv("K6_LIVE_TEST") != "true" {
		t.Skip("live test disabled (set K6_LIVE_TEST=true to enable)")
	}
	apiToken, stackID, testID := liveCredentials(t, "K6_FAILING_TEST_ID")

	f := features.New("live step plugin fail").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			svcYAML := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: k6-live-step-fail-e2e
  namespace: %s
spec:
  selector:
    app: k6-live-step-fail-e2e
  ports:
    - port: 80
      targetPort: 80
`, cfg.Namespace())
			if err := kubectlApplyStdin(cfg, svcYAML); err != nil {
				t.Fatalf("create Service: %v", err)
			}

			rolloutYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: k6-live-step-fail-e2e
  namespace: %s
spec:
  replicas: 1
  revisionHistoryLimit: 1
  selector:
    matchLabels:
      app: k6-live-step-fail-e2e
  template:
    metadata:
      labels:
        app: k6-live-step-fail-e2e
    spec:
      containers:
        - name: app
          image: k6-plugin-binaries:e2e
          imagePullPolicy: Never
          command: ["sh", "-c", "sleep 3600"]
          securityContext:
            runAsUser: 65534
  strategy:
    canary:
      steps:
        - setWeight: 20
        - plugin:
            name: jmichalek132/k6-step
            config:
              testId: %q
              apiToken: %q
              stackId: %q
              timeout: "10m"
        - setWeight: 100
`, cfg.Namespace(), testID, apiToken, stackID)
			if err := kubectlApplyStdin(cfg, rolloutYAML); err != nil {
				t.Fatalf("apply Rollout: %v", err)
			}

			if _, err := waitForRolloutPhase(cfg, "k6-live-step-fail-e2e", cfg.Namespace(), "Healthy", 2*time.Minute); err != nil {
				t.Fatalf("initial rollout did not become Healthy: %v", err)
			}

			if err := runKubectl(cfg, "patch", "rollout", "k6-live-step-fail-e2e",
				"-n", cfg.Namespace(), "--type=merge",
				"-p", `{"spec":{"template":{"metadata":{"annotations":{"test/run":"2"}}}}}`); err != nil {
				t.Fatalf("patch rollout to trigger update: %v", err)
			}
			return ctx
		}).
		Assess("rollout rolls back when step plugin fails", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			phase, err := waitForRolloutPhase(cfg, "k6-live-step-fail-e2e", cfg.Namespace(), "Degraded", 10*time.Minute)
			if err != nil {
				t.Fatalf("wait for Rollout: %v", err)
			}
			if phase != "Degraded" {
				t.Errorf("expected Rollout phase Degraded, got %s", phase)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			_ = runKubectl(cfg, "delete", "rollout", "k6-live-step-fail-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			_ = runKubectl(cfg, "delete", "service", "k6-live-step-fail-e2e", "-n", cfg.Namespace(), "--ignore-not-found")
			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}
