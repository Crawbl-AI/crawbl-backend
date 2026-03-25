package userrepo

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

var userPreferencesColumns = []string{
	"user_id",
	"platform_theme",
	"platform_language",
	"currency_code",
	"updated_at",
}
