package step

import (
	stepRpc "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider"
)

// Compile-time interface check.
var _ stepRpc.StepPlugin = (*K6StepPlugin)(nil)

// stepState holds the persisted state across Run() calls via RpcStepContext.Status.
type stepState struct {
	RunID       string `json:"runId"`
	TriggeredAt string `json:"triggeredAt"`
	TestRunURL  string `json:"testRunURL"`
	FinalStatus string `json:"finalStatus,omitempty"`
}

// K6StepPlugin implements the RpcStep interface for k6 step plugin.
type K6StepPlugin struct {
	provider provider.Provider
}

// New creates a new K6StepPlugin with the given provider backend.
func New(p provider.Provider) *K6StepPlugin {
	return &K6StepPlugin{provider: p}
}

// InitPlugin is called once when the plugin is loaded.
func (k *K6StepPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}

// Run executes the step plugin. Stub -- returns zero values.
func (k *K6StepPlugin) Run(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	return types.RpcStepResult{}, types.RpcError{}
}

// Terminate stops an uncompleted operation. Stub -- returns zero values.
func (k *K6StepPlugin) Terminate(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	return types.RpcStepResult{}, types.RpcError{}
}

// Abort reverts actions performed during Run. Stub -- returns zero values.
func (k *K6StepPlugin) Abort(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	return types.RpcStepResult{}, types.RpcError{}
}

// Type returns the plugin name.
func (k *K6StepPlugin) Type() string {
	return "jmichalek132/k6-step"
}
