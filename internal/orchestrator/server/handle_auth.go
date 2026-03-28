package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// handleHealthCheck returns the server health status and version.
// This endpoint is unauthenticated and used by load balancers and monitoring
// systems to verify the server is responsive.
func (s *Server) handleHealthCheck(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteSuccessResponse(w, http.StatusOK, &healthCheckResponse{
		Online:  true,
		Version: "1.0.0",
	})
}

// handleLegal returns the current terms of service and privacy policy documents.
// This endpoint is unauthenticated and provides legal documents for display
// in the mobile app before user authentication.
func (s *Server) handleLegal(w http.ResponseWriter, r *http.Request) {
	legalDocuments, mErr := s.authService.GetLegalDocuments(r.Context())
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, &legalResponse{
		TermsOfService:        legalDocuments.TermsOfService,
		PrivacyPolicy:         legalDocuments.PrivacyPolicy,
		TermsOfServiceVersion: legalDocuments.TermsOfServiceVersion,
		PrivacyPolicyVersion:  legalDocuments.PrivacyPolicyVersion,
	})
}

// handleSaveFCMToken stores a Firebase Cloud Messaging push token for the authenticated user.
// This token is used to send push notifications to the user's device.
// The request body must contain a valid pushToken field.
func (s *Server) handleSaveFCMToken(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	var reqBody savePushTokenRequest
	if err := decodeJSON(r, &reqBody); err != nil || reqBody.PushToken == "" {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if mErr := s.authService.SavePushToken(r.Context(), &orchestratorservice.SavePushTokenOpts{
		Sess:      s.newSession(),
		Principal: principal,
		PushToken: reqBody.PushToken,
	}); mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteJSONResponse(w, http.StatusOK, &savePushTokenResponse{Success: true})
}

// handleAuthSignIn authenticates an existing user via Firebase token.
// If the user exists in the system, it seeds their workspace runtime for the session.
// Returns 204 No Content on success, indicating the user is now authenticated.
func (s *Server) handleAuthSignIn(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	s.logger.Info("handleAuthSignIn: starting sign in", "subject", principal.Subject)

	user, mErr := s.authService.SignIn(r.Context(), &orchestratorservice.SignInOpts{
		Sess:      s.newSession(),
		Principal: principal,
	})
	if mErr != nil {
		s.logger.Error("handleAuthSignIn: sign in failed", "error", mErr.Error())
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	s.logger.Info("handleAuthSignIn: sign in succeeded", "user_id", user.ID)

	s.seedWorkspaceRuntime(r.Context(), user.ID, "sign_in")
	httpserver.WriteNoContent(w)
}

// handleAuthSignUp creates a new user account from the Firebase authentication token.
// After successful registration, it seeds the user's default workspace runtime.
// Returns 204 No Content on success.
func (s *Server) handleAuthSignUp(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	user, mErr := s.authService.SignUp(r.Context(), &orchestratorservice.SignUpOpts{
		Sess:      s.newSession(),
		Principal: principal,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	s.seedWorkspaceRuntime(r.Context(), user.ID, "sign_up")
	httpserver.WriteNoContent(w)
}

// handleAuthDelete deletes the authenticated user's account.
// Restricted to non-production environments. In addition to soft-deleting the user,
// it also deletes any UserSwarm CRs associated with the user's workspaces to prevent
// orphaned swarm resources in the cluster.
func (s *Server) handleAuthDelete(w http.ResponseWriter, r *http.Request) {
	// Block account deletion in production.
	env := strings.ToLower(strings.TrimSpace(s.httpMiddleware.Environment))
	if env == "production" || env == "prod" {
		httpserver.WriteErrorResponse(w, http.StatusForbidden, "account deletion is not available")
		return
	}

	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	var reqBody authDeleteRequest
	if err := decodeJSON(r, &reqBody); err != nil {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch workspaces before deletion so we can clean up UserSwarm CRs.
	sess := s.newSession()
	workspaces, _ := s.workspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
		Sess:   sess,
		UserID: principal.Subject,
	})

	if mErr := s.authService.Delete(r.Context(), &orchestratorservice.DeleteOpts{
		Sess:        sess,
		Principal:   principal,
		Reason:      reqBody.Reason,
		Description: reqBody.Description,
	}); mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	// Best-effort cleanup of UserSwarm CRs for each workspace.
	if s.runtimeClient != nil && len(workspaces) > 0 {
		for _, ws := range workspaces {
			if delErr := s.runtimeClient.DeleteRuntime(r.Context(), ws.ID); delErr != nil {
				s.logger.Warn("failed to delete userswarm on account deletion",
					"workspace_id", ws.ID,
					"user", principal.Subject,
					"error", delErr,
				)
			}
		}
	}

	httpserver.WriteNoContent(w)
}

// handleUsersProfile retrieves the authenticated user's profile information.
// Returns user details including preferences, subscription status, and account state.
func (s *Server) handleUsersProfile(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, toUserProfileResponse(user))
}

// handleUsersUpdate modifies the authenticated user's profile information.
// Supports updating nickname, name, surname, country code, date of birth, and preferences.
// Only provided fields are updated; omitted fields retain their current values.
func (s *Server) handleUsersUpdate(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	var reqBody userUpdateRequest
	if err := decodeJSON(r, &reqBody); err != nil {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var preferences *orchestrator.UserPreferences
	if reqBody.Preferences != nil {
		preferences = &orchestrator.UserPreferences{
			PlatformTheme:    reqBody.Preferences.PlatformTheme,
			PlatformLanguage: reqBody.Preferences.PlatformLanguage,
			CurrencyCode:     reqBody.Preferences.CurrencyCode,
		}
	}

	var dateOfBirth *time.Time
	if reqBody.DateOfBirth != nil && !reqBody.DateOfBirth.IsZero() {
		value := reqBody.DateOfBirth.UTC()
		dateOfBirth = &value
	}

	if _, mErr := s.authService.UpdateProfile(r.Context(), &orchestratorservice.UpdateProfileOpts{
		Sess:        s.newSession(),
		Principal:   principal,
		Nickname:    reqBody.Nickname,
		Name:        reqBody.Name,
		Surname:     reqBody.Surname,
		CountryCode: reqBody.CountryCode,
		DateOfBirth: dateOfBirth,
		Preferences: preferences,
	}); mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteNoContent(w)
}

// handleUsersLegal retrieves the legal documents along with the user's acceptance status.
// Returns terms of service and privacy policy content and versions, plus whether
// the user has agreed to each document.
func (s *Server) handleUsersLegal(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	legalDocuments, mErr := s.authService.GetLegalDocuments(r.Context())
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, &userLegalResponse{
		TermsOfService:             legalDocuments.TermsOfService,
		PrivacyPolicy:              legalDocuments.PrivacyPolicy,
		TermsOfServiceVersion:      legalDocuments.TermsOfServiceVersion,
		PrivacyPolicyVersion:       legalDocuments.PrivacyPolicyVersion,
		HasAgreedWithTerms:         user.HasAgreedWithTerms,
		HasAgreedWithPrivacyPolicy: user.HasAgreedWithPrivacyPolicy,
	})
}

// handleUsersLegalAccept records the user's acceptance of the specified legal document versions.
// The user must accept both terms of service and privacy policy versions to proceed.
// This updates the user's legal acceptance status in the database.
func (s *Server) handleUsersLegalAccept(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	var reqBody userLegalAcceptRequest
	if err := decodeJSON(r, &reqBody); err != nil {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, mErr := s.authService.AcceptLegal(r.Context(), &orchestratorservice.AcceptLegalOpts{
		Sess:                  s.newSession(),
		Principal:             principal,
		TermsOfServiceVersion: reqBody.TermsOfServiceVersion,
		PrivacyPolicyVersion:  reqBody.PrivacyPolicyVersion,
	}); mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteNoContent(w)
}

// toUserProfileResponse converts a domain User to the API response format.
// It handles nil pointer fields and provides default values for subscription
// when the user has no active subscription (defaults to "Freemium").
func toUserProfileResponse(user *orchestrator.User) *userProfileResponse {
	subscriptionName := user.Subscription.Name
	if subscriptionName == "" {
		subscriptionName = "Freemium"
	}
	subscriptionCode := user.Subscription.Code
	if subscriptionCode == "" {
		subscriptionCode = "freemium"
	}

	return &userProfileResponse{
		Email:                      user.Email,
		FirebaseUID:                user.Subject,
		Nickname:                   user.Nickname,
		Name:                       user.Name,
		Surname:                    user.Surname,
		AvatarURL:                  stringOrEmpty(user.AvatarURL),
		CountryCode:                stringOrEmpty(user.CountryCode),
		DateOfBirth:                user.DateOfBirth,
		CreatedAt:                  user.CreatedAt,
		IsDeleted:                  user.DeletedAt != nil,
		IsBanned:                   user.IsBanned,
		HasAgreedWithTerms:         user.HasAgreedWithTerms,
		HasAgreedWithPrivacyPolicy: user.HasAgreedWithPrivacyPolicy,
		Preferences: userPreferencesResponse{
			PlatformTheme:    stringOrEmpty(user.Preferences.PlatformTheme),
			PlatformLanguage: stringOrEmpty(user.Preferences.PlatformLanguage),
			CurrencyCode:     stringOrEmpty(user.Preferences.CurrencyCode),
		},
		Subscription: userSubscriptionResponse{
			Name:      subscriptionName,
			Code:      subscriptionCode,
			ExpiresAt: user.Subscription.ExpiresAt,
		},
	}
}

// seedWorkspaceRuntime triggers the workspace list operation to ensure the user's
// workspace runtime is initialized. This is called after sign-in and sign-up
// to warm up the workspace state for the user session.
//
// Parameters:
//   - ctx: Request context for cancellation and tracing
//   - userID: The user's unique identifier
//   - trigger: Description of the operation that triggered seeding (for logging)
func (s *Server) seedWorkspaceRuntime(ctx context.Context, userID, trigger string) {
	_, mErr := s.workspaceService.ListByUserID(ctx, &orchestratorservice.ListWorkspacesOpts{
		Sess:   s.newSession(),
		UserID: userID,
	})
	if mErr != nil {
		s.logger.Warn("failed to seed workspace runtime",
			"trigger", trigger,
			"user_id", userID,
			"error", mErr.Error(),
		)
	}
}

// stringOrEmpty safely dereferences a string pointer, returning an empty string
// if the pointer is nil. This prevents nil pointer dereference panics when
// converting optional string fields for API responses.
func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
