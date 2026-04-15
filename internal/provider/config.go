package provider

import "fmt"

// ConfigMapRef references a key in a Kubernetes ConfigMap.
type ConfigMapRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// PluginConfig holds configuration parsed from the AnalysisTemplate
// plugin config JSON. Passed to every provider method call (stateless pattern per D-06).
type PluginConfig struct {
	TestRunID   string `json:"testRunId,omitempty"`
	TestID      string `json:"testId"`
	APIToken    string `json:"apiToken"`
	StackID     string `json:"stackId"`
	Timeout     string `json:"timeout,omitempty"`
	Metric      string `json:"metric"`                // thresholds|http_req_failed|http_req_duration|http_reqs
	Aggregation string `json:"aggregation,omitempty"` // p50|p95|p99 (for http_req_duration only)

	// Provider routing (per D-04)
	Provider string `json:"provider,omitempty"` // "grafana-cloud" (default) or "k6-operator"

	// k6-operator fields (per D-04, D-09)
	ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"`
	Namespace    string        `json:"namespace,omitempty"`
}

// IsGrafanaCloud returns true when the config targets the Grafana Cloud backend.
// Empty provider defaults to grafana-cloud for backward compatibility (per D-02).
func (c *PluginConfig) IsGrafanaCloud() bool {
	return c.Provider == "" || c.Provider == "grafana-cloud"
}

// ValidateK6Operator checks k6-operator-specific config fields.
// This is the single source of truth for k6-operator field validation.
// Called by parseConfig in both metric.go and step.go (centralized to prevent
// drift -- addresses pass 2 review concern about duplicated validation).
//
// Scope: syntactic validation only. Checks struct field presence and non-empty
// values. Does NOT hit the Kubernetes API (no ConfigMap existence check).
// ConfigMap existence is verified later by K6OperatorProvider.readScript().
func (c *PluginConfig) ValidateK6Operator() error {
	if c.ConfigMapRef == nil {
		return fmt.Errorf("configMapRef is required for k6-operator provider")
	}
	if c.ConfigMapRef.Name == "" {
		return fmt.Errorf("configMapRef.name is required")
	}
	if c.ConfigMapRef.Key == "" {
		return fmt.Errorf("configMapRef.key is required")
	}
	return nil
}
