package userrepo

import (
	"context"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// userRepo is the PostgreSQL implementation of the UserRepo interface.
// It handles user data persistence and retrieval operations.
type userRepo struct{}

// New creates a new UserRepo instance backed by PostgreSQL.
// The returned repository uses the database session runner pattern for transaction support.
func New() *userRepo {
	return &userRepo{}
}

// GetBySubject retrieves a user by their Firebase authentication subject (UID).
// Returns ErrUserNotFound if no user exists with the given subject.
// Returns ErrInvalidInput if sess is nil or subject is empty.
func (r *userRepo) GetBySubject(ctx context.Context, sess orchestratorrepo.SessionRunner, subject string) (*orchestrator.User, *merrors.Error) {
	if sess == nil || strings.TrimSpace(subject) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var userRow orchestratorrepo.UserRow
	err := sess.Select(orchestratorrepo.Columns(userColumns...)...).
		From("users").
		Where("subject = ?", subject).
		LoadOneContext(ctx, &userRow)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrUserNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select user by subject")
	}

	user := userRow.ToDomain()
	r.loadPreferences(ctx, sess, user)

	return user, nil
}

// GetUser retrieves a user by email first (preferred), then by subject as fallback.
// If a user is found by email but the subject doesn't match, returns ErrUserWrongFirebaseUID.
// This allows detecting Firebase UID changes or account takeover attempts.
// Returns ErrUserNotFound if no user is found by either email or subject.
// Returns ErrInvalidInput if sess is nil and both subject and email are empty.
//
//nolint:cyclop
func (r *userRepo) GetUser(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (*orchestrator.User, *merrors.Error) {
	if sess == nil || (strings.TrimSpace(subject) == "" && strings.TrimSpace(email) == "") {
		return nil, merrors.ErrInvalidInput
	}

	var userRow orchestratorrepo.UserRow

	// Prefer lookup by email (unique), so we can detect subject mismatches
	if email != "" {
		err := sess.Select(orchestratorrepo.Columns(userColumns...)...).
			From("users").
			Where("email = ?", email).
			LoadOneContext(ctx, &userRow)
		if err == nil {
			// Found by email — verify the subject if caller provided one
			if subject != "" && userRow.Subject != subject {
				return nil, merrors.ErrUserWrongFirebaseUID
			}
			user := userRow.ToDomain()
			r.loadPreferences(ctx, sess, user)
			return user, nil
		}
		if !database.IsRecordNotFoundError(err) {
			return nil, merrors.WrapStdServerError(err, "select user by email")
		}
		// Not found by email — continue to subject lookup
	}

	// Fallback: lookup by subject
	if subject != "" {
		err := sess.Select(orchestratorrepo.Columns(userColumns...)...).
			From("users").
			Where("subject = ?", subject).
			LoadOneContext(ctx, &userRow)
		if err == nil {
			user := userRow.ToDomain()
			r.loadPreferences(ctx, sess, user)
			return user, nil
		}
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrUserNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select user by subject")
	}

	return nil, merrors.ErrUserNotFound
}

// CreateUser creates a new user with the specified legal agreement status.
// It inserts both the user record and the associated user preferences record.
// Returns a business error with code USR0003 if the user already exists.
// Note: The caller is responsible for transaction management if needed.
// Returns ErrInvalidInput if opts, opts.Sess, or opts.User is nil.
func (r *userRepo) CreateUser(ctx context.Context, opts *orchestratorrepo.CreateUserOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil || opts.User == nil {
		return merrors.ErrInvalidInput
	}

	user := opts.User

	// Insert user
	_, err := opts.Sess.InsertInto("users").
		Pair("id", user.ID).
		Pair("subject", user.Subject).
		Pair("email", user.Email).
		Pair("nickname", user.Nickname).
		Pair("name", user.Name).
		Pair("surname", user.Surname).
		Pair("avatar_url", user.AvatarURL).
		Pair("country_code", user.CountryCode).
		Pair("date_of_birth", user.DateOfBirth).
		Pair("is_banned", user.IsBanned).
		Pair("has_agreed_with_terms", opts.HasAgreedWithLegal).
		Pair("has_agreed_with_privacy_policy", opts.HasAgreedWithLegal).
		Pair("created_at", user.CreatedAt).
		Pair("updated_at", user.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return merrors.NewBusinessError("User already exists", "USR0003")
		}
		return merrors.WrapStdServerError(err, "insert user")
	}

	// Insert user preferences
	_, err = opts.Sess.InsertInto("user_preferences").
		Pair("user_id", user.ID).
		Pair("platform_theme", user.Preferences.PlatformTheme).
		Pair("platform_language", user.Preferences.PlatformLanguage).
		Pair("currency_code", user.Preferences.CurrencyCode).
		Pair("updated_at", user.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "insert user preferences")
	}

	return nil
}

