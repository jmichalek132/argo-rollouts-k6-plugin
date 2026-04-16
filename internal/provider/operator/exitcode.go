package operator

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// k6 exit codes.
// Source: https://github.com/grafana/k6/blob/master/errext/exitcodes/codes.go
const (
	exitCodeSuccess          = 0
	exitCodeThresholdsFailed = 99
)

// exitCodeToRunState maps a k6 process exit code to a provider.RunState (per D-05).
// Exit 0 = all thresholds passed, exit 99 = thresholds breached,
// any other non-zero = infrastructure/script error.
func exitCodeToRunState(code int32) provider.RunState {
	switch code {
	case exitCodeSuccess:
		return provider.Passed
	case exitCodeThresholdsFailed:
		return provider.Failed
	default:
		return provider.Errored
	}
}

// stageToRunState maps a k6-operator TestRun stage to a provider.RunState.
//
// Stage values from k6-operator testrun_types.go:
//
//	initialization, initialized, created, started, stopped, finished, error
//
// IMPORTANT: "finished" returns Running because exit code inspection is needed
// to determine Passed vs Failed (k6-operator issue #577 workaround).
//
// Empty string returns Running to handle freshly created CRs where the stage
// field has not yet been set by the operator (addresses review concern about
// absent stage field).
func stageToRunState(stage string) provider.RunState {
	switch stage {
	case "initialization", "initialized", "created", "started":
		return provider.Running
	case "stopped":
		return provider.Running // stopping but not finished
	case "finished":
		return provider.Running // need exit code check to determine pass/fail
	case "error":
		return provider.Errored
	case "":
		return provider.Running // absent stage field on freshly created CR
	default:
		return provider.Running // unknown stage, keep polling
	}
}

// checkRunnerExitCodes inspects runner pod exit codes for a given TestRun.
// Called after TestRun stage reaches "finished" to determine pass/fail.
//
// Label selector: app=k6, k6_cr=<testRunName>, runner=true
// (per k6-operator pkg/resources/jobs/runner.go)
//
// Edge case handling (addresses HIGH review concerns):
//   - Zero pods: returns error (pods GC'd or race condition)
//   - Restarted containers: returns Errored (k6-operator sets backoff limit 0)
//   - Pods not yet terminated: returns Running (retry on next poll, per Pitfall 2)
//   - Multiple containers: checks all, exit code precedence: Errored > Failed > Passed
func checkRunnerExitCodes(ctx context.Context, client kubernetes.Interface, ns, testRunName string) (provider.RunState, error) {
	selector := fmt.Sprintf("app=k6,k6_cr=%s,runner=true", testRunName)
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return provider.Errored, fmt.Errorf("list runner pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return provider.Errored, fmt.Errorf("no runner pods found for TestRun %s", testRunName)
	}

	worst := provider.Passed
	for _, pod := range pods.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			// Restarted container = infrastructure problem (k6-operator sets backoff limit 0).
			// Addresses review concern: restarted containers with prior exit codes.
			if cs.RestartCount > 0 {
				slog.Warn("runner container restarted unexpectedly",
					"pod", pod.Name,
					"container", cs.Name,
					"restartCount", cs.RestartCount,
					"testRunName", testRunName,
				)
				return provider.Errored, nil
			}
			// Pod still running -- retry on next poll (per Pitfall 2).
			if cs.State.Terminated == nil {
				return provider.Running, nil
			}
			state := exitCodeToRunState(cs.State.Terminated.ExitCode)
			if state == provider.Errored {
				return provider.Errored, nil
			}
			if state == provider.Failed {
				worst = provider.Failed
			}
		}
	}

	slog.Debug("checked runner pod exit codes",
		"testRunName", testRunName,
		"namespace", ns,
		"podCount", len(pods.Items),
		"result", worst,
	)
	return worst, nil
}
