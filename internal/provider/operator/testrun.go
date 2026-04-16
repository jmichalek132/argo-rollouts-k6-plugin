package operator

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	k6v1alpha1 "github.com/grafana/k6-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// GVR constants for k6-operator CRDs.
var testRunGVR = schema.GroupVersionResource{
	Group:    "k6.io",
	Version:  "v1alpha1",
	Resource: "testruns",
}

var plzGVR = schema.GroupVersionResource{
	Group:    "k6.io",
	Version:  "v1alpha1",
	Resource: "privateloadzones",
}

// Label constants for CR identification (per D-10).
const (
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelRollout   = "k6-plugin/rollout"
	managedByValue = "argo-rollouts-k6-plugin"
)

// sanitizeLabelValue truncates and trims a string for use as a Kubernetes label value.
// Label values must be at most 63 characters and, if non-empty, must start and end
// with an alphanumeric character (RFC 1123 / Kubernetes validation rules).
func sanitizeLabelValue(v string) string {
	if len(v) > 63 {
		v = v[:63]
	}
	// Trim trailing non-alphanumeric characters that may result from truncation.
	v = strings.TrimRight(v, ".-_")
	return v
}

// testRunName generates a deterministic CR name following the pattern k6-<rollout>-<hash>.
// The hash input includes namespace to prevent cross-namespace name collisions
// when rollout names are the same. The timestamp component ensures uniqueness
// across multiple runs of the same rollout.
func testRunName(rolloutName, namespace string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", namespace, rolloutName, time.Now().UnixNano())))
	short := fmt.Sprintf("%x", h[:4]) // 8 hex chars

	// Reserve space for "k6-" prefix (3) + "-" separator (1) + hash (8) = 12 chars.
	// Truncate the rollout name component to ensure the hash suffix is always
	// fully present, preserving uniqueness guarantees.
	const maxRolloutLen = 253 - 12
	if len(rolloutName) > maxRolloutLen {
		rolloutName = rolloutName[:maxRolloutLen]
	}

	return fmt.Sprintf("k6-%s-%s", rolloutName, short)
}

// analysisRunOwnerRef creates an OwnerReference for the AnalysisRun (per D-09).
// If analysisRunUID is empty, returns nil (fallback to label-based identification).
// Both Name and UID are required: the Kubernetes API server validates that Name is
// non-empty in OwnerReferences, and UID is used for the actual GC lookup.
func analysisRunOwnerRef(analysisRunName, analysisRunUID string) []metav1.OwnerReference {
	if analysisRunUID == "" {
		return nil
	}
	return []metav1.OwnerReference{{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "AnalysisRun",
		Name:       analysisRunName,
		UID:        types.UID(analysisRunUID),
	}}
}

// encodeRunID encodes the full lifecycle identity of a CR so that GetRunResult
// and StopRun never need to re-derive GVR from config.
// Format: namespace/resource/name (e.g., "default/testruns/k6-myapp-abc12345").
func encodeRunID(namespace, resource, name string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, resource, name)
}

// decodeRunID decodes a run ID into its constituent parts.
// Returns an error if the format is invalid or the resource is not recognized.
func decodeRunID(runID string) (namespace, resource, name string, err error) {
	parts := strings.SplitN(runID, "/", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid run ID %q: expected namespace/resource/name", runID)
	}
	namespace, resource, name = parts[0], parts[1], parts[2]
	if resource != "testruns" && resource != "privateloadzones" {
		return "", "", "", fmt.Errorf("invalid run ID %q: expected namespace/resource/name", runID)
	}
	return namespace, resource, name, nil
}

// gvrForResource maps a resource string back to its GVR constant.
// Panics on unknown resource (indicates a bug -- the resource string came from our own encodeRunID).
func gvrForResource(resource string) schema.GroupVersionResource {
	switch resource {
	case "testruns":
		return testRunGVR
	case "privateloadzones":
		return plzGVR
	default:
		panic(fmt.Sprintf("unknown resource %q", resource))
	}
}

// buildTestRun constructs a TestRun CR from plugin config (per D-01).
// Pure struct construction -- no I/O.
func buildTestRun(cfg *provider.PluginConfig, scriptCMName, scriptKey, namespace, crName string) *k6v1alpha1.TestRun {
	tr := &k6v1alpha1.TestRun{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k6.io/v1alpha1",
			Kind:       "TestRun",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelRollout:   sanitizeLabelValue(cfg.RolloutName),
			},
			OwnerReferences: analysisRunOwnerRef(cfg.AnalysisRunName, cfg.AnalysisRunUID),
		},
		Spec: k6v1alpha1.TestRunSpec{
			Script: k6v1alpha1.K6Script{
				ConfigMap: k6v1alpha1.K6Configmap{
					Name: scriptCMName,
					File: scriptKey,
				},
			},
			Parallelism: int32(cfg.Parallelism),
			Runner: k6v1alpha1.Pod{
				Image: cfg.RunnerImage,
				Env:   cfg.Env,
			},
			Arguments: strings.Join(cfg.Arguments, " "),
			Cleanup:   "post",
		},
	}

	// Set runner resources if provided.
	if cfg.Resources != nil {
		tr.Spec.Runner.Resources = *cfg.Resources
	}

	return tr
}

// buildPrivateLoadZone constructs a PrivateLoadZone CR for Grafana Cloud-connected
// in-cluster execution (per D-02). Uses cloud credentials from config.
func buildPrivateLoadZone(cfg *provider.PluginConfig, namespace, crName string) *k6v1alpha1.PrivateLoadZone {
	plz := &k6v1alpha1.PrivateLoadZone{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k6.io/v1alpha1",
			Kind:       "PrivateLoadZone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelRollout:   sanitizeLabelValue(cfg.RolloutName),
			},
			OwnerReferences: analysisRunOwnerRef(cfg.AnalysisRunName, cfg.AnalysisRunUID),
		},
		Spec: k6v1alpha1.PrivateLoadZoneSpec{
			Token: cfg.APIToken,
			Image: cfg.RunnerImage,
		},
	}

	// Set resources if provided.
	if cfg.Resources != nil {
		plz.Spec.Resources = *cfg.Resources
	}

	return plz
}

// isCloudConnected returns true if Grafana Cloud fields are present alongside
// the k6-operator provider setting (per D-02). When true, use PrivateLoadZone
// instead of TestRun.
func isCloudConnected(cfg *provider.PluginConfig) bool {
	return cfg.APIToken != "" && cfg.StackID != ""
}
