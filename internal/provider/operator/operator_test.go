package operator

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultOperatorConfig() *provider.PluginConfig {
	return &provider.PluginConfig{
		Provider: "k6-operator",
		ConfigMapRef: &provider.ConfigMapRef{
			Name: "k6-scripts",
			Key:  "load-test.js",
		},
		Namespace: "test-ns",
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

// --- Name test ---

func TestName(t *testing.T) {
	p := NewK6OperatorProvider()
	assert.Equal(t, "k6-operator", p.Name())
}

// --- ensureClient tests ---

func TestEnsureClient_WithInjectedClient(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient))

	client, err := p.ensureClient()
	require.NoError(t, err)
	assert.Equal(t, fakeClient, client)
}

func TestEnsureClient_FailureCachedPermanently(t *testing.T) {
	// Provider without injected client, not running in-cluster.
	// Verifies sync.Once caches the error permanently (intentional design per D-06).
	// Process restart is required to retry -- this is documented behavior.
	p := NewK6OperatorProvider()
	_, err1 := p.ensureClient()
	require.Error(t, err1)
	assert.Contains(t, err1.Error(), "in-cluster config")

	// Second call returns the same cached error, not a new attempt.
	_, err2 := p.ensureClient()
	require.Error(t, err2)
	assert.Equal(t, err1, err2, "sync.Once must cache the error permanently")
}

// --- readScript tests ---

func TestReadScript_Success(t *testing.T) {
	scriptContent := `import http from 'k6/http';
export default function() { http.get('https://test.k6.io'); }`

	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": scriptContent,
	})
	fakeClient := fake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient))

	cfg := defaultOperatorConfig()
	script, err := p.readScript(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, scriptContent, script)
}

func TestReadScript_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset() // empty -- no ConfigMaps
	p := NewK6OperatorProvider(WithClient(fakeClient))

	cfg := defaultOperatorConfig()
	_, err := p.readScript(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get configmap")
}

func TestReadScript_MissingKey(t *testing.T) {
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"other-key.js": "some script",
	})
	fakeClient := fake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient))

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
	fakeClient := fake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient))

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
	fakeClient := fake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient))

	cfg := defaultOperatorConfig()
	cfg.Namespace = "" // trigger fallback to "default"

	script, err := p.readScript(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "export default function() {}", script)
}

// --- TriggerRun tests ---

func TestTriggerRun_MissingConfigMapRef(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	p := NewK6OperatorProvider(WithClient(fakeClient))

	cfg := &provider.PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: nil, // missing
	}
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef is required")
}

func TestTriggerRun_LoadsScriptAndReturnsStubError(t *testing.T) {
	// TriggerRun should: validate config, load script from ConfigMap, then return
	// "not yet implemented" error (Phase 8 stub behavior).
	cm := testConfigMap("test-ns", "k6-scripts", map[string]string{
		"load-test.js": "export default function() {}",
	})
	fakeClient := fake.NewSimpleClientset(cm)
	p := NewK6OperatorProvider(WithClient(fakeClient))

	cfg := defaultOperatorConfig()
	_, err := p.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
	assert.Contains(t, err.Error(), "Phase 8")
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
