package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/httputil"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/convert"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// HealthCheck returns the server health status and version.
// This endpoint is unauthenticated and used by load balancers and monitoring
// systems to verify the server is responsive.
func HealthCheck(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteProtoSuccess(w, http.StatusOK, &mobilev1.HealthCheckResponse{
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

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.LegalResponse{
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
			httputil.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		const minFCMTokenLength = 32
		const maxFCMTokenLength = 4096

		var reqBody mobilev1.SavePushTokenRequest
		if err := DecodeProtoJSON(r, &reqBody); err != nil || reqBody.GetPushToken() == "" {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}
		if len(reqBody.GetPushToken()) < minFCMTokenLength {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, "push token is too short")
			return
		}
		if len(reqBody.GetPushToken()) > maxFCMTokenLength {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, "push token exceeds maximum allowed length")
			return
		}

		if mErr := c.AuthService.SavePushToken(r.Context(), &orchestratorservice.SavePushTokenOpts{
			Principal: principal,
			PushToken: reqBody.GetPushToken(),
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.SavePushTokenResponse{Success: true})
	}
}

// SignIn authenticates an existing user via Firebase token.
// If the user exists in the system, it seeds their workspace runtime for the session.
// Returns 204 No Content on success, indicating the user is now authenticated.
func SignIn(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httputil.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
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
		httputil.WriteNoContent(w)
	}
}

// SignUp creates a new user account from the Firebase authentication token.
// After successful registration, it seeds the user's default workspace runtime.
// Returns 204 No Content on success.
func SignUp(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httputil.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
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
		httputil.WriteNoContent(w)
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
			httputil.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody mobilev1.AuthDeleteRequest
		if err := DecodeProtoJSON(r, &reqBody); err != nil {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}

		workspaces := lookupUserWorkspaces(c, r, principal.Subject)

		if mErr := c.AuthService.Delete(r.Context(), &orchestratorservice.DeleteOpts{
			Principal:   principal,
			Reason:      reqBody.GetReason(),
			Description: reqBody.GetDescription(),
		}); mErr != nil {
			WriteError(w, mErr)
			return
		}

		cleanupRuntimes(c, r, workspaces, principal.Subject)
		httputil.WriteNoContent(w)
	}
}

// lookupUserWorkspaces fetches the user's workspaces so runtime CRs can be
// cleaned up after deletion. Returns nil when the user cannot be resolved.
func lookupUserWorkspaces(c *Context, r *http.Request, subject string) []*orchestrator.Workspace {
	user, userErr := c.AuthService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
		Subject: subject,
	})
	if userErr != nil || user == nil {
		return nil
	}
	workspaces, _ := c.WorkspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
		UserID: user.ID,
	})
	return workspaces
}

// cleanupRuntimes performs best-effort deletion of agent runtime CRs for each
// workspace. Errors are logged but do not fail the request.
func cleanupRuntimes(c *Context, r *http.Request, workspaces []*orchestrator.Workspace, subject string) {
	if c.RuntimeClient == nil {
		return
	}
	for _, ws := range workspaces {
		if delErr := c.RuntimeClient.DeleteRuntime(r.Context(), ws.ID); delErr != nil {
			c.Logger.Warn("failed to delete agent runtime on account deletion",
				"workspace_id", ws.ID,
				"user", subject,
				"error", delErr,
			)
			continue
		}
		c.Logger.Info("deleted agent runtime on account deletion",
			"workspace_id", ws.ID,
			"user", subject,
		)
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
		WriteProtoSuccess(w, http.StatusOK, convert.UserProfileToProto(user))
	}
}

// UpdateUser modifies the authenticated user's profile information.
// Supports updating nickname, name, surname, country code, date of birth, and preferences.
// Only provided fields are updated; omitted fields retain their current values.
func UpdateUser(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httputil.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody mobilev1.UserUpdateRequest
		if err := DecodeProtoJSON(r, &reqBody); err != nil {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}

		if errMsg := validateUserUpdateProtoFields(&reqBody); errMsg != "" {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, errMsg)
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

		dateOfBirth, parseErr := parseDateOfBirth(reqBody.DateOfBirth)
		if parseErr != nil {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, "invalid date_of_birth format")
			return
		}

		if _, mErr := c.AuthService.UpdateProfile(r.Context(), &orchestratorservice.UpdateProfileOpts{
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

		httputil.WriteNoContent(w)
	}
}

// validateUserUpdateProtoFields checks profile field lengths and formats.
// Returns an error message string on failure, or "" on success.
func validateUserUpdateProtoFields(req *mobilev1.UserUpdateRequest) string {
	const maxProfileNameLength = 64
	if req.Nickname != nil && len(*req.Nickname) > maxProfileNameLength {
		return "nickname exceeds maximum allowed length"
	}
	if req.Name != nil && len(*req.Name) > maxProfileNameLength {
		return "name exceeds maximum allowed length"
	}
	if req.Surname != nil && len(*req.Surname) > maxProfileNameLength {
		return "surname exceeds maximum allowed length"
	}
	if req.CountryCode != nil && *req.CountryCode != "" {
		cc := *req.CountryCode
		if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
			return "country_code must be a 2-letter uppercase ISO 3166-1 alpha-2 code"
		}
	}
	return ""
}

// parseDateOfBirth parses an optional date string in RFC3339 or millisecond format.
// Returns (nil, nil) when the input is nil or empty.
func parseDateOfBirth(raw *string) (*time.Time, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		parsed, err = time.Parse("2006-01-02T15:04:05.000", *raw)
		if err != nil {
			return nil, err
		}
	}
	utc := parsed.UTC()
	return &utc, nil
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

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.UserLegalResponse{
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
			httputil.WriteErrorMessage(w, http.StatusUnauthorized, err.Error())
			return
		}

		var reqBody mobilev1.UserLegalAcceptRequest
		if err := DecodeProtoJSON(r, &reqBody); err != nil {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, errInvalidRequestBody)
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

		httputil.WriteNoContent(w)
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
