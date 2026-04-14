package cloud

import (
	"log/slog"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// k6 Cloud API result values.
const (
	resultPassed = "passed"
	resultFailed = "failed"
	resultError  = "error"
)

// mapToRunState converts v6 API status type and result to a provider.RunState.
// v6 API status values: created, queued, initializing, running, processing_metrics, completed, aborted.
// v6 API result values: passed, failed, error (or nil if not yet finished).
func mapToRunState(statusType string, result *string) provider.RunState {
	switch statusType {
	case "created", "queued", "initializing", "running", "processing_metrics":
		return provider.Running
	case "completed":
		if result == nil {
			return provider.Errored // should not happen, but defensive
		}
		switch *result {
		case resultPassed:
			return provider.Passed
		case resultFailed:
			return provider.Failed
		case resultError:
			return provider.Errored
		default:
			return provider.Errored
		}
	case "aborted":
		return provider.Aborted
	default:
		slog.Warn("unknown k6 status type, treating as error", "statusType", statusType)
		return provider.Errored
	}
}

// isThresholdPassed returns true if the result indicates all k6 thresholds passed.
func isThresholdPassed(result *string) bool {
	return result != nil && *result == resultPassed
}
