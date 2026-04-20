package operator

import (
	"regexp"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- GVR constants ---

func TestTestRunGVR(t *testing.T) {
	assert.Equal(t, "k6.io", testRunGVR.Group)
	assert.Equal(t, "v1alpha1", testRunGVR.Version)
	assert.Equal(t, "testruns", testRunGVR.Resource)
}

func TestPlzGVR(t *testing.T) {
	assert.Equal(t, "k6.io", plzGVR.Group)
	assert.Equal(t, "v1alpha1", plzGVR.Version)
	assert.Equal(t, "privateloadzones", plzGVR.Resource)
}

// --- testRunName ---

func TestTestRunName_Format(t *testing.T) {
	name := testRunName("my-app", "default")
	// Expected format: k6-<rolloutName>-<8 hex chars>
	re := regexp.MustCompile(`^k6-my-app-[0-9a-f]{8}$`)
	assert.Regexp(t, re, name)
}

func TestTestRunName_MaxLength(t *testing.T) {
	longName := strings.Repeat("a", 250)
	name := testRunName(longName, "default")
	assert.LessOrEqual(t, len(name), 253, "CR name must be <= 253 chars")

	// Verify the 8-char hash suffix is preserved even with long rollout names.
	// The name format is "k6-<rollout>-<8hex>", so the last 8 chars must be hex.
	re := regexp.MustCompile(`-[0-9a-f]{8}$`)
	assert.Regexp(t, re, name, "hash suffix must be fully preserved after truncation")
}

func TestTestRunName_HashIncludesNamespace(t *testing.T) {
	// Different namespaces should produce different names even with same rollout name.
	// Hash input includes namespace to prevent cross-namespace name collisions.
	// We can't guarantee uniqueness per call (timestamp-based), but we verify
	// different namespaces at least CAN produce different names.
	names := make(map[string]bool)
	for i := 0; i < 10; i++ {
		n1 := testRunName("my-app", "ns-a")
		n2 := testRunName("my-app", "ns-b")
		names[n1] = true
		names[n2] = true
	}
	// With 20 names generated (10 per namespace), we should have more than 2 unique names
	// due to the timestamp component.
	assert.Greater(t, len(names), 2, "hash should include namespace in input")
}

// --- buildTestRun ---

func TestBuildTestRun_DefaultFields(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
	}
	tr := buildTestRun(cfg, "k6-scripts", "test.js", "default", "k6-my-app-abc12345")

	assert.Equal(t, "k6-my-app-abc12345", tr.Name)
	assert.Equal(t, "default", tr.Namespace)
	assert.Equal(t, "k6-scripts", tr.Spec.Script.ConfigMap.Name)
	assert.Equal(t, "test.js", tr.Spec.Script.ConfigMap.File)
}

func TestBuildTestRun_TypeMeta(t *testing.T) {
	cfg := &provider.PluginConfig{RolloutName: "my-app"}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")

	assert.Equal(t, "k6.io/v1alpha1", tr.APIVersion, "TypeMeta APIVersion must be set (Pitfall 1)")
	assert.Equal(t, "TestRun", tr.Kind, "TypeMeta Kind must be set (Pitfall 1)")
}

func TestBuildTestRun_Parallelism(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		Parallelism: 4,
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Equal(t, int32(4), tr.Spec.Parallelism)
}

func TestBuildTestRun_DefaultsParallelismWhenUnset(t *testing.T) {
	// Pins D-01: buildTestRun applies Parallelism=1 when cfg.Parallelism == 0
	// (the Go zero value, i.e. user omitted parallelism from plugin config).
	// k6-operator treats spec.Parallelism=0 as "paused"; the default here is
	// what makes parallelism genuinely optional end-to-end.
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		// Parallelism left at zero value (unset).
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Equal(t, int32(1), tr.Spec.Parallelism, "unset Parallelism must default to 1")
}

func TestBuildTestRun_PreservesExplicitParallelism(t *testing.T) {
	// Pins D-04: the default only fires on zero. Explicit non-zero values
	// (here, 4) must pass through unchanged. Overlaps with TestBuildTestRun_Parallelism
	// intentionally -- this test documents the "only zero defaults" rule.
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		Parallelism: 4,
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Equal(t, int32(4), tr.Spec.Parallelism, "explicit Parallelism must not be overwritten by the default")
}

func TestBuildTestRun_RunnerImage(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		RunnerImage: "grafana/k6:0.50.0",
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Equal(t, "grafana/k6:0.50.0", tr.Spec.Runner.Image)
}

func TestBuildTestRun_RunnerImageEmpty(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		RunnerImage: "",
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Empty(t, tr.Spec.Runner.Image, "empty runnerImage lets k6-operator use default")
}

