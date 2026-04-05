package orchestrator

import "strings"

// StatusForRuntime maps a runtime status to an agent status.
func StatusForRuntime(runtime *RuntimeStatus) AgentStatus {
	if runtime == nil {
		return AgentStatusOffline
	}
	if runtime.Verified {
		return AgentStatusOnline
	}
	switch strings.ToLower(strings.TrimSpace(runtime.Phase)) {
	case string(RuntimePhaseProgressing), string(RuntimePhasePending):
		return AgentStatusPending
	case string(RuntimePhaseFailed), string(RuntimePhaseError):
		return AgentStatusError
	default:
		return AgentStatusOffline
	}
}

// EnrichAgentStatus sets each agent's status based on the workspace runtime state.
// If runtime is nil, all agents are set to offline.
func EnrichAgentStatus(agents []*Agent, runtime *RuntimeStatus) {
	for _, agent := range agents {
		agent.Status = StatusForRuntime(runtime)
	}
}
