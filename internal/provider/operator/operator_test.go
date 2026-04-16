package operator

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCRName is a fixed CR name for tests that use pre-created unstructured objects.
const testCRName = "k6-my-app-abc12345"

func defaultOperatorConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		Provider: "k6-operator",
		ConfigMapRef: &provider.ConfigMapRef{
			Name: "k6-scripts",
			Key:  "load-test.js",
		},
		Namespace:   "test-ns",
		RolloutName: "my-app",
	}
}

func testConfigMap(ns, name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: data,
	}
}

// testDynScheme returns a runtime.Scheme configured with k6-operator GVR mappings
// so the dynamic fake client can track TestRun and PrivateLoadZone resources.
func testDynScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k6.io", Version: "v1alpha1", Kind: "TestRun"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k6.io", Version: "v1alpha1", Kind: "TestRunList"},
		&unstructured.UnstructuredList{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k6.io", Version: "v1alpha1", Kind: "PrivateLoadZone"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k6.io", Version: "v1alpha1", Kind: "PrivateLoadZoneList"},
		&unstructured.UnstructuredList{},
	)
	return scheme
}

// fakeUnstructuredTestRun creates an unstructured TestRun for the dynamic fake client.
// If stage is empty, no status.stage field is set (simulates freshly created CR).
func fakeUnstructuredTestRun(ns, name, stage string) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "k6.io/v1alpha1",
		"kind":       "TestRun",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": ns,
		},
	}
	if stage != "" {
		obj["status"] = map[string]interface{}{
			"stage": stage,
		}
	}
	return &unstructured.Unstructured{Object: obj}
}

// testRunnerPod and testRunningPod are defined in exitcode_test.go (same package).
// testRunnerPod(ns, name, exitCode) creates a pod with terminated container.
// testRunningPod(ns, name) creates a pod with running (non-terminated) container.

// --- Name test ---

func TestName(t *testing.T) {
	p := NewK6OperatorProvider()
	assert.Equal(t, "k6-operator", p.Name())
}

// --- ensureClient tests ---

func TestEnsureClient_WithInjectedClient(t *testing.T) {
	fakeClient := k8sfake.NewSimpleClientset()
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	client, dynClient, err := p.ensureClient()
	require.NoError(t, err)
	assert.Equal(t, fakeClient, client)
	assert.Equal(t, fakeDyn, dynClient)
}

func TestEnsureClient_FailureCachedPermanently(t *testing.T) {
	// Provider without injected client, not running in-cluster.
	// Verifies sync.Once caches the error permanently (intentional design per D-06).
	// Process restart is required to retry -- this is documented behavior.
	p := NewK6OperatorProvider()
	_, _, err1 := p.ensureClient()
	require.Error(t, err1)
	assert.Contains(t, err1.Error(), "in-cluster config")

	// Second call returns the same cached error, not a new attempt.
	_, _, err2 := p.ensureClient()
	require.Error(t, err2)
	assert.Equal(t, err1, err2, "sync.Once must cache the error permanently")
}

func TestEnsureClient_WithDynClient(t *testing.T) {
	fakeClient := k8sfake.NewSimpleClientset()
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	client, dynClient, err := p.ensureClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.NotNil(t, dynClient)
	assert.Equal(t, fakeClient, client)
	assert.Equal(t, fakeDyn, dynClient)
}

// --- readScript tests ---

func TestReadScript_Success(t *testing.T) {
	scriptContent := `import http from 'k6/http';
export default function() { http.get('https://test.k6.io'); }`

	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": scriptContent,
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fake.NewSimpleDynamicClient(testDynScheme())))

	cfg := defaultOperatorConfig()
	script, err := p.readScript(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, scriptContent, script)
}

func TestReadScript_NotFound(t *testing.T) {
	fakeClient := k8sfake.NewSimpleClientset() // empty -- no ConfigMaps
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fake.NewSimpleDynamicClient(testDynScheme())))

	cfg := defaultOperatorConfig()
	_, err := p.readScript(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get configmap")
}

func TestReadScript_MissingKey(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"other-key.js": "some script",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fake.NewSimpleDynamicClient(testDynScheme())))

	cfg := defaultOperatorConfig()
	_, err := p.readScript(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key")
	assert.Contains(t, err.Error(), "not found")
}

func TestReadScript_EmptyContent(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fake.NewSimpleDynamicClient(testDynScheme())))

	cfg := defaultOperatorConfig()
	_, err := p.readScript(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestReadScript_DefaultNamespace(t *testing.T) {
	// When cfg.Namespace is empty, readScript should query "default" namespace.
	cm := testConfigMap("default", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fake.NewSimpleDynamicClient(testDynScheme())))

	cfg := defaultOperatorConfig()
	cfg.Namespace = "" // trigger fallback to "default"

	script, err := p.readScript(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "export default function() {}", script)
}

