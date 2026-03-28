// Package userrepo provides PostgreSQL-based implementation of the UserRepo interface.
// It handles all database operations related to user entities including user data,
// preferences, and push notification tokens.
package userrepo

// userRepo is the PostgreSQL implementation of the UserRepo interface.
// It handles user data persistence and retrieval operations.
type userRepo struct{}

// userColumns defines the column names used in SELECT queries for the users table.
// These columns map directly to the UserRow struct fields.
var userColumns = []string{
	"id",
	"subject",
	"email",
	"nickname",
	"name",
	"surname",
	"avatar_url",
	"country_code",
	"date_of_birth",
	"is_banned",
	"has_agreed_with_terms",
	"has_agreed_with_privacy_policy",
	"created_at",
	"updated_at",
	"deleted_at",
}

// userPreferencesColumns defines the column names used in SELECT queries for the user_preferences table.
// These columns map directly to the UserPreferencesRow struct fields.
var userPreferencesColumns = []string{
	"user_id",
	"platform_theme",
	"platform_language",
	"currency_code",
	"updated_at",
}
