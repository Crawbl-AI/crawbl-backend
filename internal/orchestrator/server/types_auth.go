package server

import "time"

// authDeleteRequest represents the request body for account deletion.
// Users must provide a reason and optional description when deleting their account.
type authDeleteRequest struct {
	// Reason is the primary reason for account deletion.
	Reason string `json:"reason"`

	// Description provides additional context about the deletion request.
	Description string `json:"description"`
}

// savePushTokenRequest represents the request body for saving a FCM push token.
// The token is used to send push notifications to the user's device.
type savePushTokenRequest struct {
	// PushToken is the Firebase Cloud Messaging device token for push notifications.
	PushToken string `json:"pushToken"`
}

// savePushTokenResponse indicates the result of saving a push token.
type savePushTokenResponse struct {
	// Success indicates whether the token was saved successfully.
	Success bool `json:"success"`
}

// userPreferencesResponse represents user preference settings in API responses.
// These control platform appearance and behavior for the user.
type userPreferencesResponse struct {
	// PlatformTheme is the user's preferred UI theme (e.g., "light", "dark", "auto").
	PlatformTheme string `json:"platformTheme"`

	// PlatformLanguage is the user's preferred language code (e.g., "en", "es").
	PlatformLanguage string `json:"platformLanguage"`

	// CurrencyCode is the user's preferred currency for displaying prices (e.g., "USD").
	CurrencyCode string `json:"currencyCode"`
}

// userSubscriptionResponse represents subscription details in user profile responses.
// Free tier users receive default values for name and code fields.
type userSubscriptionResponse struct {
	// Name is the display name of the subscription tier (e.g., "Freemium", "Pro").
	Name string `json:"name"`

	// Code is the subscription plan code for programmatic use (e.g., "freemium", "pro").
	Code string `json:"code"`

	// ExpiresAt is the subscription expiration time, nil for non-expiring or free tiers.
	ExpiresAt *time.Time `json:"expiresAt"`
}

// userProfileResponse contains the complete user profile for API responses.
// Includes account details, preferences, subscription status, and legal acceptance state.
type userProfileResponse struct {
	// Email is the user's registered email address.
	Email string `json:"email"`

	// FirebaseUID is the Firebase Authentication user identifier.
	FirebaseUID string `json:"firebaseUid"`

	// Nickname is the user's display name in the platform.
	Nickname string `json:"nickname"`

	// Name is the user's given name.
	Name string `json:"name"`

	// Surname is the user's family name.
	Surname string `json:"surname"`

	// AvatarURL is the URL to the user's profile picture.
	AvatarURL string `json:"avatarUrl"`

	// CountryCode is the user's country of residence (ISO 3166-1 alpha-2).
	CountryCode string `json:"countryCode"`

	// DateOfBirth is the user's birth date, if provided.
	DateOfBirth *time.Time `json:"dateOfBirth"`

	// CreatedAt is the timestamp when the account was created.
	CreatedAt time.Time `json:"createdAt"`

	// IsDeleted indicates whether the account has been soft-deleted.
	IsDeleted bool `json:"isDeleted"`

	// IsBanned indicates whether the account has been banned from the platform.
	IsBanned bool `json:"isBanned"`

	// HasAgreedWithTerms indicates acceptance of the current terms of service.
	HasAgreedWithTerms bool `json:"hasAgreedWithTerms"`

	// HasAgreedWithPrivacyPolicy indicates acceptance of the current privacy policy.
	HasAgreedWithPrivacyPolicy bool `json:"hasAgreedWithPrivacyPolicy"`

	// Preferences contains user customization settings.
	Preferences userPreferencesResponse `json:"preferences"`

	// Subscription contains the user's current subscription details.
	Subscription userSubscriptionResponse `json:"subscription"`
}

// userUpdatePreferencesRequest represents updatable preference fields.
// All fields are optional; only provided fields will be updated.
type userUpdatePreferencesRequest struct {
	// PlatformTheme is the new preferred UI theme.
	PlatformTheme *string `json:"platformTheme"`

	// PlatformLanguage is the new preferred language code.
	PlatformLanguage *string `json:"platformLanguage"`

	// CurrencyCode is the new preferred currency code.
	CurrencyCode *string `json:"currencyCode"`
}

// userUpdateRequest represents the request body for updating user profile.
// All fields are optional; only provided fields will be modified.
type userUpdateRequest struct {
	// Nickname is the new display name.
	Nickname *string `json:"nickname"`

	// Name is the new given name.
	Name *string `json:"name"`

	// Surname is the new family name.
	Surname *string `json:"surname"`

	// CountryCode is the new country of residence.
	CountryCode *string `json:"countryCode"`

	// DateOfBirth is the new birth date.
	DateOfBirth *dateTime `json:"dateOfBirth"`

	// Preferences contains updatable preference settings.
	Preferences *userUpdatePreferencesRequest `json:"preferences"`
}

// userLegalResponse combines legal documents with the user's acceptance status.
// Used to display legal documents and show which ones the user has agreed to.
type userLegalResponse struct {
	// TermsOfService is the full text of the terms of service.
	TermsOfService string `json:"termsOfService"`

	// PrivacyPolicy is the full text of the privacy policy.
	PrivacyPolicy string `json:"privacyPolicy"`

	// TermsOfServiceVersion is the current version identifier for terms of service.
	TermsOfServiceVersion string `json:"termsOfServiceVersion"`

	// PrivacyPolicyVersion is the current version identifier for privacy policy.
	PrivacyPolicyVersion string `json:"privacyPolicyVersion"`

	// HasAgreedWithTerms indicates whether the user accepted the current terms version.
	HasAgreedWithTerms bool `json:"hasAgreedWithTerms"`

	// HasAgreedWithPrivacyPolicy indicates whether the user accepted the current policy version.
	HasAgreedWithPrivacyPolicy bool `json:"hasAgreedWithPrivacyPolicy"`
}

// userLegalAcceptRequest represents the request body for accepting legal documents.
// Users must specify which versions they are accepting.
type userLegalAcceptRequest struct {
	// TermsOfServiceVersion is the version of terms of service being accepted.
	TermsOfServiceVersion *string `json:"termsOfServiceVersion"`

	// PrivacyPolicyVersion is the version of privacy policy being accepted.
	PrivacyPolicyVersion *string `json:"privacyPolicyVersion"`
}
