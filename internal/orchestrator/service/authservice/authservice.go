// Package authservice provides authentication and user management services for the Crawbl platform.
// It handles user sign-in, sign-up, profile management, legal document acceptance, and push token storage.
// The service implements a hybrid auth pattern where sign-in creates users on first access and
// sign-up returns existing users idempotently.
package authservice

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagequotarepo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new AuthService instance with the provided dependencies.
// It initializes the auth service with a user repository for persistence,
// a workspace bootstrapper for default workspace creation, and legal document configuration.
// Returns an error if any required dependency is nil.
//
// Parameters:
//   - userRepo: Repository for user persistence operations. Must not be nil.
//   - workspaceBootstrapper: Handler for creating default workspaces. Must not be nil.
//   - legalDocuments: Configuration for legal document URLs and versions. May be nil for defaults.
//   - usageQuotaRepo: Repository for usage quota creation. Must not be nil.
//
// Returns an AuthService implementation and nil error on success.
func New(userRepo userStore, workspaceBootstrapper orchestratorservice.WorkspaceBootstrapper, legalDocuments *orchestrator.LegalDocuments, usageQuotaRepo usageQuotaCreator) (orchestratorservice.AuthService, error) {
	if userRepo == nil {
		return nil, errors.New("authservice: userRepo is required")
	}
	if workspaceBootstrapper == nil {
		return nil, errors.New("authservice: workspaceBootstrapper is required")
	}
	if usageQuotaRepo == nil {
		return nil, errors.New("authservice: usageQuotaRepo is required")
	}

	return &service{
		userRepo:              userRepo,
		workspaceBootstrapper: workspaceBootstrapper,
		legalDocuments:        newLegalDocumentsConfig(legalDocuments),
		usageQuotaRepo:        usageQuotaRepo,
	}, nil
}

// MustNew creates a new AuthService or panics if any required dependency is nil.
// Use in main/wiring only; prefer New in code that can propagate errors.
func MustNew(userRepo userStore, workspaceBootstrapper orchestratorservice.WorkspaceBootstrapper, legalDocuments *orchestrator.LegalDocuments, usageQuotaRepo usageQuotaCreator) orchestratorservice.AuthService {
	s, err := New(userRepo, workspaceBootstrapper, legalDocuments, usageQuotaRepo)
	if err != nil {
		panic(err)
	}
	return s
}

// SignIn authenticates a user. If the user doesn't exist, it creates one.
// This follows the Skatts hybrid auth pattern where sign-in creates users on first access.
// Users created via SignIn do not have legal agreements pre-accepted.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Sign-in options containing the session and principal (subject, email, name).
//
// Returns the authenticated user or an error. Possible errors include:
//   - ErrInvalidInput: nil options or session
//   - ErrNilPrincipal: nil principal in options
//   - ErrEmptySubject: empty subject in principal
//   - ErrEmptyEmail: empty email in principal
//   - ErrUserDeleted: user account has been soft-deleted
//   - ErrUserFirebaseUIDMismatch: Firebase UID mismatch detected
func (s *service) SignIn(ctx context.Context, opts *orchestratorservice.SignInOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)
	return s.signInOrUp(ctx, sess, opts.Principal, false)
}

// SignUp registers a new user. If the user already exists, returns the existing user.
// This follows the Skatts hybrid auth pattern where sign-up returns existing users too.
// Users created via SignUp have legal agreements marked as accepted (hasAgreedWithLegal=true).
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Sign-up options containing the session and principal (subject, email, name).
//
// Returns the created or existing user, or an error. Possible errors include:
//   - ErrInvalidInput: nil options or session
//   - ErrNilPrincipal: nil principal in options
//   - ErrEmptySubject: empty subject in principal
//   - ErrEmptyEmail: empty email in principal
//   - ErrUserDeleted: user account has been soft-deleted
//   - ErrUserFirebaseUIDMismatch: Firebase UID mismatch detected
func (s *service) SignUp(ctx context.Context, opts *orchestratorservice.SignUpOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)
	return s.signInOrUp(ctx, sess, opts.Principal, true)
}

