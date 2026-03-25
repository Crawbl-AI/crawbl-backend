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

type userRepo struct{}

func New() *userRepo {
	return &userRepo{}
}

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

	var preferencesRow orchestratorrepo.UserPreferencesRow
	err = sess.Select(orchestratorrepo.Columns(userPreferencesColumns...)...).
		From("user_preferences").
		Where("user_id = ?", user.ID).
		LoadOneContext(ctx, &preferencesRow)
	if err == nil {
		preferencesRow.ApplyToUser(user)
	} else if !database.IsRecordNotFoundError(err) {
		return nil, merrors.WrapStdServerError(err, "select user preferences by user id")
	}

	return user, nil
}

func (r *userRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) *merrors.Error {
	if sess == nil || user == nil {
		return merrors.ErrInvalidInput
	}

	if mErr := r.saveUserRow(ctx, sess, orchestratorrepo.NewUserRow(user)); mErr != nil {
		return mErr
	}

	return r.saveUserPreferencesRow(ctx, sess, orchestratorrepo.NewUserPreferencesRow(user))
}

func (r *userRepo) SavePushToken(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, pushToken string) *merrors.Error {
	if sess == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(pushToken) == "" {
		return merrors.ErrInvalidInput
	}

	return r.saveUserPushTokenRow(ctx, sess, orchestratorrepo.NewUserPushTokenRow(userID, pushToken, time.Now().UTC()))
}

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