// IsUserDeleted checks if a soft-deleted user exists with the given subject or email.
// This is used to prevent re-registration of previously deleted accounts.
// Returns ErrInvalidInput if sess is nil and both subject and email are empty.
func (r *userRepo) IsUserDeleted(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (bool, *merrors.Error) {
	if sess == nil || (strings.TrimSpace(subject) == "" && strings.TrimSpace(email) == "") {
		return false, merrors.ErrInvalidInput
	}

	var count int
	err := sess.Select("COUNT(*)").
		From("users").
		Where("deleted_at IS NOT NULL AND (subject = ? OR email = ?)", subject, email).
		LoadOneContext(ctx, &count)
	if err != nil {
		return false, merrors.WrapStdServerError(err, "check if user is deleted")
	}

	return count > 0, nil
}

// CheckNicknameExists checks if a nickname already exists in the database.
// This is used during registration to ensure unique nicknames.
// Returns ErrInvalidInput if sess is nil or nickname is empty.
func (r *userRepo) CheckNicknameExists(ctx context.Context, sess orchestratorrepo.SessionRunner, nickname string) (bool, *merrors.Error) {
	if sess == nil || strings.TrimSpace(nickname) == "" {
		return false, merrors.ErrInvalidInput
	}

	var count int
	err := sess.Select("COUNT(*)").
		From("users").
		Where("nickname = ?", nickname).
		LoadOneContext(ctx, &count)
	if err != nil {
		return false, merrors.WrapStdServerError(err, "check nickname exists")
	}

	return count > 0, nil
}

// loadPreferences loads user preferences from the user_preferences table
// and applies them to the user model. Errors are silently ignored
// as preferences are optional data.
func (r *userRepo) loadPreferences(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) {
	var preferencesRow orchestratorrepo.UserPreferencesRow
	err := sess.Select(orchestratorrepo.Columns(userPreferencesColumns...)...).
		From("user_preferences").
		Where("user_id = ?", user.ID).
		LoadOneContext(ctx, &preferencesRow)
	if err == nil {
		preferencesRow.ApplyToUser(user)
	}
}

// Save persists user data to the database.
// It handles both creating new users and updating existing ones by checking
// if a user with the same subject exists first.
// The operation is idempotent and handles concurrent creation attempts.
// Returns ErrInvalidInput if sess is nil or user is nil.
func (r *userRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) *merrors.Error {
	if sess == nil || user == nil {
		return merrors.ErrInvalidInput
	}

	if mErr := r.saveUserRow(ctx, sess, orchestratorrepo.NewUserRow(user)); mErr != nil {
		return mErr
	}

	return r.saveUserPreferencesRow(ctx, sess, orchestratorrepo.NewUserPreferencesRow(user))
}

// SavePushToken stores or updates a push notification token for a user.
// This is used to register mobile devices for push notifications.
// Returns ErrInvalidInput if sess is nil, userID is empty, or pushToken is empty.
func (r *userRepo) SavePushToken(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, pushToken string) *merrors.Error {
	if sess == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(pushToken) == "" {
		return merrors.ErrInvalidInput
	}

	return r.saveUserPushTokenRow(ctx, sess, orchestratorrepo.NewUserPushTokenRow(userID, pushToken, time.Now().UTC()))
}

// saveUserRow inserts or updates a user record in the database.
// It first attempts to find an existing user by subject, then either updates
// the existing record or inserts a new one.
// Handles race conditions by retrying with an update if insert fails due to duplicate key.
func (r *userRepo) saveUserRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.UserRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	var existingUserRow orchestratorrepo.UserRow
	err := sess.Select(orchestratorrepo.Columns(userColumns...)...).
		From("users").
		Where("subject = ?", row.Subject).
		LoadOneContext(ctx, &existingUserRow)
	switch {
	case err == nil:
		_, err = sess.Update("users").
			Set("email", row.Email).
			Set("nickname", row.Nickname).
			Set("name", row.Name).
			Set("surname", row.Surname).
			Set("avatar_url", row.AvatarURL).
			Set("country_code", row.CountryCode).
			Set("date_of_birth", row.DateOfBirth).
			Set("is_banned", row.IsBanned).
			Set("has_agreed_with_terms", row.HasAgreedWithTerms).
			Set("has_agreed_with_privacy_policy", row.HasAgreedWithPrivacyPolicy).
			Set("updated_at", row.UpdatedAt).
			Set("deleted_at", row.DeletedAt).
			Where("subject = ?", row.Subject).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update user")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select user by subject for save")
	}

	_, err = sess.InsertInto("users").
		Pair("id", row.ID).
		Pair("subject", row.Subject).
		Pair("email", row.Email).
		Pair("nickname", row.Nickname).
		Pair("name", row.Name).
		Pair("surname", row.Surname).
		Pair("avatar_url", row.AvatarURL).
		Pair("country_code", row.CountryCode).
		Pair("date_of_birth", row.DateOfBirth).
		Pair("is_banned", row.IsBanned).
		Pair("has_agreed_with_terms", row.HasAgreedWithTerms).
		Pair("has_agreed_with_privacy_policy", row.HasAgreedWithPrivacyPolicy).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		Pair("deleted_at", row.DeletedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("users").
				Set("email", row.Email).
				Set("nickname", row.Nickname).
				Set("name", row.Name).
				Set("surname", row.Surname).
				Set("avatar_url", row.AvatarURL).
				Set("country_code", row.CountryCode).
				Set("date_of_birth", row.DateOfBirth).
				Set("is_banned", row.IsBanned).
				Set("has_agreed_with_terms", row.HasAgreedWithTerms).
				Set("has_agreed_with_privacy_policy", row.HasAgreedWithPrivacyPolicy).
				Set("updated_at", row.UpdatedAt).
				Set("deleted_at", row.DeletedAt).
				Where("subject = ?", row.Subject).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update user after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert user")
	}

	return nil
}

