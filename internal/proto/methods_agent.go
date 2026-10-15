package proto

// Control methods of the agents (M3).
const (
	MethodAgentList    = "agent.list"
	MethodAgentReload  = "agent.reload"
	MethodAgentExplain = "agent.explain"
	MethodHookEvent    = "hook.event"
)

// AgentInfo describes one known agent for agent.list.
type AgentInfo struct {
	Name       string   `json:"name"`
	Display    string   `json:"display,omitempty"`
	Source     string   `json:"source"` // builtin:… or a file path
	Command    string   `json:"command"`
	Injector   string   `json:"injector"`
	Tested     []string `json:"tested_versions,omitempty"`
	LastTested string   `json:"last_tested,omitempty"` // information only
	Unenforced bool     `json:"unenforced,omitempty"`  // renders no access policy
}

// AgentListResult is the result of agent.list and agent.reload.
type AgentListResult struct {
	Agents []AgentInfo `json:"agents"`
	// Errors lists broken user manifests, which were skipped.
	Errors []string `json:"errors,omitempty"`
}

// HookEventParams are what `tm hook` sends: one event of one session.
type HookEventParams struct {
	Session string         `json:"session"`
	Agent   string         `json:"agent"`
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
	// Token is the agent's prompt-channel token from the hook's
	// environment (the manifest's inject.token_env), when it has one.
	Token string `json:"token,omitempty"`
}

// HookEventResult is what `tm hook` prints back to the harness.
type HookEventResult struct {
	Stdout string `json:"stdout,omitempty"`
}