// signInOrUp implements the shared authentication flow for both SignIn and SignUp.
// It validates the principal, looks up the user by email and subject, handles deleted
// accounts, refreshes info on existing users, and creates a new user when not found.
//
// The requireLegalConsent parameter controls whether a newly created user has legal
// agreements pre-accepted:
//   - false (SignIn): user is created without pre-accepted legal agreements
//   - true (SignUp): user is created with HasAgreedWithTerms and HasAgreedWithPrivacyPolicy set
//
// Possible errors include:
//   - ErrNilPrincipal / ErrEmptySubject / ErrEmptyEmail: invalid principal
//   - ErrUserDeleted: account has been soft-deleted
//   - ErrUserFirebaseUIDMismatch: Firebase UID collision detected
func (s *service) signInOrUp(ctx context.Context, sess *dbr.Session, rawPrincipal *orchestrator.Principal, requireLegalConsent bool) (*orchestrator.User, *merrors.Error) {
	principal, mErr := validatePrincipal(rawPrincipal)
	if mErr != nil {
		return nil, mErr
	}

	// Try to find user by email first, then by subject.
	user, mErr := s.userRepo.GetUser(ctx, sess, principal.Subject, principal.Email)
	if mErr == nil && user != nil {
		// User found - check if banned or deleted.
		if user.IsBanned {
			return nil, merrors.ErrUserBanned
		}
		if user.DeletedAt != nil {
			return nil, merrors.ErrUserDeleted
		}
		// Refresh mutable identity fields and persist.
		user.Email = principal.Email
		user.Name = principal.Name
		user.UpdatedAt = time.Now().UTC()
		if mErr := s.saveUser(ctx, sess, user); mErr != nil {
			return nil, mErr
		}
		return user, nil
	}

	// Handle Firebase UID mismatch before the not-found branch.
	if merrors.IsErrUserWrongFirebaseUID(mErr) {
		return nil, merrors.ErrUserFirebaseUIDMismatch
	}

	// User not found - check whether a soft-deleted record exists.
	if merrors.IsErrUserNotFound(mErr) {
		isDeleted, derr := s.userRepo.IsUserDeleted(ctx, sess, principal.Subject, principal.Email)
		if derr != nil {
			return nil, merrors.WrapServerError(derr, "check deleted user state")
		}
		if isDeleted {
			return nil, merrors.ErrUserDeleted
		}

		return s.createUser(ctx, sess, principal, requireLegalConsent)
	}

	return nil, merrors.WrapServerError(mErr, "get user from database")
}

// Delete soft-deletes a user account by setting the DeletedAt timestamp.
// The user is marked as deleted but remains in the database for audit purposes.
// Deleted users cannot sign in or sign up again with the same credentials.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Delete options containing the session and principal.
//
// Returns an error if the operation fails. Possible errors include:
//   - ErrInvalidInput: nil options or session
//   - ErrNilPrincipal: nil principal in options
//   - ErrEmptySubject: empty subject in principal
//   - ErrEmptyEmail: empty email in principal
//   - Errors from user repository if lookup fails
func (s *service) Delete(ctx context.Context, opts *orchestratorservice.DeleteOpts) *merrors.Error {
	if opts == nil {
		return merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return mErr
	}

	sess := database.SessionFromContext(ctx)
	user, mErr := s.userRepo.GetBySubject(ctx, sess, principal.Subject)
	if mErr != nil {
		return mErr
	}

	now := time.Now().UTC()
	user.DeletedAt = &now
	user.UpdatedAt = now

	return database.WithTransactionNoResult(sess, "auth delete", func(tx *dbr.Tx) *merrors.Error {
		return s.userRepo.Save(ctx, tx, user)
	})
}

// GetBySubject retrieves a user by their authentication subject identifier.
// This is used to look up the current authenticated user's profile.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Options containing the session and subject identifier.
//
// Returns the user entity or an error if not found.
func (s *service) GetBySubject(ctx context.Context, opts *orchestratorservice.GetUserBySubjectOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)
	return s.userRepo.GetBySubject(ctx, sess, opts.Subject)
}

// UpdateProfile updates a user's profile information.
// This includes nickname, name, surname, country code, date of birth, and preferences.
// The method validates the principal, ensures the user is not deleted, and applies
// updates in a transaction.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Update options containing session, principal, and fields to update.
//
// Returns the updated user entity or an error. Possible errors include:
//   - ErrInvalidInput: nil options or session
//   - ErrNilPrincipal: nil principal in options
//   - ErrUserDeleted: user account has been soft-deleted
func (s *service) UpdateProfile(ctx context.Context, opts *orchestratorservice.UpdateProfileOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return nil, mErr
	}

	sess := database.SessionFromContext(ctx)
	user, mErr := s.userRepo.GetBySubject(ctx, sess, principal.Subject)
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

	return database.WithTransaction(sess, "users update profile", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
		if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
			return nil, mErr
		}
		return user, nil
	})
}

// GetLegalDocuments returns the current legal documents configuration.
// This includes URLs and versions for Terms of Service and Privacy Policy.
// The method returns static configuration and does not require database access.
//
// Parameters:
//   - ctx: Context for the operation (unused, but kept for interface consistency).
//
// Returns the legal documents configuration or an error.
func (s *service) GetLegalDocuments(_ context.Context) (*orchestrator.LegalDocuments, *merrors.Error) {
	return &orchestrator.LegalDocuments{
		TermsOfService:        s.legalDocuments.TermsOfService,
		PrivacyPolicy:         s.legalDocuments.PrivacyPolicy,
		TermsOfServiceVersion: s.legalDocuments.TermsOfServiceVersion,
		PrivacyPolicyVersion:  s.legalDocuments.PrivacyPolicyVersion,
	}, nil
}

