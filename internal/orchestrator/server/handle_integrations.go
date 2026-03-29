package server

import (
	"net/http"

	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// handleIntegrationsList returns all available integrations with the user's connection status.
//
// GET /v1/integrations
func (s *Server) handleIntegrationsList(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	items, mErr := s.integrationService.ListIntegrations(r.Context(), &orchestratorservice.ListIntegrationsOpts{
		Sess:   s.newSession(),
		UserID: user.ID,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	response := make([]integrationItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, integrationItemResponse{
			Provider:    item.Provider,
			Name:        item.Name,
			Description: item.Description,
			IconURL:     item.IconURL,
			IsConnected: item.IsConnected,
			IsEnabled:   item.IsEnabled,
		})
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, response)
}

// handleIntegrationConnect returns OAuth configuration for a provider.
//
// POST /v1/integrations/connect
func (s *Server) handleIntegrationConnect(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	var req integrationConnectRequest
	if err := decodeJSON(r, &req); err != nil || req.Provider == "" {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "provider is required")
		return
	}

	config, mErr := s.integrationService.GetOAuthConfig(r.Context(), &orchestratorservice.GetOAuthConfigOpts{
		Sess:     s.newSession(),
		UserID:   user.ID,
		Provider: req.Provider,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, integrationConnectResponse{
		ClientID:              config.ClientID,
		RedirectURL:           config.RedirectURL,
		AuthorizationEndpoint: config.AuthorizationEndpoint,
		TokenEndpoint:         config.TokenEndpoint,
		Scopes:                config.Scopes,
		AdditionalParameters:  config.AdditionalParameters,
	})
}

// handleIntegrationCallback exchanges the OAuth code for tokens.
//
// POST /v1/integrations/callback
func (s *Server) handleIntegrationCallback(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
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

	if mErr := s.integrationService.HandleOAuthCallback(r.Context(), &orchestratorservice.OAuthCallbackOpts{
		Sess:              s.newSession(),
		UserID:            user.ID,
		Provider:          req.Provider,
		AuthorizationCode: req.AuthorizationCode,
		CodeVerifier:      req.CodeVerifier,
		RedirectURL:       req.RedirectURL,
	}); mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
