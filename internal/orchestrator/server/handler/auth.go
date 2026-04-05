package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// HealthCheck returns the server health status and version.
// This endpoint is unauthenticated and used by load balancers and monitoring
// systems to verify the server is responsive.
func HealthCheck(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteSuccess(w, http.StatusOK, &dto.HealthCheckResponse{
			Online:  true,
			Version: orchestrator.APIVersion,
		})
	}
}

// Legal returns the current terms of service and privacy policy documents.
// This endpoint is unauthenticated and provides legal documents for display
// in the mobile app before user authentication.
func Legal(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		legalDocuments, mErr := c.AuthService.GetLegalDocuments(r.Context())
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, &dto.LegalResponse{
			TermsOfService:        legalDocuments.TermsOfService,
			PrivacyPolicy:         legalDocuments.PrivacyPolicy,
			TermsOfServiceVersion: legalDocuments.TermsOfServiceVersion,
			PrivacyPolicyVersion:  legalDocuments.PrivacyPolicyVersion,
		})
	}
}

// SaveFCMToken stores a Firebase Cloud Messaging push token for the authenticated user.
// This token is used to send push notifications to the user's device.
// The request body must contain a valid pushToken field.
func SaveFCMToken(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.SavePushTokenRequest
		if err := DecodeJSON(r, &reqBody); err != nil || reqBody.PushToken == "" {
			httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if mErr := c.AuthService.SavePushToken(r.Context(), &orchestratorservice.SavePushTokenOpts{
			Sess:      c.NewSession(),
			Principal: principal,
			PushToken: reqBody.PushToken,
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, &dto.SavePushTokenResponse{Success: true})
	}
}

// SignIn authenticates an existing user via Firebase token.
// If the user exists in the system, it seeds their workspace runtime for the session.
// Returns 204 No Content on success, indicating the user is now authenticated.
func SignIn(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		c.Logger.Info("handleAuthSignIn: starting sign in", "subject", principal.Subject)

		user, mErr := c.AuthService.SignIn(r.Context(), &orchestratorservice.SignInOpts{
			Sess:      c.NewSession(),
			Principal: principal,
		})
		if mErr != nil {
			c.Logger.Error("handleAuthSignIn: sign in failed", "error", mErr.Error())
			WriteError(w, mErr)
			return
		}

		c.Logger.Info("handleAuthSignIn: sign in succeeded", "user_id", user.ID)

		go seedWorkspaceRuntime(c, r.Context(), user.ID, "sign_in")
		httpserver.WriteNoContent(w)
	}
}

// SignUp creates a new user account from the Firebase authentication token.
// After successful registration, it seeds the user's default workspace runtime.
// Returns 204 No Content on success.
func SignUp(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		user, mErr := c.AuthService.SignUp(r.Context(), &orchestratorservice.SignUpOpts{
			Sess:      c.NewSession(),
			Principal: principal,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		go seedWorkspaceRuntime(c, r.Context(), user.ID, "sign_up")
		httpserver.WriteNoContent(w)
	}
}

// DeleteAccount deletes the authenticated user's account.
// Restricted to non-production environments. In addition to soft-deleting the user,
// it also deletes any agent runtime CRs associated with the user's workspaces to prevent
// orphaned agent runtime resources in the cluster.
func DeleteAccount(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Block account deletion in production.
		env := strings.ToLower(strings.TrimSpace(c.HTTPMiddleware.Environment))
		if env == "production" || env == "prod" {
			WriteError(w, merrors.ErrAccountDeletionDisabled)
			return
		}

		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.AuthDeleteRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}

		// Look up the user to get their internal UUID for workspace lookup.
		sess := c.NewSession()
		user, userErr := c.AuthService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
			Sess:    sess,
			Subject: principal.Subject,
		})

		// Fetch workspaces before deletion so we can clean up agent runtime CRs.
		var workspaces []*orchestrator.Workspace
		if userErr == nil && user != nil {
			workspaces, _ = c.WorkspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
				Sess:   sess,
				UserID: user.ID,
			})
		}

		if mErr := c.AuthService.Delete(r.Context(), &orchestratorservice.DeleteOpts{
			Sess:        sess,
			Principal:   principal,
			Reason:      reqBody.Reason,
			Description: reqBody.Description,
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Best-effort cleanup of agent runtime CRs for each workspace.
		if c.RuntimeClient != nil {
			for _, ws := range workspaces {
				if delErr := c.RuntimeClient.DeleteRuntime(r.Context(), ws.ID); delErr != nil {
					c.Logger.Warn("failed to delete agent runtime on account deletion",
						"workspace_id", ws.ID,
						"user", principal.Subject,
						"error", delErr,
					)
				} else {
					c.Logger.Info("deleted agent runtime on account deletion",
						"workspace_id", ws.ID,
						"user", principal.Subject,
					)
				}
			}
		}

		httpserver.WriteNoContent(w)
	}
}

// UserProfile retrieves the authenticated user's profile information.
// Returns user details including preferences, subscription status, and account state.
func UserProfile(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, toUserProfileResponse(user))
	}
}

