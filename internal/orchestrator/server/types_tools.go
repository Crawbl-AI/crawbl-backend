package server

// toolResponse represents a single tool in the API response.
type toolResponse struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	Enabled       bool   `json:"enabled"`
	Toggleable    bool   `json:"toggleable"`
	RequiresSetup bool   `json:"requires_setup,omitempty"`
}

// toolsListResponse is the response for GET /v1/workspaces/{workspaceId}/tools.
type toolsListResponse struct {
	// DefaultTools are always-on tools that cannot be toggled off.
	DefaultTools []toolResponse `json:"default_tools"`
	// OptionalTools can be enabled/disabled by the user.
	// Currently empty — ready for future integrations (Gmail, Slack, etc.).
	OptionalTools []toolResponse `json:"optional_tools"`
}