// --- TriggerRun tests ---

func TestTriggerRun_MissingConfigMapRef(t *testing.T) {
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fake.NewSimpleDynamicClient(testDynScheme())))

	cfg := &provider.PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: nil, // missing
	}
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef is required")
}

func TestTriggerRun_CreatesTestRun(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID, err := p.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, runID)

	// Decode runID and verify the TestRun CR was created
	ns, resource, name, decErr := decodeRunID(runID)
	require.NoError(t, decErr)
	assert.Equal(t, "test-ns", ns)
	assert.Equal(t, "testruns", resource)

	created, getErr := fakeDyn.Resource(testRunGVR).Namespace(ns).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, getErr)
	assert.Equal(t, "TestRun", created.GetKind())
	assert.Equal(t, name, created.GetName())

	// Check labels
	labels := created.GetLabels()
	assert.Equal(t, "argo-rollouts-k6-plugin", labels[labelManagedBy])
	assert.Equal(t, "my-app", labels[labelRollout])
}

func TestTriggerRun_CreatesPrivateLoadZone(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	cfg.APIToken = "test-token"
	cfg.StackID = "123456"

	runID, err := p.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, runID)

	// Decode runID and verify the PrivateLoadZone CR was created
	ns, resource, name, decErr := decodeRunID(runID)
	require.NoError(t, decErr)
	assert.Equal(t, "test-ns", ns)
	assert.Equal(t, "privateloadzones", resource)

	created, getErr := fakeDyn.Resource(plzGVR).Namespace(ns).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, getErr)
	assert.Equal(t, "PrivateLoadZone", created.GetKind())
}

func TestTriggerRun_WithAnalysisRunUID(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	cfg.AnalysisRunName = "my-app-analysis-run-1"
	cfg.AnalysisRunUID = "uid-abc-123"

	runID, err := p.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)

	// Decode runID, retrieve created CR from fake dynamic client
	ns, _, name, decErr := decodeRunID(runID)
	require.NoError(t, decErr)

	created, getErr := fakeDyn.Resource(testRunGVR).Namespace(ns).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, getErr)

	// Check OwnerReferences
	ownerRefs := created.GetOwnerReferences()
	require.Len(t, ownerRefs, 1)
	assert.Equal(t, "argoproj.io/v1alpha1", ownerRefs[0].APIVersion)
	assert.Equal(t, "AnalysisRun", ownerRefs[0].Kind)
	assert.Equal(t, "my-app-analysis-run-1", ownerRefs[0].Name, "OwnerReference Name must be populated")
	assert.Equal(t, "uid-abc-123", string(ownerRefs[0].UID))
}

func TestTriggerRun_WithoutAnalysisRunUID(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := k8sfake.NewSimpleClientset(cm)
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	cfg.AnalysisRunUID = "" // empty -- no owner ref

	runID, err := p.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)

	// Decode runID, retrieve created CR from fake dynamic client
	ns, _, name, decErr := decodeRunID(runID)
	require.NoError(t, decErr)

	created, getErr := fakeDyn.Resource(testRunGVR).Namespace(ns).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, getErr)

	// OwnerReferences should be empty/nil
	ownerRefs := created.GetOwnerReferences()
	assert.Empty(t, ownerRefs)
}

func TestTriggerRun_ValidationBeforeIO(t *testing.T) {
	// Config with missing configMapRef AND missing ConfigMap in fake clientset.
	// Validation should fail BEFORE readScript is called (no I/O wasted on invalid config).
	fakeClient := k8sfake.NewSimpleClientset() // empty -- no ConfigMaps
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := &provider.PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: nil, // missing -- triggers validation error
	}
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	// Should be a validation error, NOT a ConfigMap not found error
	assert.Contains(t, err.Error(), "configMapRef is required")
	assert.NotContains(t, err.Error(), "get configmap")
}

func TestTriggerRun_ConfigMapNotFound(t *testing.T) {
	// Valid config (configMapRef present) but no ConfigMap in fake clientset.
	fakeClient := k8sfake.NewSimpleClientset() // empty
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get configmap")
}

// --- GetRunResult tests ---

func TestGetRunResult_StageStarted(t *testing.T) {
	tr := fakeUnstructuredTestRun("test-ns", testCRName, "started")
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", testCRName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Running, result.State)
}

func TestGetRunResult_StageFinished_AllPassed(t *testing.T) {
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "finished")
	pod := testRunnerPod("test-ns", crName, 0) // exit code 0 = passed
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset(pod)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Passed, result.State)
	assert.True(t, result.ThresholdsPassed)
}

