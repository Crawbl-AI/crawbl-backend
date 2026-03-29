package server

import (
	"net/http"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// handleIntegrationsList returns both agent tools and third-party integrations
// in a single response. The mobile app renders this as two tabs in the profile
// capabilities screen: "Tools" and "Connected Apps".
//
// GET /v1/integrations
func (s *Server) handleIntegrationsList(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	// Fetch integrations (with connection status from DB).
	items, mErr := s.integrationService.ListIntegrations(r.Context(), &orchestratorservice.ListIntegrationsOpts{
		Sess:   s.newSession(),
		UserID: user.ID,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	// Build categories by merging tool categories (zeroclaw) and integration categories (orchestrator).
	toolCats := zeroclaw.ToolCategories()
	appCats := orchestrator.IntegrationCategories()
	categories := make([]categoryResponse, 0, len(toolCats)+len(appCats))
	for _, c := range toolCats {
		categories = append(categories, categoryResponse{
			ID:       string(c.ID),
			Name:     c.Name,
			ImageURL: c.ImageURL,
		})
	}
	for _, c := range appCats {
		categories = append(categories, categoryResponse{
			ID:       c.ID,
			Name:     c.Name,
			ImageURL: c.ImageURL,
		})
	}

	// Build items: tools first, then integrations.
	catalog := zeroclaw.DefaultToolCatalog()
	itemsList := make([]integrationItemResponse, 0, len(catalog)+len(items))

	for _, t := range catalog {
		itemsList = append(itemsList, integrationItemResponse{
			Name:        t.DisplayName,
			Description: t.Description,
			IconURL:     t.IconURL,
			CategoryID:  string(t.Category),
			Type:        string(orchestrator.ItemTypeTool),
			Enabled:     true,
		})
	}

	for _, ig := range items {
		itemsList = append(itemsList, integrationItemResponse{
			Name:        ig.Name,
			Description: ig.Description,
			IconURL:     ig.IconURL,
			CategoryID:  ig.CategoryID,
			Type:        string(orchestrator.ItemTypeApp),
			Provider:    ig.Provider,
			Enabled:     ig.IsEnabled,
		})
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, integrationsResponse{
		Data: integrationsData{
			Categories: categories,
			Items:      itemsList,
		},
	})
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
