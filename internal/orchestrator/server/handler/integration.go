package handler

import (
	"net/http"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
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
			UserID: user.ID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Build categories by merging tool categories (agent runtime) and integration categories (orchestrator).
		toolCats := agentruntimetools.ToolCategories()
		appCats := orchestrator.IntegrationCategories()
		categories := make([]*mobilev1.CategoryResponse, 0, len(toolCats)+len(appCats))
		for _, cat := range toolCats {
			categories = append(categories, &mobilev1.CategoryResponse{
				Id:       string(cat.ID),
				Name:     cat.Name,
				ImageUrl: cat.ImageURL,
			})
		}
		for _, cat := range appCats {
			categories = append(categories, &mobilev1.CategoryResponse{
				Id:       cat.ID,
				Name:     cat.Name,
				ImageUrl: cat.ImageURL,
			})
		}

		// Build items: tools first, then integrations.
		catalog := agentruntimetools.ImplementedCatalog()
		itemsList := make([]*mobilev1.IntegrationItemResponse, 0, len(catalog)+len(items))

		for _, t := range catalog {
			itemsList = append(itemsList, &mobilev1.IntegrationItemResponse{
				Name:        t.DisplayName,
				Description: t.Description,
				IconUrl:     t.IconURL,
				CategoryId:  string(t.Category),
				Type:        string(orchestrator.ItemTypeTool),
				Enabled:     true,
			})
		}

		for _, ig := range items {
			itemsList = append(itemsList, &mobilev1.IntegrationItemResponse{
				Name:        ig.Name,
				Description: ig.Description,
				IconUrl:     ig.IconURL,
				CategoryId:  ig.CategoryID,
				Type:        string(orchestrator.ItemTypeApp),
				Provider:    ig.Provider,
				Enabled:     ig.IsEnabled,
				IsConnected: ig.IsConnected,
			})
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.IntegrationsResponse{
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

		var req mobilev1.IntegrationConnectRequest
		if err := DecodeProtoJSON(r, &req); err != nil || req.GetProvider() == "" {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "provider is required")
			return
		}

		config, mErr := c.IntegrationService.GetOAuthConfig(r.Context(), &orchestratorservice.GetOAuthConfigOpts{
			UserID:   user.ID,
			Provider: req.GetProvider(),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.IntegrationConnectResponse{
			ClientId:              config.ClientID,
			RedirectUrl:           config.RedirectURL,
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

		var req mobilev1.IntegrationCallbackRequest
		if err := DecodeProtoJSON(r, &req); err != nil {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.GetProvider() == "" || req.GetAuthorizationCode() == "" || req.GetCodeVerifier() == "" {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "provider, authorization_code, and code_verifier are required")
			return
		}

		if mErr := c.IntegrationService.HandleOAuthCallback(r.Context(), &orchestratorservice.OAuthCallbackOpts{
			UserID:            user.ID,
			Provider:          req.GetProvider(),
			AuthorizationCode: req.GetAuthorizationCode(),
			CodeVerifier:      req.GetCodeVerifier(),
			RedirectURL:       req.GetRedirectUrl(),
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