func TestGetRunResult_StageFinished_ThresholdsFailed(t *testing.T) {
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "finished")
	pod := testRunnerPod("test-ns", crName, 99) // exit code 99 = thresholds failed
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset(pod)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Failed, result.State)
	assert.False(t, result.ThresholdsPassed)
}

func TestGetRunResult_StageError(t *testing.T) {
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "error")
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Errored, result.State)
}

func TestGetRunResult_NotFound(t *testing.T) {
	// No TestRun in fake dyn client, but valid encoded runID.
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", "nonexistent-cr")

	_, err := p.GetRunResult(context.Background(), cfg, runID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get testruns")
}

func TestGetRunResult_AbsentStage(t *testing.T) {
	// TestRun with no status.stage field at all (freshly created).
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "") // empty = no stage field
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Running, result.State, "absent stage should map to Running")
}

func TestGetRunResult_PodsStillRunning(t *testing.T) {
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "finished")
	pod := testRunningPod("test-ns", crName) // pod not yet terminated
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset(pod)
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Running, result.State, "pods not terminated should return Running")
}

func TestGetRunResult_InvalidRunID(t *testing.T) {
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	_, err := p.GetRunResult(context.Background(), cfg, "just-a-name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid run ID")
}

// --- StopRun tests ---

func TestStopRun_DeletesTestRun(t *testing.T) {
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "started")
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	err := p.StopRun(context.Background(), cfg, runID)
	require.NoError(t, err)

	// Verify it was deleted
	_, getErr := fakeDyn.Resource(testRunGVR).Namespace("test-ns").Get(
		context.Background(), crName, metav1.GetOptions{},
	)
	assert.Error(t, getErr, "TestRun should no longer exist after StopRun")
}

func TestStopRun_DeletesPrivateLoadZone(t *testing.T) {
	crName := testCRName
	plz := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k6.io/v1alpha1",
			"kind":       "PrivateLoadZone",
			"metadata": map[string]interface{}{
				"name":      crName,
				"namespace": "test-ns",
			},
		},
	}
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), plz)
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "privateloadzones", crName)

	err := p.StopRun(context.Background(), cfg, runID)
	require.NoError(t, err)
}

func TestStopRun_NotFound_ReturnsSuccess(t *testing.T) {
	// Empty fake dyn client (no CR exists).
	// StopRun should treat NotFound as success (idempotent delete for abort paths).
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", "nonexistent-cr")

	err := p.StopRun(context.Background(), cfg, runID)
	assert.NoError(t, err, "NotFound should be treated as success (idempotent delete)")
}

func TestStopRun_InvalidRunID(t *testing.T) {
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme())
	fakeClient := k8sfake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))

	cfg := defaultOperatorConfig()
	err := p.StopRun(context.Background(), cfg, "malformed-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid run ID")
}

// --- Validate tests ---

func TestValidate_DelegatesToValidateK6Operator(t *testing.T) {
	p := NewK6OperatorProvider()

	// Valid config -- should pass validation.
	cfg := defaultOperatorConfig()
	err := p.Validate(cfg)
	require.NoError(t, err)

	// Invalid config (nil ConfigMapRef) -- should fail same as ValidateK6Operator.
	cfgBad := &provider.PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: nil,
	}
	err = p.Validate(cfgBad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef is required")
}

func TestValidate_SyntacticOnly(t *testing.T) {
	// Validate must be syntactic-only -- no K8s API calls.
	// This provider has NO client injected. If Validate tried to call
	// ensureClient or readScript, it would fail with "in-cluster config" error.
	// Validate must succeed without any client interaction.
	p := NewK6OperatorProvider() // no WithClient -- no k8s client available
	cfg := defaultOperatorConfig()
	err := p.Validate(cfg)
	require.NoError(t, err, "Validate must not require K8s client (syntactic-only)")
}

// --- WithLogReader tests ---

func TestWithLogReader(t *testing.T) {
	mock := &mockPodLogReader{logs: map[string]string{}}
	p := NewK6OperatorProvider(WithLogReader(mock))
	assert.NotNil(t, p.logReader, "WithLogReader must set logReader field")
}

// --- GetRunResult metric integration tests ---

