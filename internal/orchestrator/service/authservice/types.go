// Package authservice provides authentication and user management services for the Crawbl platform.
// It handles user sign-in, sign-up, profile management, legal document acceptance, and push token storage.
// The service implements a hybrid auth pattern where sign-in creates users on first access and
// sign-up returns existing users idempotently.
package authservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// service implements the orchestratorservice.AuthService interface.
// It coordinates user authentication, profile management, and workspace bootstrapping
// through its dependencies on userRepo and workspaceBootstrapper.
type service struct {
	// userRepo provides access to user persistence operations.
	userRepo userRepo
	// workspaceBootstrapper handles the creation of default workspaces for new users.
	workspaceBootstrapper workspaceBootstrapper
	// legalDocuments holds the current legal document URLs and versions.
	legalDocuments *legalDocumentsConfig
}

// legalDocumentsConfig holds the URLs and versions for legal documents
// that users must accept (Terms of Service and Privacy Policy).
type legalDocumentsConfig struct {
	// TermsOfService is the URL to the Terms of Service document.
	TermsOfService string
	// PrivacyPolicy is the URL to the Privacy Policy document.
	PrivacyPolicy string
	// TermsOfServiceVersion is the current version string for Terms of Service.
	TermsOfServiceVersion string
	// PrivacyPolicyVersion is the current version string for Privacy Policy.
	PrivacyPolicyVersion string
}

// userRepo defines the interface for user persistence operations.
// It abstracts the database layer and allows the authservice to work with
// various implementations (Postgres, in-memory for testing, etc.).
type userRepo interface {
	// GetBySubject retrieves a user by their authentication subject identifier.
	// Returns the user if found, or an error if the user does not exist.
	GetBySubject(ctx context.Context, sess orchestratorrepo.SessionRunner, subject string) (*orchestrator.User, *merrors.Error)
	// GetUser retrieves a user by subject or email, checking both identifiers.
	// This allows flexible user lookup during authentication flows.
	GetUser(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (*orchestrator.User, *merrors.Error)
	// CreateUser persists a new user entity with the provided options.
	// This includes creating the user record and handling legal agreement timestamps.
	CreateUser(ctx context.Context, opts *orchestratorrepo.CreateUserOpts) *merrors.Error
	// Save updates an existing user entity in the database.
	// This is used for profile updates and legal agreement changes.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) *merrors.Error
	// SavePushToken stores a push notification token for a user device.
	// This enables sending notifications to the user's mobile device.
	SavePushToken(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, pushToken string) *merrors.Error
	// ClearPushTokens removes all push notification tokens for a user.
	// Called on logout so the user stops receiving push notifications.
	ClearPushTokens(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) *merrors.Error
	// IsUserDeleted checks if a user with the given subject or email has been soft-deleted.
	// This prevents re-registration of deleted user accounts.
	IsUserDeleted(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (bool, *merrors.Error)
	// CheckNicknameExists verifies if a nickname is already in use.
	// This is used during user creation to ensure unique nicknames.
	CheckNicknameExists(ctx context.Context, sess orchestratorrepo.SessionRunner, nickname string) (bool, *merrors.Error)
}

// workspaceBootstrapper defines the interface for workspace initialization.
// It handles creating the default workspace for new users, which includes
// setting up the necessary agent runtime runtime and any default resources.
type workspaceBootstrapper interface {
	// EnsureDefaultWorkspace creates a default workspace for the specified user
	// if one does not already exist. This is called during user registration.
	EnsureDefaultWorkspace(ctx context.Context, opts *orchestratorservice.EnsureDefaultWorkspaceOpts) *merrors.Error
}
