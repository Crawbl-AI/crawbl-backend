package handler

import (
	"net/http"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// IntegrationsList returns both agent tools and third-party integrations
// in a single response. The mobile app renders this as two tabs in the profile
// capabilities screen: "Tools" and "Connected Apps".
//
// GET /v1/integrations
func IntegrationsList(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Fetch integrations (with connection status from DB).
		items, mErr := c.IntegrationService.ListIntegrations(r.Context(), &orchestratorservice.ListIntegrationsOpts{
			Sess:   c.NewSession(),
			UserID: user.ID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Build categories by merging tool categories (agent runtime) and integration categories (orchestrator).
		toolCats := agentruntimetools.ToolCategories()
		appCats := orchestrator.IntegrationCategories()
		categories := make([]dto.CategoryResponse, 0, len(toolCats)+len(appCats))
		for _, cat := range toolCats {
			categories = append(categories, dto.CategoryResponse{
				ID:       string(cat.ID),
				Name:     cat.Name,
				ImageURL: cat.ImageURL,
			})
		}
		for _, cat := range appCats {
			categories = append(categories, dto.CategoryResponse{
				ID:       cat.ID,
				Name:     cat.Name,
				ImageURL: cat.ImageURL,
			})
		}

		// Build items: tools first, then integrations. Filter to the
		// implemented subset — users must never see a tool the agent
		// cannot actually call. Roadmap entries live in the seed file
		// but stay out of the API response until their implementation
		// lands and the "implemented" flag flips in tools.json.
		catalog := agentruntimetools.ImplementedCatalog()
		itemsList := make([]dto.IntegrationItemResponse, 0, len(catalog)+len(items))

		for _, t := range catalog {
			itemsList = append(itemsList, dto.IntegrationItemResponse{
				Name:        t.DisplayName,
				Description: t.Description,
				IconURL:     t.IconURL,
				CategoryID:  string(t.Category),
				Type:        string(orchestrator.ItemTypeTool),
				Enabled:     true,
			})
		}

		for _, ig := range items {
			itemsList = append(itemsList, dto.IntegrationItemResponse{
				Name:        ig.Name,
				Description: ig.Description,
				IconURL:     ig.IconURL,
				CategoryID:  ig.CategoryID,
				Type:        string(orchestrator.ItemTypeApp),
				Provider:    ig.Provider,
				Enabled:     ig.IsEnabled,
				IsConnected: ig.IsConnected,
			})
		}

		WriteSuccess(w, http.StatusOK, dto.IntegrationsResponse{
			Categories: categories,
			Items:      itemsList,
		})
	}
}

// IntegrationConnect returns OAuth configuration for a provider.
//
// POST /v1/integrations/connect
func IntegrationConnect(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var req dto.IntegrationConnectRequest
		if err := DecodeJSON(r, &req); err != nil || req.Provider == "" {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "provider is required")
			return
		}

		config, mErr := c.IntegrationService.GetOAuthConfig(r.Context(), &orchestratorservice.GetOAuthConfigOpts{
			Sess:     c.NewSession(),
			UserID:   user.ID,
			Provider: req.Provider,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.IntegrationConnectResponse{
			ClientID:              config.ClientID,
			RedirectURL:           config.RedirectURL,
			AuthorizationEndpoint: config.AuthorizationEndpoint,
			TokenEndpoint:         config.TokenEndpoint,
			Scopes:                config.Scopes,
			AdditionalParameters:  config.AdditionalParameters,
		})
	}
}

// IntegrationCallback exchanges the OAuth code for tokens.
//
// POST /v1/integrations/callback
func IntegrationCallback(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var req dto.IntegrationCallbackRequest
		if err := DecodeJSON(r, &req); err != nil {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Provider == "" || req.AuthorizationCode == "" || req.CodeVerifier == "" {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "provider, authorization_code, and code_verifier are required")
			return
		}

		if mErr := c.IntegrationService.HandleOAuthCallback(r.Context(), &orchestratorservice.OAuthCallbackOpts{
			Sess:              c.NewSession(),
			UserID:            user.ID,
			Provider:          req.Provider,
			AuthorizationCode: req.AuthorizationCode,
			CodeVerifier:      req.CodeVerifier,
			RedirectURL:       req.RedirectURL,
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
