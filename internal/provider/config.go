package provider

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
}