func TestGetRunResult_WithSummaryMetrics(t *testing.T) {
	// Finished TestRun, exit code 0, valid handleSummary JSON in pod logs.
	// Expects: Passed state with populated metrics from handleSummary.
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "finished")
	pod := testRunnerPod("test-ns", crName, 0) // exit code 0 = passed
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset(pod)

	// Build log content: mixed k6 output with valid handleSummary JSON at end.
	logContent := `     execution: local
        script: load-test.js
     scenarios: (100.00%) 1 scenario

running (1m30s), 0/10 VUs, 1000 complete iterations
default   [==============================] 10 VUs  1m30s
` + validSummaryJSON + "\n"

	mockReader := &mockPodLogReader{
		logs: map[string]string{
			"test-ns/" + pod.Name: logContent,
		},
	}

	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn), WithLogReader(mockReader))
	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Passed, result.State)
	assert.True(t, result.ThresholdsPassed)
	assert.InDelta(t, 0.023, result.HTTPReqFailed, 0.001)
	assert.InDelta(t, 210.3, result.HTTPReqDuration.P50, 0.1)
	assert.InDelta(t, 523.4, result.HTTPReqDuration.P95, 0.1)
	assert.InDelta(t, 780.2, result.HTTPReqDuration.P99, 0.1)
	assert.InDelta(t, 33.33, result.HTTPReqs, 0.01)
}

func TestGetRunResult_ThresholdsFailedWithMetrics(t *testing.T) {
	// Finished TestRun, exit code 99, valid handleSummary JSON.
	// Expects: Failed state with populated metrics (handleSummary metrics independent of threshold pass/fail).
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "finished")
	pod := testRunnerPod("test-ns", crName, 99) // exit code 99 = thresholds failed
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset(pod)

	logContent := `     execution: local
        script: load-test.js

running (1m30s), 0/10 VUs, 1000 complete iterations
default   [==============================] 10 VUs  1m30s
` + validSummaryJSON + "\n"

	mockReader := &mockPodLogReader{
		logs: map[string]string{
			"test-ns/" + pod.Name: logContent,
		},
	}

	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn), WithLogReader(mockReader))
	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Failed, result.State)
	assert.False(t, result.ThresholdsPassed)
	// Metrics still populated even when thresholds failed.
	assert.InDelta(t, 0.023, result.HTTPReqFailed, 0.001)
	assert.InDelta(t, 210.3, result.HTTPReqDuration.P50, 0.1)
	assert.InDelta(t, 523.4, result.HTTPReqDuration.P95, 0.1)
	assert.InDelta(t, 780.2, result.HTTPReqDuration.P99, 0.1)
	assert.InDelta(t, 33.33, result.HTTPReqs, 0.01)
}

func TestGetRunResult_NoSummaryGracefulDegradation(t *testing.T) {
	// Finished TestRun, exit code 0, but pod logs have no handleSummary JSON.
	// Expects: Passed state with zero metrics (graceful degradation per D-05).
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "finished")
	pod := testRunnerPod("test-ns", crName, 0)
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset(pod)

	logContent := `     execution: local
        script: load-test.js

running (1m30s), 0/10 VUs, 1000 complete iterations
default   [==============================] 10 VUs  1m30s

     data_received..................: 1.2 MB 13 kB/s
     http_req_duration..............: avg=234ms min=100ms med=210ms max=890ms p(90)=450ms p(95)=523ms
`

	mockReader := &mockPodLogReader{
		logs: map[string]string{
			"test-ns/" + pod.Name: logContent,
		},
	}

	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn), WithLogReader(mockReader))
	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Passed, result.State)
	assert.True(t, result.ThresholdsPassed)
	assert.Equal(t, float64(0), result.HTTPReqFailed)
	assert.Equal(t, float64(0), result.HTTPReqDuration.P95)
	assert.Equal(t, float64(0), result.HTTPReqs)
}

func TestGetRunResult_NonTerminalSkipsMetrics(t *testing.T) {
	// Started TestRun (non-terminal stage). Should NOT call parseSummaryFromPods.
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "started")
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset()
	// No mockReader needed -- parseSummaryFromPods should not be called for non-terminal states.

	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))
	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Running, result.State)
}

func TestGetRunResult_ErrorStateSkipsMetrics(t *testing.T) {
	// TestRun with stage="error". Should NOT call parseSummaryFromPods.
	crName := testCRName
	tr := fakeUnstructuredTestRun("test-ns", crName, "error")
	fakeDyn := fake.NewSimpleDynamicClient(testDynScheme(), tr)
	fakeClient := k8sfake.NewSimpleClientset()

	p := NewK6OperatorProvider(WithClient(fakeClient), WithDynClient(fakeDyn))
	cfg := defaultOperatorConfig()
	runID := encodeRunID("test-ns", "testruns", crName)

	result, err := p.GetRunResult(context.Background(), cfg, runID)
	require.NoError(t, err)
	assert.Equal(t, provider.Errored, result.State)
}