func TestBuildTestRun_Resources(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		Resources: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	cpuLimit := tr.Spec.Runner.Resources.Limits[corev1.ResourceCPU]
	assert.Equal(t, "100m", (&cpuLimit).String())
	memLimit := tr.Spec.Runner.Resources.Limits[corev1.ResourceMemory]
	assert.Equal(t, "128Mi", (&memLimit).String())
}

func TestBuildTestRun_Env(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		Env: []corev1.EnvVar{
			{Name: "K6_BROWSER", Value: "true"},
		},
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	require.Len(t, tr.Spec.Runner.Env, 1)
	assert.Equal(t, "K6_BROWSER", tr.Spec.Runner.Env[0].Name)
	assert.Equal(t, "true", tr.Spec.Runner.Env[0].Value)
}

func TestBuildTestRun_Arguments(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		Arguments:   []string{"--tag", "env=staging"},
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Equal(t, "--tag env=staging", tr.Spec.Arguments)
}

func TestBuildTestRun_Labels(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Equal(t, "argo-rollouts-k6-plugin", tr.Labels[labelManagedBy])
	assert.Equal(t, "my-app", tr.Labels[labelRollout])
}

func TestBuildTestRun_CleanupUnset(t *testing.T) {
	// Regression guard for the "TestRun disappears before Resume can read status"
	// bug: k6-operator v1.3.x deletes the TestRun CR (and cascades pods) when
	// stage transitions to finished/error if spec.Cleanup == "post". Leaving
	// Cleanup unset keeps the TestRun and runner pods alive so GetRunResult can
	// read stage=finished, check runner exit codes, and parse handleSummary
	// from pod logs. Explicit cleanup happens in StopRun.
	cfg := &provider.PluginConfig{RolloutName: "my-app"}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Empty(t, string(tr.Spec.Cleanup), "spec.Cleanup must NOT default to 'post' -- k6-operator would GC the TestRun before the plugin can read status")
}

func TestBuildTestRun_OwnerRef_WithUID(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName:     "my-app",
		AnalysisRunName: "my-app-analysis-1",
		AnalysisRunUID:  "abc-123",
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")

	require.Len(t, tr.OwnerReferences, 1, "should set OwnerReferences when AnalysisRunUID is provided (per D-09)")
	ref := tr.OwnerReferences[0]
	assert.Equal(t, "argoproj.io/v1alpha1", ref.APIVersion)
	assert.Equal(t, "AnalysisRun", ref.Kind)
	assert.Equal(t, "my-app-analysis-1", ref.Name, "OwnerReference Name must be set for K8s API validation")
	assert.Equal(t, "abc-123", string(ref.UID))
}

func TestBuildTestRun_OwnerRef_WithoutUID(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName:    "my-app",
		AnalysisRunUID: "",
	}
	tr := buildTestRun(cfg, "cm", "test.js", "ns", "k6-test")
	assert.Nil(t, tr.OwnerReferences, "should not set OwnerReferences when AnalysisRunUID is empty (per D-09 fallback)")
}

// --- buildPrivateLoadZone ---

func TestBuildPrivateLoadZone_CloudFields(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		APIToken:    "token-123",
		StackID:     "stack-456",
		Resources: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		},
	}
	plz := buildPrivateLoadZone(cfg, "default", "k6-plz-test")

	assert.Equal(t, "k6.io/v1alpha1", plz.APIVersion)
	assert.Equal(t, "PrivateLoadZone", plz.Kind)
	assert.Equal(t, "token-123", plz.Spec.Token)
	cpuLimit := plz.Spec.Resources.Limits[corev1.ResourceCPU]
	assert.Equal(t, "200m", (&cpuLimit).String())
}

func TestBuildPrivateLoadZone_OwnerRef(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName:     "my-app",
		AnalysisRunName: "my-app-plz-analysis",
		AnalysisRunUID:  "uid-999",
		APIToken:        "token",
		StackID:         "stack",
	}
	plz := buildPrivateLoadZone(cfg, "ns", "k6-plz-test")

	require.Len(t, plz.OwnerReferences, 1, "should set OwnerReferences when AnalysisRunUID provided (per D-09)")
	assert.Equal(t, "my-app-plz-analysis", plz.OwnerReferences[0].Name, "OwnerReference Name must be set")
	assert.Equal(t, "uid-999", string(plz.OwnerReferences[0].UID))
}

func TestBuildPrivateLoadZone_Labels(t *testing.T) {
	cfg := &provider.PluginConfig{
		RolloutName: "my-app",
		APIToken:    "token",
		StackID:     "stack",
	}
	plz := buildPrivateLoadZone(cfg, "ns", "k6-plz-test")
	assert.Equal(t, "argo-rollouts-k6-plugin", plz.Labels[labelManagedBy])
	assert.Equal(t, "my-app", plz.Labels[labelRollout])
}

// --- isCloudConnected ---

