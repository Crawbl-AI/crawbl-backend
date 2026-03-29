package server

// integrationsResponse contains tool categories and a flat list of items
// (both tools and third-party integrations).
// WriteSuccessResponse wraps this in the standard {"data": ...} envelope.
type integrationsResponse struct {
	Categories []categoryResponse        `json:"categories"`
	Items      []integrationItemResponse `json:"items"`
}

// categoryResponse represents a tool category in the integrations list.
type categoryResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

// integrationItemResponse represents a single tool or integration item.
type integrationItemResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	CategoryID  string `json:"category_id"`
	Type        string `json:"type"`
	Provider    string `json:"provider,omitempty"`
	Enabled     bool   `json:"enabled"`
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
