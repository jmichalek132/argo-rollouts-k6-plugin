package provider

// PluginConfig holds configuration parsed from the AnalysisTemplate
// plugin config JSON. Passed to every provider method call (stateless pattern per D-06).
type PluginConfig struct {
	TestRunID string `json:"testRunId,omitempty"`
	TestID    string `json:"testId"`
	APIToken  string `json:"apiToken"`
	StackID   string `json:"stackId"`
	Timeout   string `json:"timeout,omitempty"`
}
