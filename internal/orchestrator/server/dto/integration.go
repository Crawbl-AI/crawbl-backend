package dto

// IntegrationsResponse contains tool categories and a flat list of items
// (both tools and third-party integrations).
// WriteSuccessResponse wraps this in the standard {"data": ...} envelope.
type IntegrationsResponse struct {
	Categories []CategoryResponse        `json:"categories"`
	Items      []IntegrationItemResponse `json:"items"`
}

// CategoryResponse represents a tool category in the integrations list.
type CategoryResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

// IntegrationItemResponse represents a single tool or integration item.
type IntegrationItemResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	CategoryID  string `json:"category_id"`
	Type        string `json:"type"`
	Provider    string `json:"provider,omitempty"`
	Enabled     bool   `json:"enabled"`
	IsConnected bool   `json:"is_connected"`
}

// IntegrationConnectRequest is the body for POST /v1/integrations/connect.
type IntegrationConnectRequest struct {
	Provider string `json:"provider"`
}

// IntegrationConnectResponse returns OAuth config for the mobile app to start the flow.
type IntegrationConnectResponse struct {
	ClientID              string            `json:"client_id"`
	RedirectURL           string            `json:"redirect_url"`
	AuthorizationEndpoint string            `json:"authorization_endpoint"`
	TokenEndpoint         string            `json:"token_endpoint"`
	Scopes                []string          `json:"scopes"`
	AdditionalParameters  map[string]string `json:"additional_parameters,omitempty"`
}

// IntegrationCallbackRequest is the body for POST /v1/integrations/callback.
type IntegrationCallbackRequest struct {
	Provider          string `json:"provider"`
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	RedirectURL       string `json:"redirect_url"`
}