// UpdateUser modifies the authenticated user's profile information.
// Supports updating nickname, name, surname, country code, date of birth, and preferences.
// Only provided fields are updated; omitted fields retain their current values.
func UpdateUser(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.UserUpdateRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
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

		if _, mErr := c.AuthService.UpdateProfile(r.Context(), &orchestratorservice.UpdateProfileOpts{
			Sess:        c.NewSession(),
			Principal:   principal,
			Nickname:    reqBody.Nickname,
			Name:        reqBody.Name,
			Surname:     reqBody.Surname,
			CountryCode: reqBody.CountryCode,
			DateOfBirth: dateOfBirth,
			Preferences: preferences,
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		httpserver.WriteNoContent(w)
	}
}

// UserLegal retrieves the legal documents along with the user's acceptance status.
// Returns terms of service and privacy policy content and versions, plus whether
// the user has agreed to each document.
func UserLegal(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		legalDocuments, mErr := c.AuthService.GetLegalDocuments(r.Context())
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, &dto.UserLegalResponse{
			TermsOfService:             legalDocuments.TermsOfService,
			PrivacyPolicy:              legalDocuments.PrivacyPolicy,
			TermsOfServiceVersion:      legalDocuments.TermsOfServiceVersion,
			PrivacyPolicyVersion:       legalDocuments.PrivacyPolicyVersion,
			HasAgreedWithTerms:         user.HasAgreedWithTerms,
			HasAgreedWithPrivacyPolicy: user.HasAgreedWithPrivacyPolicy,
		})
	}
}

// AcceptLegal records the user's acceptance of the specified legal document versions.
// The user must accept both terms of service and privacy policy versions to proceed.
// This updates the user's legal acceptance status in the database.
func AcceptLegal(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.UserLegalAcceptRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if _, mErr := c.AuthService.AcceptLegal(r.Context(), &orchestratorservice.AcceptLegalOpts{
			Sess:                  c.NewSession(),
			Principal:             principal,
			TermsOfServiceVersion: reqBody.TermsOfServiceVersion,
			PrivacyPolicyVersion:  reqBody.PrivacyPolicyVersion,
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		httpserver.WriteNoContent(w)
	}
}

// Logout clears the push notification token for the authenticated user's device
// so the user stops receiving push notifications after signing out.
// The Firebase session remains valid (stateless auth); only the push token is cleared.
// Returns 204 No Content on success.
func Logout(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Best-effort: clear push tokens so the device stops receiving notifications.
		_ = c.AuthService.ClearPushToken(r.Context(), &orchestratorservice.ClearPushTokenOpts{
			Sess:   c.NewSession(),
			UserID: user.ID,
		})

		httpserver.WriteNoContent(w)
	}
}

// toUserProfileResponse converts a domain User to the API response format.
// It handles nil pointer fields and provides default values for subscription
// when the user has no active subscription (defaults to "Freemium").
func toUserProfileResponse(user *orchestrator.User) *dto.UserProfileResponse {
	subscriptionName := user.Subscription.Name
	if subscriptionName == "" {
		subscriptionName = orchestrator.DefaultSubscriptionName
	}
	subscriptionCode := user.Subscription.Code
	if subscriptionCode == "" {
		subscriptionCode = orchestrator.DefaultSubscriptionCode
	}

	return &dto.UserProfileResponse{
		Email:                      user.Email,
		FirebaseUID:                user.Subject,
		Nickname:                   user.Nickname,
		Name:                       user.Name,
		Surname:                    user.Surname,
		AvatarURL:                  StringOrEmpty(user.AvatarURL),
		CountryCode:                StringOrEmpty(user.CountryCode),
		DateOfBirth:                user.DateOfBirth,
		CreatedAt:                  user.CreatedAt,
		IsDeleted:                  user.DeletedAt != nil,
		IsBanned:                   user.IsBanned,
		HasAgreedWithTerms:         user.HasAgreedWithTerms,
		HasAgreedWithPrivacyPolicy: user.HasAgreedWithPrivacyPolicy,
		Preferences: dto.UserPreferencesResponse{
			PlatformTheme:    StringOrEmpty(user.Preferences.PlatformTheme),
			PlatformLanguage: StringOrEmpty(user.Preferences.PlatformLanguage),
			CurrencyCode:     StringOrEmpty(user.Preferences.CurrencyCode),
		},
		Subscription: dto.UserSubscriptionResponse{
			Name:      subscriptionName,
			Code:      subscriptionCode,
			ExpiresAt: user.Subscription.ExpiresAt,
		},
	}
}

// seedWorkspaceRuntime triggers the workspace list operation to ensure the user's
// workspace runtime is initialized. This is called after sign-in and sign-up
// to warm up the workspace state for the user session.
func seedWorkspaceRuntime(c *Context, ctx context.Context, userID, trigger string) {
	sess := c.NewSession()
	workspaces, mErr := c.WorkspaceService.ListByUserID(ctx, &orchestratorservice.ListWorkspacesOpts{
		Sess:   sess,
		UserID: userID,
	})
	if mErr != nil {
		c.Logger.Warn("failed to seed workspace runtime",
			"trigger", trigger,
			"user_id", userID,
			"error", mErr.Error(),
		)
		return
	}

	// Eagerly bootstrap default agents and conversations for each workspace
	// so the mobile app sees correct agent counts immediately.
	for _, ws := range workspaces {
		if _, mErr := c.ChatService.ListAgents(ctx, &orchestratorservice.ListAgentsOpts{
			Sess:        c.NewSession(),
			UserID:      userID,
			WorkspaceID: ws.ID,
		}); mErr != nil {
			c.Logger.Warn("failed to bootstrap workspace agents",
				"trigger", trigger,
				"user_id", userID,
				"workspace_id", ws.ID,
				"error", mErr.Error(),
			)
		}
	}
}
