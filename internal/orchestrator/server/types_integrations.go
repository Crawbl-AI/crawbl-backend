package server

// integrationsResponse is the unified response for GET /v1/integrations.
// Contains both agent tools and third-party app integrations in one payload
// so the mobile app can render the full profile capabilities screen.
type integrationsResponse struct {
	// Tools are the agent's built-in capabilities (search, memory, scheduling, etc.).
	// Shown in the "Tools" tab of the profile capabilities screen.
	Tools []integrationItemResponse `json:"tools"`

	// Integrations are third-party app connections (Gmail, Slack, etc.).
	// Shown in the "Connected Apps" tab of the profile capabilities screen.
	Integrations []integrationItemResponse `json:"integrations"`
}

// integrationItemResponse represents a single item in the tools or integrations list.
// Field names match the mobile app's IntegrationItemData model.
type integrationItemResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	Provider    string `json:"provider,omitempty"`
	Category    string `json:"category,omitempty"`
	IsConnected bool   `json:"is_connected"`
	IsEnabled   bool   `json:"is_enabled"`
}

// integrationConnectRequest is the body for POST /v1/integrations/connect.
type integrationConnectRequest struct {
	Provider string `json:"provider"`
}

// integrationConnectResponse returns OAuth config for the mobile app to start the flow.
type integrationConnectResponse struct {
	ClientID              string            `json:"client_id"`
	RedirectURL           string            `json:"redirect_url"`
	AuthorizationEndpoint string            `json:"authorization_endpoint"`
	TokenEndpoint         string            `json:"token_endpoint"`
	Scopes                []string          `json:"scopes"`
	AdditionalParameters  map[string]string `json:"additional_parameters,omitempty"`
}

// integrationCallbackRequest is the body for POST /v1/integrations/callback.
type integrationCallbackRequest struct {
	Provider          string `json:"provider"`
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	RedirectURL       string `json:"redirect_url"`
}