// AcceptLegal records a user's acceptance of legal documents.
// The user must accept the current version of Terms of Service and/or Privacy Policy.
// If the provided version doesn't match the current version, an error is returned.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Options containing session, principal, and version strings to accept.
//
// Returns the updated user entity or an error. Possible errors include:
//   - ErrInvalidInput: nil options or session
//   - ErrNilPrincipal: nil principal in options
//   - ErrUserDeleted: user account has been soft-deleted
//   - Business error if version doesn't match current version
func (s *service) AcceptLegal(ctx context.Context, opts *orchestratorservice.AcceptLegalOpts) (*orchestrator.User, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return nil, mErr
	}

	sess := database.SessionFromContext(ctx)
	user, mErr := s.userRepo.GetBySubject(ctx, sess, principal.Subject)
	if mErr != nil {
		return nil, mErr
	}
	if user.DeletedAt != nil {
		return nil, merrors.ErrUserDeleted
	}

	if opts.TermsOfServiceVersion != nil && strings.TrimSpace(*opts.TermsOfServiceVersion) != "" {
		if strings.TrimSpace(*opts.TermsOfServiceVersion) != s.legalDocuments.TermsOfServiceVersion {
			return nil, merrors.ErrLegalVersionMismatch
		}
		user.HasAgreedWithTerms = true
	}
	if opts.PrivacyPolicyVersion != nil && strings.TrimSpace(*opts.PrivacyPolicyVersion) != "" {
		if strings.TrimSpace(*opts.PrivacyPolicyVersion) != s.legalDocuments.PrivacyPolicyVersion {
			return nil, merrors.ErrLegalVersionMismatch
		}
		user.HasAgreedWithPrivacyPolicy = true
	}
	user.UpdatedAt = time.Now().UTC()

	return database.WithTransaction(sess, "users accept legal", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
		if mErr := s.userRepo.Save(ctx, tx, user); mErr != nil {
			return nil, mErr
		}
		return user, nil
	})
}

// SavePushToken stores a Firebase Cloud Messaging push token for a user device.
// This enables sending push notifications to the user's mobile device.
// The token is associated with the authenticated user.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Options containing session, principal, and the push token string.
//
// Returns an error if the operation fails. Possible errors include:
//   - ErrInvalidInput: nil options or session
//   - ErrNilPrincipal: nil principal in options
//   - ErrUserDeleted: user account has been soft-deleted
func (s *service) SavePushToken(ctx context.Context, opts *orchestratorservice.SavePushTokenOpts) *merrors.Error {
	if opts == nil {
		return merrors.ErrInvalidInput
	}

	principal, mErr := validatePrincipal(opts.Principal)
	if mErr != nil {
		return mErr
	}

	sess := database.SessionFromContext(ctx)
	user, mErr := s.userRepo.GetBySubject(ctx, sess, principal.Subject)
	if mErr != nil {
		return mErr
	}
	if user.DeletedAt != nil {
		return merrors.ErrUserDeleted
	}

	return database.WithTransactionNoResult(sess, "users save push token", func(tx *dbr.Tx) *merrors.Error {
		return s.userRepo.SavePushToken(ctx, tx, user.ID, strings.TrimSpace(opts.PushToken))
	})
}

// ClearPushToken removes all push notification tokens for a user.
// This is called on logout so the user stops receiving push notifications.
// The operation is best-effort — callers may safely ignore the returned error.
//
// Parameters:
//   - ctx: Context for the operation.
//   - opts: Options containing the session and user ID.
//
// Returns an error if the operation fails. Possible errors include:
//   - ErrInvalidInput: nil options or session
func (s *service) ClearPushToken(ctx context.Context, opts *orchestratorservice.ClearPushTokenOpts) *merrors.Error {
	if opts == nil {
		return merrors.ErrInvalidInput
	}

	sess := database.SessionFromContext(ctx)
	return s.userRepo.ClearPushTokens(ctx, sess, opts.UserID)
}

