// Package authservice provides authentication and user management services for the Crawbl platform.
// It handles user sign-in, sign-up, profile management, legal document acceptance, and push token storage.
// The service implements a hybrid auth pattern where sign-in creates users on first access and
// sign-up returns existing users idempotently.
package authservice

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new AuthService instance with the provided dependencies.
// It initializes the auth service with a user repository for persistence,
// a workspace bootstrapper for default workspace creation, and legal document configuration.
// The function panics if required dependencies are nil, as these are essential for operation.
//
// Parameters:
//   - userRepo: Repository for user persistence operations. Must not be nil.
//   - workspaceBootstrapper: Handler for creating default workspaces. Must not be nil.
//   - legalDocuments: Configuration for legal document URLs and versions. May be nil for defaults.
//
// Returns an AuthService implementation ready for use.
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
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}
	return s.signInOrUp(ctx, opts.Sess, opts.Principal, false)
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
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}
	return s.signInOrUp(ctx, opts.Sess, opts.Principal, true)
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
		// User found - check if deleted.
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
	return s.userRepo.GetBySubject(ctx, opts.Sess, opts.Subject)
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
//
//nolint:cyclop
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
//
//nolint:cyclop
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

	return database.WithTransaction(opts.Sess, "users accept legal", func(tx *dbr.Tx) (*orchestrator.User, *merrors.Error) {
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
	if opts == nil || opts.Sess == nil {
		return merrors.ErrInvalidInput
	}

	_, err := opts.Sess.DeleteFrom("user_push_tokens").
		Where("user_id = ?", opts.UserID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "clear push token")
	}

	return nil
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

	if mErr := s.userRepo.CreateUser(ctx, &orchestratorrepo.CreateUserOpts{
		Sess:               sess,
		User:               user,
		HasAgreedWithLegal: hasAgreedWithLegal,
	}); mErr != nil {
		return nil, mErr
	}

	// Ensure default workspace exists
	if mErr := s.workspaceBootstrapper.EnsureDefaultWorkspace(ctx, &orchestratorservice.EnsureDefaultWorkspaceOpts{
		Sess:   sess,
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
	const digits = "0123456789"

	for range maxAttempts {
		// Generate random 4-digit suffix
		var randNum string
		for range 4 {
			randNum += string(digits[rand.Intn(len(digits))])
		}
		nickname := emailPrefix + "#" + randNum

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
