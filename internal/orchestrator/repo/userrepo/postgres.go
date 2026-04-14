package userrepo

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a PostgreSQL-backed user persistence struct. The returned
// value uses the SessionRunner pattern for transaction support and
// implicitly satisfies any consumer-side interface (e.g.
// authservice.userStore) that lists a subset of its methods.
func New() *userRepo {
	return &userRepo{}
}

// GetBySubject retrieves a user by their Firebase authentication subject (UID).
// Returns ErrUserNotFound if no user exists with the given subject.
// Returns ErrInvalidInput if sess is nil or subject is empty.
func (r *userRepo) GetBySubject(ctx context.Context, sess orchestratorrepo.SessionRunner, subject string) (*orchestrator.User, *merrors.Error) {
	if strings.TrimSpace(subject) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var userRow orchestratorrepo.UserRow
	err := sess.Select(userColumns...).
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

func (r *userRepo) GetUser(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (*orchestrator.User, *merrors.Error) {
	if strings.TrimSpace(subject) == "" && strings.TrimSpace(email) == "" {
		return nil, merrors.ErrInvalidInput
	}

	// Prefer lookup by email (unique), so we can detect subject mismatches.
	if email != "" {
		user, mErr := r.getUserByEmail(ctx, sess, email, subject)
		if mErr == nil || !merrors.IsCode(mErr, merrors.ErrCodeUserNotFound) {
			return user, mErr
		}
		// Not found by email — fall through to subject lookup.
	}

	// Fallback: lookup by subject.
	if subject != "" {
		return r.getUserBySubject(ctx, sess, subject)
	}

	return nil, merrors.ErrUserNotFound
}

// getUserByEmail looks up a user by email. If found and the subject doesn't match, returns ErrUserWrongFirebaseUID.
func (r *userRepo) getUserByEmail(ctx context.Context, sess orchestratorrepo.SessionRunner, email, subject string) (*orchestrator.User, *merrors.Error) {
	var userRow orchestratorrepo.UserRow
	err := sess.Select(userColumns...).
		From("users").
		Where("email = ?", email).
		LoadOneContext(ctx, &userRow)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrUserNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select user by email")
	}
	if subject != "" && userRow.Subject != subject {
		return nil, merrors.ErrUserWrongFirebaseUID
	}
	user := userRow.ToDomain()
	r.loadPreferences(ctx, sess, user)
	return user, nil
}

// getUserBySubject looks up a user by Firebase subject (UID).
func (r *userRepo) getUserBySubject(ctx context.Context, sess orchestratorrepo.SessionRunner, subject string) (*orchestrator.User, *merrors.Error) {
	var userRow orchestratorrepo.UserRow
	err := sess.Select(userColumns...).
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

// CreateUser creates a new user with the specified legal agreement status.
// It inserts both the user record and the associated user preferences record.
//
// The user INSERT uses ON CONFLICT (subject) DO UPDATE so that two concurrent
// first-login requests for the same Firebase subject (which generate different
// UUIDs locally) both resolve to the same database row. The winning row's id
// is written back to opts.User.ID so the caller can use it for subsequent
// inserts (workspace, usage quota, etc.).
//
// Returns ErrInvalidInput if opts, opts.Sess, or opts.User is nil.
func (r *userRepo) CreateUser(ctx context.Context, opts *orchestratorrepo.CreateUserOpts) *merrors.Error {
	if opts == nil || opts.User == nil {
		return merrors.ErrInvalidInput
	}

	user := opts.User

	// INSERT ... ON CONFLICT (subject) DO UPDATE SET updated_at = NOW() RETURNING id
	//
	// If a concurrent request already inserted a row for this subject, the
	// conflict target fires and we update updated_at (a no-op in practice) and
	// return the existing id. This makes CreateUser idempotent under concurrent
	// first-login races on the subject UNIQUE constraint.
	//
	// Raw SQL is required because dbr has no ON CONFLICT … RETURNING builder.
	const userQuery = `
INSERT INTO users (
	id, subject, email, nickname, name, surname,
	avatar_url, country_code, date_of_birth, is_banned,
	has_agreed_with_terms, has_agreed_with_privacy_policy,
	created_at, updated_at
) VALUES (
	?, ?, ?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?,
	?, ?
)
ON CONFLICT (subject) DO UPDATE SET
	updated_at = EXCLUDED.updated_at
RETURNING id`

	var resolvedID string
	if err := opts.Sess.InsertBySql(userQuery,
		user.ID, user.Subject, user.Email, user.Nickname, user.Name, user.Surname,
		user.AvatarURL, user.CountryCode, user.DateOfBirth, user.IsBanned,
		opts.HasAgreedWithLegal, opts.HasAgreedWithLegal,
		user.CreatedAt, user.UpdatedAt,
	).LoadContext(ctx, &resolvedID); err != nil {
		return merrors.WrapStdServerError(err, "insert user")
	}
	// Write back the winning id — may differ from user.ID if a concurrent
	// insert won the race and the existing row was returned.
	opts.User.ID = resolvedID

	// Insert user preferences using ON CONFLICT DO NOTHING: if a concurrent
	// request already created the row (because it won the users INSERT race),
	// we leave the existing preferences untouched.
	const prefsQuery = `
INSERT INTO user_preferences (user_id, platform_theme, platform_language, currency_code, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id) DO NOTHING`

	if _, err := opts.Sess.InsertBySql(prefsQuery,
		resolvedID,
		user.Preferences.PlatformTheme,
		user.Preferences.PlatformLanguage,
		user.Preferences.CurrencyCode,
		user.UpdatedAt,
	).ExecContext(ctx); err != nil {
		return merrors.WrapStdServerError(err, "insert user preferences")
	}

	return nil
}

// IsUserDeleted checks if a soft-deleted user exists with the given subject or email.
// This is used to prevent re-registration of previously deleted accounts.
// Returns ErrInvalidInput if sess is nil and both subject and email are empty.
func (r *userRepo) IsUserDeleted(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (bool, *merrors.Error) {
	if strings.TrimSpace(subject) == "" && strings.TrimSpace(email) == "" {
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
	if strings.TrimSpace(nickname) == "" {
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
// and applies them to the user model. Not-found is silently ignored since
// preferences are optional; all other errors are logged as warnings.
func (r *userRepo) loadPreferences(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) {
	var preferencesRow orchestratorrepo.UserPreferencesRow
	err := sess.Select(userPreferencesColumns...).
		From("user_preferences").
		Where("user_id = ?", user.ID).
		LoadOneContext(ctx, &preferencesRow)
	if err == nil {
		preferencesRow.ApplyToUser(user)
		return
	}
	if !database.IsRecordNotFoundError(err) {
		slog.Warn("failed to load user preferences", "user_id", user.ID, "error", err)
	}
}

// Save persists user data to the database.
// It handles both creating new users and updating existing ones by checking
// if a user with the same subject exists first.
// The operation is idempotent and handles concurrent creation attempts.
// Returns ErrInvalidInput if sess is nil or user is nil.
func (r *userRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) *merrors.Error {
	if user == nil {
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
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(pushToken) == "" {
		return merrors.ErrInvalidInput
	}

	return r.saveUserPushTokenRow(ctx, sess, orchestratorrepo.NewUserPushTokenRow(userID, pushToken, time.Now().UTC()))
}

// ClearPushTokens removes all push notification tokens for a user.
// Returns ErrInvalidInput if sess is nil or userID is empty.
func (r *userRepo) ClearPushTokens(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) *merrors.Error {
	if strings.TrimSpace(userID) == "" {
		return merrors.ErrInvalidInput
	}

	_, err := sess.DeleteFrom("user_push_tokens").
		Where("user_id = ?", userID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "clear push tokens")
	}

	return nil
}

// saveUserRow atomically upserts a user record in the database.
// A single INSERT ... ON CONFLICT (id) DO UPDATE SET ... is used so that
// concurrent first-login requests for the same user cannot race into a
// primary-key violation.
//
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *userRepo) saveUserRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.UserRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	const query = `
INSERT INTO users (
	id, subject, email, nickname, name, surname,
	avatar_url, country_code, date_of_birth, is_banned,
	has_agreed_with_terms, has_agreed_with_privacy_policy,
	created_at, updated_at, deleted_at
) VALUES (
	?, ?, ?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?,
	?, ?, ?
)
ON CONFLICT (id) DO UPDATE SET
	email                          = EXCLUDED.email,
	nickname                       = EXCLUDED.nickname,
	name                           = EXCLUDED.name,
	surname                        = EXCLUDED.surname,
	avatar_url                     = EXCLUDED.avatar_url,
	country_code                   = EXCLUDED.country_code,
	date_of_birth                  = EXCLUDED.date_of_birth,
	is_banned                      = EXCLUDED.is_banned,
	has_agreed_with_terms          = EXCLUDED.has_agreed_with_terms,
	has_agreed_with_privacy_policy = EXCLUDED.has_agreed_with_privacy_policy,
	updated_at                     = EXCLUDED.updated_at,
	deleted_at                     = EXCLUDED.deleted_at`

	_, err := sess.InsertBySql(query,
		row.ID, row.Subject, row.Email, row.Nickname, row.Name, row.Surname,
		row.AvatarURL, row.CountryCode, row.DateOfBirth, row.IsBanned,
		row.HasAgreedWithTerms, row.HasAgreedWithPrivacyPolicy,
		row.CreatedAt, row.UpdatedAt, row.DeletedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert user")
	}

	return nil
}

// saveUserPreferencesRow atomically upserts user preferences in the database.
// A single INSERT ... ON CONFLICT (user_id) DO UPDATE SET ... eliminates the
// SELECT-then-INSERT/UPDATE race that could cause duplicate-key errors or lost
// updates under concurrent requests. created_at is intentionally excluded from
// the DO UPDATE clause to preserve the original creation timestamp.
//
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *userRepo) saveUserPreferencesRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.UserPreferencesRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	const query = `
INSERT INTO user_preferences (user_id, platform_theme, platform_language, currency_code, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET
	platform_theme    = EXCLUDED.platform_theme,
	platform_language = EXCLUDED.platform_language,
	currency_code     = EXCLUDED.currency_code,
	updated_at        = EXCLUDED.updated_at`

	_, err := sess.InsertBySql(query,
		row.UserID, row.PlatformTheme, row.PlatformLanguage, row.CurrencyCode, row.UpdatedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert user preferences")
	}

	return nil
}

// saveUserPushTokenRow atomically upserts a push notification token in the database.
// A single INSERT ... ON CONFLICT (user_id) DO UPDATE SET ... eliminates the
// SELECT-then-INSERT/UPDATE race that could produce duplicate-key errors or lost
// updates under concurrent SavePushToken calls for the same user.
//
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *userRepo) saveUserPushTokenRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.UserPushTokenRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	const query = `
INSERT INTO user_push_tokens (user_id, push_token, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET
	push_token = EXCLUDED.push_token,
	updated_at = EXCLUDED.updated_at`

	_, err := sess.InsertBySql(query,
		row.UserID, row.PushToken, row.UpdatedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert user push token")
	}

	return nil
}