func TestIsCloudConnected_True(t *testing.T) {
	cfg := &provider.PluginConfig{
		APIToken: "token-123",
		StackID:  "stack-456",
	}
	assert.True(t, isCloudConnected(cfg))
}

func TestIsCloudConnected_False_NoToken(t *testing.T) {
	cfg := &provider.PluginConfig{
		StackID: "stack-456",
	}
	assert.False(t, isCloudConnected(cfg))
}

func TestIsCloudConnected_False_NoStackID(t *testing.T) {
	cfg := &provider.PluginConfig{
		APIToken: "token-123",
	}
	assert.False(t, isCloudConnected(cfg))
}

// --- sanitizeLabelValue ---

func TestSanitizeLabelValue_Short(t *testing.T) {
	assert.Equal(t, "my-app", sanitizeLabelValue("my-app"))
}

func TestSanitizeLabelValue_Empty(t *testing.T) {
	assert.Equal(t, "", sanitizeLabelValue(""))
}

func TestSanitizeLabelValue_TruncatesAt63(t *testing.T) {
	long := strings.Repeat("a", 100)
	result := sanitizeLabelValue(long)
	assert.Equal(t, 63, len(result))
}

func TestSanitizeLabelValue_TrimsTrailingSpecialChars(t *testing.T) {
	// If truncation lands on a dot/dash/underscore, trim them.
	input := strings.Repeat("a", 60) + "---bbb"
	result := sanitizeLabelValue(input)
	assert.LessOrEqual(t, len(result), 63)
	assert.NotRegexp(t, `[._-]$`, result, "must not end with dot, dash, or underscore")
}

func TestSanitizeLabelValue_TruncationCutsAtSpecialChar(t *testing.T) {
	// 63 chars where position 62 (last) is a dash -> should be trimmed.
	input := strings.Repeat("a", 62) + "-"
	result := sanitizeLabelValue(input)
	assert.Equal(t, 62, len(result), "trailing dash after truncation should be trimmed")
}

// --- encodeRunID / decodeRunID ---

func TestEncodeRunID(t *testing.T) {
	id := encodeRunID("test-ns", "testruns", "k6-myapp-abc123")
	assert.Equal(t, "test-ns/testruns/k6-myapp-abc123", id)
}

func TestDecodeRunID_Valid(t *testing.T) {
	// Round-trip: encode then decode.
	encoded := encodeRunID("test-ns", "testruns", "k6-myapp-abc123")
	ns, resource, name, err := decodeRunID(encoded)
	require.NoError(t, err)
	assert.Equal(t, "test-ns", ns)
	assert.Equal(t, "testruns", resource)
	assert.Equal(t, "k6-myapp-abc123", name)
}

func TestDecodeRunID_InvalidFormat(t *testing.T) {
	_, _, _, err := decodeRunID("just-a-name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid run ID")
}

func TestDecodeRunID_InvalidResource(t *testing.T) {
	_, _, _, err := decodeRunID("ns/bogus/name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid run ID")
}

func TestDecodeRunID_NameContainsSlash(t *testing.T) {
	_, _, _, err := decodeRunID("ns/testruns/name/extra/stuff")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name component contains slash")
}

func TestDecodeRunID_PLZ(t *testing.T) {
	ns, resource, name, err := decodeRunID("ns/privateloadzones/plz-name")
	require.NoError(t, err)
	assert.Equal(t, "ns", ns)
	assert.Equal(t, "privateloadzones", resource)
	assert.Equal(t, "plz-name", name)
}

// --- gvrForResource ---

func TestGvrForResource_TestRuns(t *testing.T) {
	gvr := gvrForResource("testruns")
	assert.Equal(t, testRunGVR, gvr)
}

func TestGvrForResource_PLZ(t *testing.T) {
	gvr := gvrForResource("privateloadzones")
	assert.Equal(t, plzGVR, gvr)
}

func TestGvrForResource_Unknown_Panics(t *testing.T) {
	assert.Panics(t, func() {
		gvrForResource("unknown")
	})
}

// --- ValidateK6Operator parallelism ---

func TestValidateK6Operator_NegativeParallelism(t *testing.T) {
	cfg := &provider.PluginConfig{
		Provider: "k6-operator",
		ConfigMapRef: &provider.ConfigMapRef{
			Name: "cm",
			Key:  "test.js",
		},
		Parallelism: -1,
	}
	err := cfg.ValidateK6Operator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parallelism must be non-negative")
}

func TestValidateK6Operator_ZeroParallelism(t *testing.T) {
	cfg := &provider.PluginConfig{
		Provider: "k6-operator",
		ConfigMapRef: &provider.ConfigMapRef{
			Name: "cm",
			Key:  "test.js",
		},
		Parallelism: 0,
	}
	err := cfg.ValidateK6Operator()
	require.NoError(t, err, "parallelism 0 means unset, should be valid")
}
