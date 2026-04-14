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

const errInvalidRequestBody = "invalid request body"

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
			httpserver.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		const minFCMTokenLength = 32
		const maxFCMTokenLength = 4096

		var reqBody dto.SavePushTokenRequest
		if err := DecodeJSON(r, &reqBody); err != nil || reqBody.PushToken == "" {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}
		if len(reqBody.PushToken) < minFCMTokenLength {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "push token is too short")
			return
		}
		if len(reqBody.PushToken) > maxFCMTokenLength {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "push token exceeds maximum allowed length")
			return
		}

		if mErr := c.AuthService.SavePushToken(r.Context(), &orchestratorservice.SavePushTokenOpts{
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
			httpserver.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		c.Logger.Info("handleAuthSignIn: starting sign in", "subject", principal.Subject)

		user, mErr := c.AuthService.SignIn(r.Context(), &orchestratorservice.SignInOpts{
			Principal: principal,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		c.Logger.Info("handleAuthSignIn: sign in succeeded", "user_id", user.ID)

		seedWorkspaceRuntime(c, r.Context(), user.ID, "sign_in")
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
			httpserver.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		user, mErr := c.AuthService.SignUp(r.Context(), &orchestratorservice.SignUpOpts{
			Principal: principal,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		seedWorkspaceRuntime(c, r.Context(), user.ID, "sign_up")
		httpserver.WriteNoContent(w)
	}
}

// DeleteAccount deletes the authenticated user's account.
// Restricted to non-production environments. In addition to soft-deleting the user,
// it also deletes any agent runtime CRs associated with the user's workspaces to prevent
// orphaned agent runtime resources in the cluster.
func DeleteAccount(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isProductionEnv(c.HTTPMiddleware.Environment) {
			WriteError(w, merrors.ErrAccountDeletionDisabled)
			return
		}

		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.AuthDeleteRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}

		workspaces := lookupUserWorkspaces(c, r.Context(), principal.Subject)

		if mErr := c.AuthService.Delete(r.Context(), &orchestratorservice.DeleteOpts{
			Principal:   principal,
			Reason:      reqBody.Reason,
			Description: reqBody.Description,
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		cleanupRuntimes(c, r.Context(), workspaces, principal.Subject)
		httpserver.WriteNoContent(w)
	}
}

// isProductionEnv reports whether env names a production deployment.
func isProductionEnv(raw string) bool {
	env := strings.ToLower(strings.TrimSpace(raw))
	return env == "production" || env == "prod"
}

// lookupUserWorkspaces fetches the caller's workspaces before account deletion
// so the runtime cleanup pass can remove them afterwards. Best-effort — any
// error is swallowed so the delete flow still proceeds.
func lookupUserWorkspaces(c *Context, ctx context.Context, subject string) []*orchestrator.Workspace {
	user, userErr := c.AuthService.GetBySubject(ctx, &orchestratorservice.GetUserBySubjectOpts{
		Subject: subject,
	})
	if userErr != nil || user == nil {
		return nil
	}
	workspaces, _ := c.WorkspaceService.ListByUserID(ctx, &orchestratorservice.ListWorkspacesOpts{
		UserID: user.ID,
	})
	return workspaces
}

// cleanupRuntimes best-effort deletes the runtime CR for every workspace the
// user owned before account deletion so we do not leak cluster resources.
func cleanupRuntimes(c *Context, ctx context.Context, workspaces []*orchestrator.Workspace, subject string) {
	if c.RuntimeClient == nil {
		return
	}
	for _, ws := range workspaces {
		deleteOneRuntime(c, ctx, ws.ID, subject)
	}
}

func deleteOneRuntime(c *Context, ctx context.Context, workspaceID, subject string) {
	if delErr := c.RuntimeClient.DeleteRuntime(ctx, workspaceID); delErr != nil {
		c.Logger.Warn("failed to delete agent runtime on account deletion",
			"workspace_id", workspaceID,
			"user", subject,
			"error", delErr,
		)
		return
	}
	c.Logger.Info("deleted agent runtime on account deletion",
		"workspace_id", workspaceID,
		"user", subject,
	)
}

// UserProfile retrieves the authenticated user's profile information.
// Returns user details including preferences, subscription status, and account state.
func UserProfile(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (*dto.UserProfileResponse, *merrors.Error) {
		return toUserProfileResponse(deps.User), nil
	})
}

// UpdateUser modifies the authenticated user's profile information.
// Supports updating nickname, name, surname, country code, date of birth, and preferences.
// Only provided fields are updated; omitted fields retain their current values.
func UpdateUser(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.UserUpdateRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}
		if msg := validateUserUpdate(&reqBody); msg != "" {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, msg)
			return
		}

		if _, mErr := c.AuthService.UpdateProfile(r.Context(), &orchestratorservice.UpdateProfileOpts{
			Principal:   principal,
			Nickname:    reqBody.Nickname,
			Name:        reqBody.Name,
			Surname:     reqBody.Surname,
			CountryCode: reqBody.CountryCode,
			DateOfBirth: userUpdateDateOfBirth(reqBody.DateOfBirth),
			Preferences: userUpdatePreferences(reqBody.Preferences),
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		httpserver.WriteNoContent(w)
	}
}

// maxProfileNameLength is the max byte length accepted for any single
// user-provided profile name field (nickname, name, surname).
const maxProfileNameLength = 64

// validateUserUpdate returns an empty string when the request body passes
// profile validation, otherwise the client-facing error message.
func validateUserUpdate(reqBody *dto.UserUpdateRequest) string {
	if msg := checkNameLen("nickname", reqBody.Nickname); msg != "" {
		return msg
	}
	if msg := checkNameLen("name", reqBody.Name); msg != "" {
		return msg
	}
	if msg := checkNameLen("surname", reqBody.Surname); msg != "" {
		return msg
	}
	return checkCountryCode(reqBody.CountryCode)
}

func checkNameLen(field string, value *string) string {
	if value == nil || len(*value) <= maxProfileNameLength {
		return ""
	}
	return field + " exceeds maximum allowed length"
}

func checkCountryCode(cc *string) string {
	if cc == nil || *cc == "" {
		return ""
	}
	v := *cc
	if len(v) != 2 || v[0] < 'A' || v[0] > 'Z' || v[1] < 'A' || v[1] > 'Z' {
		return "country_code must be a 2-letter uppercase ISO 3166-1 alpha-2 code"
	}
	return ""
}

func userUpdatePreferences(src *dto.UserUpdatePreferencesRequest) *orchestrator.UserPreferences {
	if src == nil {
		return nil
	}
	return &orchestrator.UserPreferences{
		PlatformTheme:    src.PlatformTheme,
		PlatformLanguage: src.PlatformLanguage,
		CurrencyCode:     src.CurrencyCode,
	}
}

func userUpdateDateOfBirth(src *dto.DateTime) *time.Time {
	if src == nil || src.IsZero() {
		return nil
	}
	v := src.UTC()
	return &v
}

// UserLegal retrieves the legal documents along with the user's acceptance status.
// Returns terms of service and privacy policy content and versions, plus whether
// the user has agreed to each document.
func UserLegal(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (*dto.UserLegalResponse, *merrors.Error) {
		legalDocuments, mErr := c.AuthService.GetLegalDocuments(r.Context())
		if mErr != nil {
			return nil, mErr
		}

		return &dto.UserLegalResponse{
			TermsOfService:             legalDocuments.TermsOfService,
			PrivacyPolicy:              legalDocuments.PrivacyPolicy,
			TermsOfServiceVersion:      legalDocuments.TermsOfServiceVersion,
			PrivacyPolicyVersion:       legalDocuments.PrivacyPolicyVersion,
			HasAgreedWithTerms:         deps.User.HasAgreedWithTerms,
			HasAgreedWithPrivacyPolicy: deps.User.HasAgreedWithPrivacyPolicy,
		}, nil
	})
}

// AcceptLegal records the user's acceptance of the specified legal document versions.
// The user must accept both terms of service and privacy policy versions to proceed.
// This updates the user's legal acceptance status in the database.
func AcceptLegal(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody dto.UserLegalAcceptRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}

		if _, mErr := c.AuthService.AcceptLegal(r.Context(), &orchestratorservice.AcceptLegalOpts{
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
	return AuthedHandlerNoContent(c, func(r *http.Request, deps *AuthedHandlerDeps) *merrors.Error {
		// Best-effort: clear push tokens so the device stops receiving notifications.
		_ = c.AuthService.ClearPushToken(r.Context(), &orchestratorservice.ClearPushTokenOpts{
			UserID: deps.User.ID,
		})
		return nil
	})
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
	workspaces, mErr := c.WorkspaceService.ListByUserID(ctx, &orchestratorservice.ListWorkspacesOpts{
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