// saveUserPreferencesRow inserts or updates user preferences in the database.
// It handles both creating new preferences and updating existing ones.
// Handles race conditions by retrying with an update if insert fails due to duplicate key.
func (r *userRepo) saveUserPreferencesRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.UserPreferencesRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	var existingRow orchestratorrepo.UserPreferencesRow
	err := sess.Select(orchestratorrepo.Columns(userPreferencesColumns...)...).
		From("user_preferences").
		Where("user_id = ?", row.UserID).
		LoadOneContext(ctx, &existingRow)
	switch {
	case err == nil:
		_, err = sess.Update("user_preferences").
			Set("platform_theme", row.PlatformTheme).
			Set("platform_language", row.PlatformLanguage).
			Set("currency_code", row.CurrencyCode).
			Set("updated_at", row.UpdatedAt).
			Where("user_id = ?", row.UserID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update user preferences")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select user preferences by user id for save")
	}

	_, err = sess.InsertInto("user_preferences").
		Pair("user_id", row.UserID).
		Pair("platform_theme", row.PlatformTheme).
		Pair("platform_language", row.PlatformLanguage).
		Pair("currency_code", row.CurrencyCode).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("user_preferences").
				Set("platform_theme", row.PlatformTheme).
				Set("platform_language", row.PlatformLanguage).
				Set("currency_code", row.CurrencyCode).
				Set("updated_at", row.UpdatedAt).
				Where("user_id = ?", row.UserID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update user preferences after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert user preferences")
	}

	return nil
}

// saveUserPushTokenRow inserts or updates a user push token in the database.
// It handles both creating new tokens and updating existing ones for a user.
// Handles race conditions by retrying with an update if insert fails due to duplicate key.
func (r *userRepo) saveUserPushTokenRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.UserPushTokenRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	var existingRow orchestratorrepo.UserPushTokenRow
	err := sess.Select("user_id", "push_token", "updated_at").
		From("user_push_tokens").
		Where("user_id = ?", row.UserID).
		LoadOneContext(ctx, &existingRow)
	switch {
	case err == nil:
		_, err = sess.Update("user_push_tokens").
			Set("push_token", row.PushToken).
			Set("updated_at", row.UpdatedAt).
			Where("user_id = ?", row.UserID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update user push token")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select user push token by user id for save")
	}

	_, err = sess.InsertInto("user_push_tokens").
		Pair("user_id", row.UserID).
		Pair("push_token", row.PushToken).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("user_push_tokens").
				Set("push_token", row.PushToken).
				Set("updated_at", row.UpdatedAt).
				Where("user_id = ?", row.UserID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update user push token after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert user push token")
	}

	return nil
}
