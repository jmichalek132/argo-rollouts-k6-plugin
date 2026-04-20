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

// parentOwnerRef returns an owner reference for the TestRun/PrivateLoadZone CR based
// on D-07 precedence (AR > Rollout > none). The most-immediate parent gets the GC
// relationship -- metric plugin naturally selects AR; step plugin naturally selects
// Rollout. When both UIDs are empty, returns nil (no owner reference) which the
// Kubernetes API server accepts -- CRs will still be discoverable via the
// app.kubernetes.io/managed-by and k6-plugin/rollout labels.
//
// Caller invariant: caller MUST set both the UID and the matching Name for the chosen
// parent; this helper does not validate non-empty names. The Kubernetes API server
// will reject OwnerReferences with empty Name, so an empty Name here will surface as
// a Create error, not a silent corruption.
func parentOwnerRef(cfg *provider.PluginConfig) []metav1.OwnerReference {
	if cfg.AnalysisRunUID != "" {
		return []metav1.OwnerReference{{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "AnalysisRun",
			Name:       cfg.AnalysisRunName,
			UID:        types.UID(cfg.AnalysisRunUID),
		}}
	}
	if cfg.RolloutUID != "" {
		return []metav1.OwnerReference{{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Rollout",
			Name:       cfg.RolloutName,
			UID:        types.UID(cfg.RolloutUID),
		}}
	}
	return nil
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
	// Kubernetes resource names cannot contain slashes. Reject malformed runIDs
	// like "ns/testruns/name/extra" which SplitN(3) would accept as name="name/extra".
	if strings.Contains(name, "/") {
		return "", "", "", fmt.Errorf("invalid run ID %q: name component contains slash", runID)
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
//
// Applies the Parallelism=1 default when cfg.Parallelism == 0 (Go zero value /
// "unset"). k6-operator treats spec.Parallelism=0 as "paused", so forwarding 0
// would leave the TestRun waiting forever and no runner pods would spawn.
// ValidateK6Operator continues to accept Parallelism==0 at the config boundary
// ("0 means unset"); the default is applied here at the consumer site per D-01.
//
// Does NOT set spec.Cleanup. k6-operator v1.3.x treats spec.Cleanup="post" as
// "delete the TestRun CR AND its runner pods once stage transitions to
// finished/error" (see k6-operator testrun_controller.go case "error","finished"
// which calls r.Delete(ctx, k6)). That caused the metric plugin's Resume to
// observe the TestRun as NotFound immediately after the run completed -- the
// plugin could not read the terminal stage or parse handleSummary from pod logs
// because both the CR and its pods were gone. Leaving Cleanup unset keeps the
// TestRun and runner pods alive after completion so the plugin can extract
// exit codes and handleSummary. Explicit deletion happens in StopRun (called
// by Terminate/Abort paths); success-path cleanup is deferred to a follow-up
// phase that implements GarbageCollect on the metric plugin (and a symmetric
// post-terminal delete on the step plugin).
func buildTestRun(cfg *provider.PluginConfig, scriptCMName, scriptKey, namespace, crName string) *k6v1alpha1.TestRun {
	// Default Parallelism=1 when unset (cfg.Parallelism == 0) per D-01 / Phase 08.2.
	// k6-operator treats spec.Parallelism=0 as "paused" -- forwarding 0 would
	// leave the TestRun permanently paused. Silent default (no log emission)
	// per D-02 keeps buildTestRun a pure builder.
	parallelism := int32(cfg.Parallelism)
	if cfg.Parallelism == 0 {
		parallelism = 1
	}

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
			OwnerReferences: parentOwnerRef(cfg),
		},
		Spec: k6v1alpha1.TestRunSpec{
			Script: k6v1alpha1.K6Script{
				ConfigMap: k6v1alpha1.K6Configmap{
					Name: scriptCMName,
					File: scriptKey,
				},
			},
			Parallelism: parallelism,
			Runner: k6v1alpha1.Pod{
				Image: cfg.RunnerImage,
				Env:   cfg.Env,
			},
			Arguments: strings.Join(cfg.Arguments, " "),
			// spec.Cleanup intentionally left unset -- see function doc.
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
			OwnerReferences: parentOwnerRef(cfg),
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
