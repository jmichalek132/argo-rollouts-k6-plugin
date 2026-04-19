package provider

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

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

	// Phase 8 k6-operator fields (per D-06)

	// Parallelism is the number of k6 runner pods. Defaults to 1 when unset or zero
	// (applied by buildTestRun per D-01 / Phase 08.2). k6-operator treats
	// spec.Parallelism=0 as "paused", so forwarding 0 would leave the TestRun
	// permanently paused; the plugin resolves 0 to 1 at the TestRun construction site.
	// ValidateK6Operator accepts Parallelism==0 at the config boundary ("means unset")
	// and rejects negative values.
	Parallelism int                          `json:"parallelism,omitempty"`
	Resources   *corev1.ResourceRequirements `json:"resources,omitempty"`
	RunnerImage string                       `json:"runnerImage,omitempty"`
	Env         []corev1.EnvVar              `json:"env,omitempty"`
	Arguments   []string                     `json:"arguments,omitempty"`

	// Injected by step/metric plugin layer (not from user JSON config).
	// Used for CR naming (per D-10), labels, and owner references.
	RolloutName     string `json:"-"` // from Rollout ObjectMeta.Name
	AnalysisRunName string `json:"-"` // from AnalysisRun ObjectMeta.Name (for OwnerReference)
	AnalysisRunUID  string `json:"-"` // from AnalysisRun ObjectMeta.UID (per D-09)
	RolloutUID      string `json:"-"` // from Rollout ObjectMeta.UID (for OwnerReference when no AnalysisRunUID, per D-07)
}

// IsGrafanaCloud returns true when the config targets the Grafana Cloud backend.
// Empty provider defaults to grafana-cloud for backward compatibility (per D-02).
func (c *PluginConfig) IsGrafanaCloud() bool {
	return c.Provider == "" || c.Provider == "grafana-cloud"
}

// ValidateGrafanaCloud checks Grafana Cloud-specific config fields.
// This is the single source of truth for Grafana Cloud field validation.
// Called by parseConfig in both metric.go and step.go (centralized to prevent
// drift -- parallel to ValidateK6Operator).
func (c *PluginConfig) ValidateGrafanaCloud() error {
	if c.TestID == "" && c.TestRunID == "" {
		return fmt.Errorf("either testId or testRunId is required")
	}
	if c.APIToken == "" {
		return fmt.Errorf("apiToken is required (check Secret reference)")
	}
	if c.StackID == "" {
		return fmt.Errorf("stackId is required (check Secret reference)")
	}
	return nil
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
	if c.Parallelism < 0 {
		return fmt.Errorf("parallelism must be non-negative, got %d", c.Parallelism)
	}
	return nil
}
