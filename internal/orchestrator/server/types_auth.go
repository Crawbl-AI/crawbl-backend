package server

import "time"

type authDeleteRequest struct {
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

type savePushTokenRequest struct {
	PushToken string `json:"pushToken"`
}

type savePushTokenResponse struct {
	Success bool `json:"success"`
}

type userPreferencesResponse struct {
	PlatformTheme    string `json:"platformTheme"`
	PlatformLanguage string `json:"platformLanguage"`
	CurrencyCode     string `json:"currencyCode"`
}

type userSubscriptionResponse struct {
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type userProfileResponse struct {
	Email                      string                   `json:"email"`
	FirebaseUID                string                   `json:"firebaseUid"`
	Nickname                   string                   `json:"nickname"`
	Name                       string                   `json:"name"`
	Surname                    string                   `json:"surname"`
	AvatarURL                  string                   `json:"avatarUrl"`
	CountryCode                string                   `json:"countryCode"`
	DateOfBirth                *time.Time               `json:"dateOfBirth"`
	CreatedAt                  time.Time                `json:"createdAt"`
	IsDeleted                  bool                     `json:"isDeleted"`
	IsBanned                   bool                     `json:"isBanned"`
	HasAgreedWithTerms         bool                     `json:"hasAgreedWithTerms"`
	HasAgreedWithPrivacyPolicy bool                     `json:"hasAgreedWithPrivacyPolicy"`
	Preferences                userPreferencesResponse  `json:"preferences"`
	Subscription               userSubscriptionResponse `json:"subscription"`
}

type userUpdatePreferencesRequest struct {
	PlatformTheme    *string `json:"platformTheme"`
	PlatformLanguage *string `json:"platformLanguage"`
	CurrencyCode     *string `json:"currencyCode"`
}

type userUpdateRequest struct {
	Nickname    *string                       `json:"nickname"`
	Name        *string                       `json:"name"`
	Surname     *string                       `json:"surname"`
	CountryCode *string                       `json:"countryCode"`
	DateOfBirth *dateTime                     `json:"dateOfBirth"`
	Preferences *userUpdatePreferencesRequest `json:"preferences"`
}

type userLegalResponse struct {
	TermsOfService             string `json:"termsOfService"`
	PrivacyPolicy              string `json:"privacyPolicy"`
	TermsOfServiceVersion      string `json:"termsOfServiceVersion"`
	PrivacyPolicyVersion       string `json:"privacyPolicyVersion"`
	HasAgreedWithTerms         bool   `json:"hasAgreedWithTerms"`
	HasAgreedWithPrivacyPolicy bool   `json:"hasAgreedWithPrivacyPolicy"`
}

type userLegalAcceptRequest struct {
	TermsOfServiceVersion *string `json:"termsOfServiceVersion"`
	PrivacyPolicyVersion  *string `json:"privacyPolicyVersion"`
}
