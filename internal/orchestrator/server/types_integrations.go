package server

// integrationItemResponse represents a single integration in the list.
// Field names must match the mobile app's IntegrationItemData model.
type integrationItemResponse struct {
	Provider    string `json:"provider"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	IsConnected bool   `json:"is_connected"`
	IsEnabled   bool   `json:"is_enabled"`
}

// integrationsListResponse wraps the list for GET /v1/integrations.
type integrationsListResponse struct {
	Data []integrationItemResponse `json:"data"`
}

// integrationConnectRequest is the body for POST /v1/integrations/connect.
type integrationConnectRequest struct {
	Provider string `json:"provider"`
}

// integrationConnectResponse returns OAuth config for the mobile app to start the flow.
// Field names must match IntegrationConnectResponse in the mobile app.
type integrationConnectResponse struct {
	ClientID              string            `json:"client_id"`
	RedirectURL           string            `json:"redirect_url"`
	AuthorizationEndpoint string            `json:"authorization_endpoint"`
	TokenEndpoint         string            `json:"token_endpoint"`
	Scopes                []string          `json:"scopes"`
	AdditionalParameters  map[string]string `json:"additional_parameters,omitempty"`
}

// integrationCallbackRequest is the body for POST /v1/integrations/callback.
// Contains the OAuth authorization code from the mobile OAuth flow (PKCE).
type integrationCallbackRequest struct {
	Provider          string `json:"provider"`
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	RedirectURL       string `json:"redirect_url"`
}
