package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// internalMock is a minimal Provider mock for router tests.
// We cannot import providertest (circular dependency: provider -> providertest -> provider),
// so we define a lightweight mock here.
type internalMock struct {
	name           string
	triggerCalled  bool
	getResultCalled bool
	stopCalled     bool
}

func (m *internalMock) TriggerRun(_ context.Context, _ *PluginConfig) (string, error) {
	m.triggerCalled = true
	return "mock-run-1", nil
}

func (m *internalMock) GetRunResult(_ context.Context, _ *PluginConfig, _ string) (*RunResult, error) {
	m.getResultCalled = true
	return &RunResult{State: Running}, nil
}

func (m *internalMock) StopRun(_ context.Context, _ *PluginConfig, _ string) error {
	m.stopCalled = true
	return nil
}

func (m *internalMock) Name() string { return m.name }

func setupRouter() (*Router, *internalMock, *internalMock) {
	cloudMock := &internalMock{name: "grafana-cloud"}
	operatorMock := &internalMock{name: "k6-operator"}
	r := NewRouter(
		WithProvider("grafana-cloud", cloudMock),
		WithProvider("k6-operator", operatorMock),
	)
	return r, cloudMock, operatorMock
}

// --- Router dispatch tests ---

func TestRouter_TriggerRun_DispatchToCloud(t *testing.T) {
	r, cloudMock, operatorMock := setupRouter()
	cfg := &PluginConfig{Provider: "grafana-cloud"}
	_, err := r.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.True(t, cloudMock.triggerCalled, "cloud mock should be called")
	assert.False(t, operatorMock.triggerCalled, "operator mock should NOT be called")
}

func TestRouter_TriggerRun_DispatchToOperator(t *testing.T) {
	r, cloudMock, operatorMock := setupRouter()
	cfg := &PluginConfig{Provider: "k6-operator"}
	_, err := r.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.True(t, operatorMock.triggerCalled, "operator mock should be called")
	assert.False(t, cloudMock.triggerCalled, "cloud mock should NOT be called")
}

func TestRouter_TriggerRun_DefaultProvider(t *testing.T) {
	r, cloudMock, operatorMock := setupRouter()
	cfg := &PluginConfig{Provider: ""} // empty defaults to grafana-cloud
	_, err := r.TriggerRun(context.Background(), cfg)
	require.NoError(t, err)
	assert.True(t, cloudMock.triggerCalled, "cloud mock should be called for empty provider (backward compat)")
	assert.False(t, operatorMock.triggerCalled, "operator mock should NOT be called")
}

func TestRouter_TriggerRun_UnknownProvider(t *testing.T) {
	r, _, _ := setupRouter()
	cfg := &PluginConfig{Provider: "nonexistent"}
	_, err := r.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown provider "nonexistent"`)
}

func TestRouter_GetRunResult_Dispatch(t *testing.T) {
	r, cloudMock, operatorMock := setupRouter()
	cfg := &PluginConfig{Provider: "grafana-cloud"}
	_, err := r.GetRunResult(context.Background(), cfg, "run-1")
	require.NoError(t, err)
	assert.True(t, cloudMock.getResultCalled, "cloud mock GetRunResult should be called")
	assert.False(t, operatorMock.getResultCalled, "operator mock should NOT be called")
}

func TestRouter_StopRun_Dispatch(t *testing.T) {
	r, cloudMock, operatorMock := setupRouter()
	cfg := &PluginConfig{Provider: "k6-operator"}
	err := r.StopRun(context.Background(), cfg, "run-1")
	require.NoError(t, err)
	assert.True(t, operatorMock.stopCalled, "operator mock StopRun should be called")
	assert.False(t, cloudMock.stopCalled, "cloud mock should NOT be called")
}

func TestRouter_Name(t *testing.T) {
	r := NewRouter()
	assert.Equal(t, "router", r.Name())
}

func TestRouter_ExactMatchOnly(t *testing.T) {
	r, _, _ := setupRouter()
	cfg := &PluginConfig{Provider: "Grafana-Cloud"} // wrong case
	_, err := r.TriggerRun(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

// --- PluginConfig tests ---

func TestPluginConfig_JSONBackwardCompat(t *testing.T) {
	// Configs without new fields (provider, configMapRef, namespace) must
	// deserialize identically to existing behavior.
	raw := `{"testId":"42","apiToken":"tok","stackId":"123","metric":"thresholds"}`
	var cfg PluginConfig
	err := json.Unmarshal([]byte(raw), &cfg)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Provider, "Provider should be empty when not in JSON")
	assert.Nil(t, cfg.ConfigMapRef, "ConfigMapRef should be nil when not in JSON")
	assert.Equal(t, "", cfg.Namespace, "Namespace should be empty when not in JSON")
	// Verify existing fields still work
	assert.Equal(t, "42", cfg.TestID)
	assert.Equal(t, "tok", cfg.APIToken)
	assert.Equal(t, "123", cfg.StackID)
	assert.Equal(t, "thresholds", cfg.Metric)
}

func TestPluginConfig_IsGrafanaCloud(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		{"empty provider defaults to grafana-cloud", "", true},
		{"explicit grafana-cloud", "grafana-cloud", true},
		{"k6-operator is not grafana-cloud", "k6-operator", false},
		{"unknown provider is not grafana-cloud", "other", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PluginConfig{Provider: tt.provider}
			assert.Equal(t, tt.want, cfg.IsGrafanaCloud())
		})
	}
}

// --- ValidateK6Operator tests ---

func TestPluginConfig_ValidateK6Operator_Valid(t *testing.T) {
	cfg := &PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: &ConfigMapRef{Name: "scripts", Key: "test.js"},
	}
	err := cfg.ValidateK6Operator()
	assert.NoError(t, err)
}

func TestPluginConfig_ValidateK6Operator_NilConfigMapRef(t *testing.T) {
	cfg := &PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: nil,
	}
	err := cfg.ValidateK6Operator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef is required")
}

func TestPluginConfig_ValidateK6Operator_EmptyName(t *testing.T) {
	cfg := &PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: &ConfigMapRef{Name: "", Key: "test.js"},
	}
	err := cfg.ValidateK6Operator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef.name is required")
}

func TestPluginConfig_ValidateK6Operator_EmptyKey(t *testing.T) {
	cfg := &PluginConfig{
		Provider:     "k6-operator",
		ConfigMapRef: &ConfigMapRef{Name: "scripts", Key: ""},
	}
	err := cfg.ValidateK6Operator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef.key is required")
}
