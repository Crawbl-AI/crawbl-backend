package authservice

import (
	"context"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func New(userRepo userRepo, workspaceBootstrapper workspaceBootstrapper, legalDocuments *orchestrator.LegalDocuments) orchestratorservice.AuthService {
	if userRepo == nil {
		panic("auth service user repo cannot be nil")
	}
	if workspaceBootstrapper == nil {
		panic("auth service workspace bootstrapper cannot be nil")
	}

	return &service{
		userRepo:              userRepo,
		workspaceBootstrapper: workspaceBootstrapper,
		legalDocuments:        newLegalDocumentsConfig(legalDocuments),
	}
}

func (s *service) SignUp(ctx context.Context, opts *orchestratorservice.SignUpOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return nil, mErr
	}

	user, mErr := s.userRepo.GetBySubject(ctx, opts.Sess, principal.Subject)
	switch {
	case mErr == nil && user != nil:
		if user.DeletedAt != nil {
			return nil, merrors.ErrUserDeleted
		}

		user.Email = principal.Email
		user.Name = principal.Name
		user.UpdatedAt = time.Now().UTC()

		return database.WithTransaction(opts.Sess, "auth sign up existing user", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
			if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
				return nil, mErr
			}
			if mErr := s.workspaceBootstrapper.EnsureDefaultWorkspace(ctx, &orchestratorservice.EnsureDefaultWorkspaceOpts{
				Sess:   tx,
				UserID: user.ID,
			}); mErr != nil {
				return nil, mErr
			}
			return user, nil
		})
	case mErr != nil && !merrors.IsCode(mErr, merrors.ErrCodeUserNotFound):
		return nil, mErr
	}

	now := time.Now().UTC()
	user = &orchestrator.User{
		ID:                         uuid.NewString(),
		Subject:                    principal.Subject,
		Email:                      principal.Email,
		Name:                       principal.Name,
		CreatedAt:                  now,
		UpdatedAt:                  now,
		HasAgreedWithTerms:         false,
		HasAgreedWithPrivacyPolicy: false,
	}

	return database.WithTransaction(opts.Sess, "auth sign up", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
		if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
			return nil, mErr
		}
		if mErr := s.workspaceBootstrapper.EnsureDefaultWorkspace(ctx, &orchestratorservice.EnsureDefaultWorkspaceOpts{
			Sess:   tx,
			UserID: user.ID,
		}); mErr != nil {
			return nil, mErr
		}
		return user, nil
	})
}

func (s *service) SignIn(ctx context.Context, opts *orchestratorservice.SignInOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return nil, mErr
	}

	user, mErr := s.userRepo.GetBySubject(ctx, opts.Sess, principal.Subject)
	if mErr != nil {
		return nil, mErr
	}
	if user.DeletedAt != nil {
		return nil, merrors.ErrUserDeleted
	}

	user.Email = principal.Email
	user.Name = principal.Name
	user.UpdatedAt = time.Now().UTC()

	return database.WithTransaction(opts.Sess, "auth sign in", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
		if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
			return nil, mErr
		}
		if mErr := s.workspaceBootstrapper.EnsureDefaultWorkspace(ctx, &orchestratorservice.EnsureDefaultWorkspaceOpts{
			Sess:   tx,
			UserID: user.ID,
		}); mErr != nil {
			return nil, mErr
		}
		return user, nil
	})
}

func (s *service) Delete(ctx context.Context, opts *orchestratorservice.DeleteOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil {
		return merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return mErr
	}

	user, mErr := s.userRepo.GetBySubject(ctx, opts.Sess, principal.Subject)
	if mErr != nil {
		return mErr
	}

	now := time.Now().UTC()
	user.DeletedAt = &now
	user.UpdatedAt = now

	return database.WithTransactionNoResult(opts.Sess, "auth delete", func(tx *dbr.Tx) *merrors.Error {
		return s.userRepo.Save(ctx, tx, user)
	})
}

func (s *service) GetBySubject(ctx context.Context, opts *orchestratorservice.GetUserBySubjectOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	return s.userRepo.GetBySubject(ctx, opts.Sess, opts.Subject)
}