// createUser creates a new user with an auto-generated unique nickname.
// This is an internal helper method used by SignIn and SignUp.
//
// Parameters:
//   - ctx: Context for the operation.
//   - sess: Database session for the operation.
//   - principal: User identity information from authentication provider.
//   - hasAgreedWithLegal: Whether legal documents are considered accepted (true for SignUp, false for SignIn).
//
// Returns the created user entity or an error. The method also ensures
// a default workspace is created for the new user.
func (s *service) createUser(ctx context.Context, sess *dbr.Session, principal *orchestrator.Principal, hasAgreedWithLegal bool) (*orchestrator.User, *merrors.Error) {
	// Generate unique nickname
	nickname, mErr := s.generateUniqueNickname(ctx, sess, principal.Email)
	if mErr != nil {
		return nil, merrors.WrapServerError(mErr, "generate unique nickname")
	}

	now := time.Now().UTC()
	user := &orchestrator.User{
		ID:                         uuid.NewString(),
		Subject:                    principal.Subject,
		Email:                      principal.Email,
		Name:                       principal.Name,
		Nickname:                   nickname,
		CreatedAt:                  now,
		UpdatedAt:                  now,
		HasAgreedWithTerms:         hasAgreedWithLegal,
		HasAgreedWithPrivacyPolicy: hasAgreedWithLegal,
	}

	if mErr := database.WithTransactionNoResult(sess, "create user", func(tx *dbr.Tx) *merrors.Error {
		return s.userRepo.CreateUser(ctx, &orchestratorrepo.CreateUserOpts{
			Sess:               tx,
			User:               user,
			HasAgreedWithLegal: hasAgreedWithLegal,
		})
	}); mErr != nil {
		return nil, mErr
	}

	// Assign the free usage plan to the new user so quota enforcement is active from day one.
	if qErr := s.usageQuotaRepo.Create(ctx, sess, &usagequotarepo.Row{
		UserID:      user.ID,
		PlanID:      "free",
		EffectiveAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); qErr != nil {
		slog.Warn("failed to assign free usage plan", "user_id", user.ID, "error", qErr.Error())
		// Non-fatal: user can still use the platform, quota enforcement will be a no-op.
	}

	// Ensure default workspace exists
	if mErr := s.workspaceBootstrapper.EnsureDefaultWorkspace(ctx, &orchestratorservice.EnsureDefaultWorkspaceOpts{
		UserID: user.ID,
	}); mErr != nil {
		return nil, mErr
	}

	return user, nil
}

// generateUniqueNickname generates a unique nickname in the format: email_prefix#random4digits.
// The nickname is composed of the email's local part (before @) followed by a hash symbol
// and a random 4-digit suffix to ensure uniqueness.
//
// Parameters:
//   - ctx: Context for the operation.
//   - sess: Database session for checking nickname uniqueness.
//   - email: User's email address to extract the prefix from.
//
// Returns a unique nickname or an error if generation fails after max attempts.
func (s *service) generateUniqueNickname(ctx context.Context, sess *dbr.Session, email string) (string, *merrors.Error) {
	if sess == nil || email == "" {
		return "", merrors.ErrInvalidInput
	}

	// Extract email prefix (part before @)
	idx := strings.Index(email, "@")
	emailPrefix := email
	if idx > 0 {
		emailPrefix = email[:idx]
	}

	// Try to generate a unique nickname
	const maxAttempts = 5

	for range maxAttempts {
		// Generate cryptographically random 4-digit suffix (0000–9999)
		const nicknameSuffixRange = 10000
		n, err := rand.Int(rand.Reader, big.NewInt(nicknameSuffixRange))
		if err != nil {
			return "", merrors.WrapStdServerError(err, "generate random nickname suffix")
		}
		nickname := emailPrefix + "#" + fmt.Sprintf("%04d", n.Int64())

		exists, mErr := s.userRepo.CheckNicknameExists(ctx, sess, nickname)
		if mErr != nil {
			return "", merrors.WrapServerError(mErr, "check nickname exists")
		}
		if !exists {
			return nickname, nil
		}
	}

	return "", merrors.ErrNicknameGenerationFailed
}

// saveUser persists user changes within a transaction.
// This is a helper method used to wrap user save operations.
//
// Parameters:
//   - ctx: Context for the operation.
//   - sess: Database session for the operation.
//   - user: User entity to save.
//
// Returns an error if the save operation fails.
func (s *service) saveUser(ctx context.Context, sess *dbr.Session, user *orchestrator.User) *merrors.Error {
	return database.WithTransactionNoResult(sess, "save user", func(tx *dbr.Tx) *merrors.Error {
		return s.userRepo.Save(ctx, tx, user)
	})
}

// validatePrincipal validates that the principal contains required authentication information.
// It ensures subject and email are present and non-empty.
//
// Parameters:
//   - principal: The principal to validate.
//
// Returns the validated principal or an error if validation fails.
// Possible errors include:
//   - ErrNilPrincipal: principal is nil
//   - ErrEmptySubject: subject is empty
//   - ErrEmptyEmail: email is empty
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

// newLegalDocumentsConfig creates a legalDocumentsConfig with defaults applied.
// If input values are provided, they override the defaults.
// Default URLs point to the Crawbl platform's legal documents.
//
// Parameters:
//   - input: Optional configuration to override defaults. May be nil.
//
// Returns a fully populated legalDocumentsConfig.
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
