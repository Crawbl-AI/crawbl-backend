package server

import (
	"context"
	"net/http"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

func (s *Server) handleHealthCheck(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteSuccessResponse(w, http.StatusOK, &healthCheckResponse{
		Online:  true,
		Version: "dev",
	})
}

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

func (s *Server) handleAuthSignIn(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	user, mErr := s.authService.SignIn(r.Context(), &orchestratorservice.SignInOpts{
		Sess:      s.newSession(),
		Principal: principal,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	s.seedWorkspaceRuntime(r.Context(), user.ID, "sign_in")
	httpserver.WriteNoContent(w)
}

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

func (s *Server) handleAuthDelete(w http.ResponseWriter, r *http.Request) {
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

	if mErr := s.authService.Delete(r.Context(), &orchestratorservice.DeleteOpts{
		Sess:        s.newSession(),
		Principal:   principal,
		Reason:      reqBody.Reason,
		Description: reqBody.Description,
	}); mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteNoContent(w)
}

func (s *Server) handleUsersProfile(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, toUserProfileResponse(user))
}

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
	if reqBody.DateOfBirth != nil && !reqBody.DateOfBirth.Time.IsZero() {
		value := reqBody.DateOfBirth.Time.UTC()
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

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
