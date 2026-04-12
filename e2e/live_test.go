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
