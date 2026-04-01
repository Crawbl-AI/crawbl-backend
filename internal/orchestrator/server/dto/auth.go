package dto

import "time"

// AuthDeleteRequest represents the request body for account deletion.
// Users must provide a reason and optional description when deleting their account.
type AuthDeleteRequest struct {
	// Reason is the primary reason for account deletion.
	Reason string `json:"reason"`

	// Description provides additional context about the deletion request.
	Description string `json:"description"`
}

// SavePushTokenRequest represents the request body for saving a FCM push token.
// The token is used to send push notifications to the user's device.
type SavePushTokenRequest struct {
	// PushToken is the Firebase Cloud Messaging device token for push notifications.
	PushToken string `json:"push_token"`
}

// SavePushTokenResponse indicates the result of saving a push token.
type SavePushTokenResponse struct {
	// Success indicates whether the token was saved successfully.
	Success bool `json:"success"`
}

// UserPreferencesResponse represents user preference settings in API responses.
// These control platform appearance and behavior for the user.
type UserPreferencesResponse struct {
	// PlatformTheme is the user's preferred UI theme (e.g., "light", "dark", "auto").
	PlatformTheme string `json:"platform_theme"`

	// PlatformLanguage is the user's preferred language code (e.g., "en", "es").
	PlatformLanguage string `json:"platform_language"`

	// CurrencyCode is the user's preferred currency for displaying prices (e.g., "USD").
	CurrencyCode string `json:"currency_code"`
}

// UserSubscriptionResponse represents subscription details in user profile responses.
// Free tier users receive default values for name and code fields.
type UserSubscriptionResponse struct {
	// Name is the display name of the subscription tier (e.g., "Freemium", "Pro").
	Name string `json:"name"`

	// Code is the subscription plan code for programmatic use (e.g., "freemium", "pro").
	Code string `json:"code"`

	// ExpiresAt is the subscription expiration time, nil for non-expiring or free tiers.
	ExpiresAt *time.Time `json:"expires_at"`
}

// UserProfileResponse contains the complete user profile for API responses.
// Includes account details, preferences, subscription status, and legal acceptance state.
type UserProfileResponse struct {
	// Email is the user's registered email address.
	Email string `json:"email"`

	// FirebaseUID is the Firebase Authentication user identifier.
	FirebaseUID string `json:"firebase_uid"`

	// Nickname is the user's display name in the platform.
	Nickname string `json:"nickname"`

	// Name is the user's given name.
	Name string `json:"name"`

	// Surname is the user's family name.
	Surname string `json:"surname"`

	// AvatarURL is the URL to the user's profile picture.
	AvatarURL string `json:"avatar_url"`

	// CountryCode is the user's country of residence (ISO 3166-1 alpha-2).
	CountryCode string `json:"country_code"`

	// DateOfBirth is the user's birth date, if provided.
	DateOfBirth *time.Time `json:"date_of_birth"`

	// CreatedAt is the timestamp when the account was created.
	CreatedAt time.Time `json:"created_at"`

	// IsDeleted indicates whether the account has been soft-deleted.
	IsDeleted bool `json:"is_deleted"`

	// IsBanned indicates whether the account has been banned from the platform.
	IsBanned bool `json:"is_banned"`

	// HasAgreedWithTerms indicates acceptance of the current terms of service.
	HasAgreedWithTerms bool `json:"has_agreed_with_terms"`

	// HasAgreedWithPrivacyPolicy indicates acceptance of the current privacy policy.
	HasAgreedWithPrivacyPolicy bool `json:"has_agreed_with_privacy_policy"`

	// Preferences contains user customization settings.
	Preferences UserPreferencesResponse `json:"preferences"`

	// Subscription contains the user's current subscription details.
	Subscription UserSubscriptionResponse `json:"subscription"`
}

// UserUpdatePreferencesRequest represents updatable preference fields.
// All fields are optional; only provided fields will be updated.
type UserUpdatePreferencesRequest struct {
	// PlatformTheme is the new preferred UI theme.
	PlatformTheme *string `json:"platform_theme"`

	// PlatformLanguage is the new preferred language code.
	PlatformLanguage *string `json:"platform_language"`

	// CurrencyCode is the new preferred currency code.
	CurrencyCode *string `json:"currency_code"`
}

// UserUpdateRequest represents the request body for updating user profile.
// All fields are optional; only provided fields will be modified.
type UserUpdateRequest struct {
	// Nickname is the new display name.
	Nickname *string `json:"nickname"`

	// Name is the new given name.
	Name *string `json:"name"`

	// Surname is the new family name.
	Surname *string `json:"surname"`

	// CountryCode is the new country of residence.
	CountryCode *string `json:"country_code"`

	// DateOfBirth is the new birth date.
	DateOfBirth *DateTime `json:"date_of_birth"`

	// Preferences contains updatable preference settings.
	Preferences *UserUpdatePreferencesRequest `json:"preferences"`
}

// UserLegalResponse combines legal documents with the user's acceptance status.
// Used to display legal documents and show which ones the user has agreed to.
type UserLegalResponse struct {
	// TermsOfService is the full text of the terms of service.
	TermsOfService string `json:"terms_of_service"`

	// PrivacyPolicy is the full text of the privacy policy.
	PrivacyPolicy string `json:"privacy_policy"`

	// TermsOfServiceVersion is the current version identifier for terms of service.
	TermsOfServiceVersion string `json:"terms_of_service_version"`

	// PrivacyPolicyVersion is the current version identifier for privacy policy.
	PrivacyPolicyVersion string `json:"privacy_policy_version"`

	// HasAgreedWithTerms indicates whether the user accepted the current terms version.
	HasAgreedWithTerms bool `json:"has_agreed_with_terms"`

	// HasAgreedWithPrivacyPolicy indicates whether the user accepted the current policy version.
	HasAgreedWithPrivacyPolicy bool `json:"has_agreed_with_privacy_policy"`
}

// UserLegalAcceptRequest represents the request body for accepting legal documents.
// Users must specify which versions they are accepting.
type UserLegalAcceptRequest struct {
	// TermsOfServiceVersion is the version of terms of service being accepted.
	TermsOfServiceVersion *string `json:"terms_of_service_version"`

	// PrivacyPolicyVersion is the version of privacy policy being accepted.
	PrivacyPolicyVersion *string `json:"privacy_policy_version"`
}
