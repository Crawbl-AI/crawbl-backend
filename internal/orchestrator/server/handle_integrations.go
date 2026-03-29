package server

import (
	"net/http"

	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// availableIntegrations defines all integrations the platform supports.
// Each entry represents a third-party service users can connect via OAuth.
// New integrations are added here as they're implemented.
func availableIntegrations() []integrationItemResponse {
	return []integrationItemResponse{
		{
			Provider:    "google_calendar",
			Name:        "Google Calendar",
			Description: "View and create calendar events, check availability",
			IconURL:     "https://cdn.crawbl.com/integrations/google-calendar.png",
			IsConnected: false,
			IsEnabled:   true,
		},
		{
			Provider:    "gmail",
			Name:        "Gmail",
			Description: "Search and read emails, draft responses",
			IconURL:     "https://cdn.crawbl.com/integrations/gmail.png",
			IsConnected: false,
			IsEnabled:   true,
		},
		{
			Provider:    "slack",
			Name:        "Slack",
			Description: "Send messages, search channels, manage notifications",
			IconURL:     "https://cdn.crawbl.com/integrations/slack.png",
			IsConnected: false,
			IsEnabled:   false,
		},
		{
			Provider:    "jira",
			Name:        "Jira",
			Description: "Search and manage issues, track projects",
			IconURL:     "https://cdn.crawbl.com/integrations/jira.png",
			IsConnected: false,
			IsEnabled:   false,
		},
		{
			Provider:    "notion",
			Name:        "Notion",
			Description: "Search pages, create documents, manage databases",
			IconURL:     "https://cdn.crawbl.com/integrations/notion.png",
			IsConnected: false,
			IsEnabled:   false,
		},
		{
			Provider:    "asana",
			Name:        "Asana",
			Description: "Manage tasks, track project progress",
			IconURL:     "https://cdn.crawbl.com/integrations/asana.png",
			IsConnected: false,
			IsEnabled:   false,
		},
		{
			Provider:    "github",
			Name:        "GitHub",
			Description: "Browse repositories, manage issues and pull requests",
			IconURL:     "https://cdn.crawbl.com/integrations/github.png",
			IsConnected: false,
			IsEnabled:   false,
		},
		{
			Provider:    "zoom",
			Name:        "Zoom",
			Description: "Schedule and manage meetings",
			IconURL:     "https://cdn.crawbl.com/integrations/zoom.png",
			IsConnected: false,
			IsEnabled:   false,
		},
	}
}

// handleIntegrationsList returns all available integrations with their connection status.
// In the future, this will check the integration_connections table to determine
// which providers the user has actually connected via OAuth.
//
// GET /v1/integrations
func (s *Server) handleIntegrationsList(w http.ResponseWriter, r *http.Request) {
	_, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	// TODO: Query integration_connections table to check which providers
	// are connected for this user and set IsConnected=true accordingly.
	integrations := availableIntegrations()

	httpserver.WriteSuccessResponse(w, http.StatusOK, integrations)
}

// handleIntegrationConnect returns the OAuth configuration for a provider.
// The mobile app uses this to initiate the OAuth PKCE flow via flutter_appauth.
//
// POST /v1/integrations/connect
func (s *Server) handleIntegrationConnect(w http.ResponseWriter, r *http.Request) {
	_, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	var req integrationConnectRequest
	if err := decodeJSON(r, &req); err != nil || req.Provider == "" {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "provider is required")
		return
	}

	// TODO: Look up OAuth client credentials from config/secrets based on provider.
	// For now, return a placeholder response that tells the mobile app
	// this integration is not yet configured on the server side.
	httpserver.WriteErrorResponse(w, http.StatusNotImplemented, "integration "+req.Provider+" is not yet available")
}

// handleIntegrationCallback receives the OAuth authorization code from the mobile app
// after the user completes the OAuth flow. The backend exchanges the code for
// access/refresh tokens and stores them in the integration_connections table.
//
// POST /v1/integrations/callback
func (s *Server) handleIntegrationCallback(w http.ResponseWriter, r *http.Request) {
	_, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	var req integrationCallbackRequest
	if err := decodeJSON(r, &req); err != nil {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider == "" || req.AuthorizationCode == "" || req.CodeVerifier == "" {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "provider, authorization_code, and code_verifier are required")
		return
	}

	// TODO: Exchange authorization code for tokens using the provider's token endpoint.
	// Store tokens in integration_connections table (encrypted).
	// For now, return not implemented.
	httpserver.WriteErrorResponse(w, http.StatusNotImplemented, "OAuth callback for "+req.Provider+" is not yet implemented")
}
