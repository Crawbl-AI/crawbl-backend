package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/handler"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// registerRoutes creates the HTTP router with all endpoints and middleware.
// It configures panic recovery, request ID, real IP, and authentication.
func registerRoutes(s *Server) http.Handler {
	h := &handler.Context{
		DB:                 s.db,
		Logger:             s.logger,
		AuthService:        s.authService,
		WorkspaceService:   s.workspaceService,
		ChatService:        s.chatService,
		AgentService:       s.agentService,
		IntegrationService: s.integrationService,
		HTTPMiddleware:     s.httpMiddleware,
		Broadcaster:        s.broadcaster,
		RuntimeClient:      s.runtimeClient,
	}

	router := chi.NewRouter()

	// Middleware stack (order matters):
	// 1. RequestID - generates unique ID for each request
	// 2. RealIP - extracts real client IP from headers
	// 3. PanicRecoverer - catches panics and logs them
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)
	router.Use(PanicRecoverer(s.logger))

	router.Route("/v1", func(r chi.Router) {
		r.Get("/health", handler.HealthCheck(h))
		r.Get("/legal", handler.Legal(h))
		r.Get("/models", handler.ListModels(h))

		r.Group(func(r chi.Router) {
			r.Use(httpserver.AuthMiddleware(s.httpMiddleware, s.logger))
			r.Post("/fcm-token", handler.SaveFCMToken(h))
			r.Post("/auth/sign-in", handler.SignIn(h))
			r.Post("/auth/sign-up", handler.SignUp(h))
			r.Delete("/auth/delete", handler.DeleteAccount(h))
			r.Get("/users/profile", handler.UserProfile(h))
			r.Patch("/users", handler.UpdateUser(h))
			r.Get("/users/legal", handler.UserLegal(h))
			r.Post("/users/legal/accept", handler.AcceptLegal(h))
			r.Get("/workspaces", handler.WorkspacesList(h))
			r.Get("/workspaces/{id}", handler.WorkspaceGet(h))
			r.Get("/workspaces/{workspaceId}/agents", handler.WorkspaceAgentsList(h))
			r.Get("/workspaces/{workspaceId}/conversations", handler.ConversationsList(h))
			r.Post("/workspaces/{workspaceId}/conversations", handler.ConversationCreate(h))
			r.Get("/workspaces/{workspaceId}/conversations/{id}", handler.ConversationGet(h))
			r.Delete("/workspaces/{workspaceId}/conversations/{id}", handler.ConversationDelete(h))
			r.Post("/workspaces/{workspaceId}/conversations/{id}/read", handler.ConversationMarkRead(h))
			r.Get("/workspaces/{workspaceId}/conversations/{id}/messages", handler.MessagesList(h))
			r.Get("/workspaces/{workspaceId}/conversations/{id}/messages/search", handler.SearchMessages(h))
			r.Post("/workspaces/{workspaceId}/conversations/{id}/messages", handler.MessagesSend(h))
			r.Post("/workspaces/{workspaceId}/messages/{id}/action", handler.ActionCardResponse(h))
			r.Post("/auth/logout", handler.Logout(h))
			r.Post("/uploads", handler.FileUpload(h))
			r.Get("/integrations", handler.IntegrationsList(h))
			r.Post("/integrations/connect", handler.IntegrationConnect(h))
			r.Post("/integrations/callback", handler.IntegrationCallback(h))
			r.Get("/agents/{id}", handler.GetAgent(h))
			r.Get("/agents/{id}/details", handler.GetAgentDetails(h))
			r.Get("/agents/{id}/settings", handler.GetAgentSettings(h))
			r.Get("/agents/{id}/tools", handler.GetAgentTools(h))
			r.Get("/agents/{id}/memories", handler.GetAgentMemories(h))
			r.Delete("/agents/{id}/memories/{key}", handler.DeleteAgentMemory(h))
			r.Post("/agents/{id}/memories", handler.CreateAgentMemory(h))
		})
	})

	return router
}
