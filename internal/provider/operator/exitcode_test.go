package operator

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- exitCodeToRunState ---

func TestExitCodeToRunState(t *testing.T) {
	tests := []struct {
		name     string
		code     int32
		expected provider.RunState
	}{
		{"exit 0 -> Passed", 0, provider.Passed},
		{"exit 99 -> Failed (thresholds breached)", 99, provider.Failed},
		{"exit 1 -> Errored", 1, provider.Errored},
		{"exit 107 -> Errored", 107, provider.Errored},
		{"exit -1 -> Errored", -1, provider.Errored},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, exitCodeToRunState(tt.code))
		})
	}
}

// --- stageToRunState ---

func TestStageToRunState(t *testing.T) {
	tests := []struct {
		name     string
		stage    string
		expected provider.RunState
	}{
		{"initialization -> Running", "initialization", provider.Running},
		{"initialized -> Running", "initialized", provider.Running},
		{"created -> Running", "created", provider.Running},
		{"started -> Running", "started", provider.Running},
		{"stopped -> Running", "stopped", provider.Running},
		{"finished -> Running (needs exit code check)", "finished", provider.Running},
		{"error -> Errored", "error", provider.Errored},
		{"empty string -> Running (absent stage field)", "", provider.Running},
		{"unknown-stage -> Running (keep polling)", "unknown-stage", provider.Running},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stageToRunState(tt.stage))
		})
	}
}

// --- test helpers for runner pods ---

func testRunnerPod(ns, testRunName string, exitCode int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("k6-%s-runner-0", testRunName),
			Namespace: ns,
			Labels: map[string]string{
				"app":    "k6",
				"k6_cr":  testRunName,
				"runner": "true",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "k6",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: exitCode,
						},
					},
				},
			},
		},
	}
}

func testRunningPod(ns, testRunName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("k6-%s-runner-running", testRunName),
			Namespace: ns,
			Labels: map[string]string{
				"app":    "k6",
				"k6_cr":  testRunName,
				"runner": "true",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "k6",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}
}

func testRestartedPod(ns, testRunName string, exitCode int32, restartCount int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("k6-%s-runner-restarted", testRunName),
			Namespace: ns,
			Labels: map[string]string{
				"app":    "k6",
				"k6_cr":  testRunName,
				"runner": "true",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "k6",
					RestartCount: restartCount,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: exitCode,
						},
					},
				},
			},
		},
	}
}

// --- checkRunnerExitCodes ---

func TestCheckRunnerExitCodes_AllPassed(t *testing.T) {
	pod1 := testRunnerPod("ns", "k6-myapp-abc", 0)
	pod1.Name = "k6-myapp-abc-runner-0"
	pod2 := testRunnerPod("ns", "k6-myapp-abc", 0)
	pod2.Name = "k6-myapp-abc-runner-1"

	client := fake.NewSimpleClientset(pod1, pod2)
	state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
	require.NoError(t, err)
	assert.Equal(t, provider.Passed, state)
}

func TestCheckRunnerExitCodes_OneFailed(t *testing.T) {
	pod1 := testRunnerPod("ns", "k6-myapp-abc", 0)
	pod1.Name = "k6-myapp-abc-runner-0"
	pod2 := testRunnerPod("ns", "k6-myapp-abc", 99)
	pod2.Name = "k6-myapp-abc-runner-1"

	client := fake.NewSimpleClientset(pod1, pod2)
	state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
	require.NoError(t, err)
	assert.Equal(t, provider.Failed, state)
}

func TestCheckRunnerExitCodes_OneErrored(t *testing.T) {
	pod1 := testRunnerPod("ns", "k6-myapp-abc", 0)
	pod1.Name = "k6-myapp-abc-runner-0"
	pod2 := testRunnerPod("ns", "k6-myapp-abc", 107)
	pod2.Name = "k6-myapp-abc-runner-1"

	client := fake.NewSimpleClientset(pod1, pod2)
	state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
	require.NoError(t, err)
	assert.Equal(t, provider.Errored, state)
}

func TestCheckRunnerExitCodes_NoPods(t *testing.T) {
	client := fake.NewSimpleClientset() // no pods
	state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no runner pods")
	assert.Equal(t, provider.Errored, state)
}

func TestCheckRunnerExitCodes_PodStillRunning(t *testing.T) {
	pod1 := testRunnerPod("ns", "k6-myapp-abc", 0)
	pod1.Name = "k6-myapp-abc-runner-0"
	pod2 := testRunningPod("ns", "k6-myapp-abc")

	client := fake.NewSimpleClientset(pod1, pod2)
	state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
	require.NoError(t, err)
	assert.Equal(t, provider.Running, state, "pods not yet terminated should return Running (per Pitfall 2)")
}

func TestCheckRunnerExitCodes_RestartedContainer(t *testing.T) {
	pod := testRestartedPod("ns", "k6-myapp-abc", 0, 1)

	client := fake.NewSimpleClientset(pod)
	state, err := checkRunnerExitCodes(context.Background(), client, "ns", "k6-myapp-abc")
	require.NoError(t, err)
	assert.Equal(t, provider.Errored, state, "restarted container should be treated as Errored")
}