func (s *service) UpdateProfile(ctx context.Context, opts *orchestratorservice.UpdateProfileOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return nil, mErr
	}

	user, mErr := s.userRepo.GetBySubject(ctx, opts.Sess, principal.Subject)
	if mErr != nil {
		return nil, mErr
	}
	if user.DeletedAt != nil {
		return nil, merrors.ErrUserDeleted
	}

	if opts.Nickname != nil {
		user.Nickname = strings.TrimSpace(*opts.Nickname)
	}
	if opts.Name != nil {
		user.Name = strings.TrimSpace(*opts.Name)
	}
	if opts.Surname != nil {
		user.Surname = strings.TrimSpace(*opts.Surname)
	}
	if opts.CountryCode != nil {
		countryCode := strings.TrimSpace(*opts.CountryCode)
		if countryCode == "" {
			user.CountryCode = nil
		} else {
			user.CountryCode = &countryCode
		}
	}
	if opts.DateOfBirth != nil {
		dob := opts.DateOfBirth.UTC()
		user.DateOfBirth = &dob
	}
	if opts.Preferences != nil {
		user.Preferences.PlatformTheme = opts.Preferences.PlatformTheme
		user.Preferences.PlatformLanguage = opts.Preferences.PlatformLanguage
		user.Preferences.CurrencyCode = opts.Preferences.CurrencyCode
	}
	user.UpdatedAt = time.Now().UTC()

	return database.WithTransaction(opts.Sess, "users update profile", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
		if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
			return nil, mErr
		}
		return user, nil
	})
}

func (s *service) GetLegalDocuments(_ context.Context) (*orchestrator.LegalDocuments, *merrors.Error) {
	return &orchestrator.LegalDocuments{
		TermsOfService:        s.legalDocuments.TermsOfService,
		PrivacyPolicy:         s.legalDocuments.PrivacyPolicy,
		TermsOfServiceVersion: s.legalDocuments.TermsOfServiceVersion,
		PrivacyPolicyVersion:  s.legalDocuments.PrivacyPolicyVersion,
	}, nil
}

func (s *service) AcceptLegal(ctx context.Context, opts *orchestratorservice.AcceptLegalOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return nil, mErr
	}

	user, mErr := s.userRepo.GetBySubject(ctx, opts.Sess, principal.Subject)
	if mErr != nil {
		return nil, mErr
	}
	if user.DeletedAt != nil {
		return nil, merrors.ErrUserDeleted
	}

	if opts.TermsOfServiceVersion != nil && strings.TrimSpace(*opts.TermsOfServiceVersion) != "" {
		if strings.TrimSpace(*opts.TermsOfServiceVersion) != s.legalDocuments.TermsOfServiceVersion {
			return nil, merrors.NewBusinessError("Legal document version does not match current version", "USR0012")
		}
		user.HasAgreedWithTerms = true
	}
	if opts.PrivacyPolicyVersion != nil && strings.TrimSpace(*opts.PrivacyPolicyVersion) != "" {
		if strings.TrimSpace(*opts.PrivacyPolicyVersion) != s.legalDocuments.PrivacyPolicyVersion {
			return nil, merrors.NewBusinessError("Legal document version does not match current version", "USR0012")
		}
		user.HasAgreedWithPrivacyPolicy = true
	}
	user.UpdatedAt = time.Now().UTC()

	return database.WithTransaction(opts.Sess, "users accept legal", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
		if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
			return nil, mErr
		}
		return user, nil
	})
}

func (s *service) SavePushToken(ctx context.Context, opts *orchestratorservice.SavePushTokenOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil {
		return merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return mErr
	}

	user, mErr := s.userRepo.GetBySubject(ctx, opts.Sess, principal.Subject)
	if mErr != nil {
		return mErr
	}
	if user.DeletedAt != nil {
		return merrors.ErrUserDeleted
	}

	return database.WithTransactionNoResult(opts.Sess, "users save push token", func(tx *dbr.Tx) *merrors.Error {
		return s.userRepo.SavePushToken(ctx, tx, user.ID, strings.TrimSpace(opts.PushToken))
	})
}

func validatePrincipal(principal *orchestrator.Principal) (*orchestrator.Principal, *merrors.Error) {
	if principal == nil {
		return nil, merrors.ErrNilPrincipal
	}
	if principal.Subject == "" {
		return nil, merrors.ErrEmptySubject
	}
	if principal.Email == "" {
		return nil, merrors.ErrEmptyEmail
	}
	return principal, nil
}

func newLegalDocumentsConfig(input *orchestrator.LegalDocuments) *legalDocumentsConfig {
	config := &legalDocumentsConfig{
		TermsOfService:        "https://crawbl.com/terms",
		PrivacyPolicy:         "https://crawbl.com/privacy",
		TermsOfServiceVersion: "v1",
		PrivacyPolicyVersion:  "v1",
	}
	if input == nil {
		return config
	}
	if input.TermsOfService != "" {
		config.TermsOfService = input.TermsOfService
	}
	if input.PrivacyPolicy != "" {
		config.PrivacyPolicy = input.PrivacyPolicy
	}
	if input.TermsOfServiceVersion != "" {
		config.TermsOfServiceVersion = input.TermsOfServiceVersion
	}
	if input.PrivacyPolicyVersion != "" {
		config.PrivacyPolicyVersion = input.PrivacyPolicyVersion
	}
	return config
}
