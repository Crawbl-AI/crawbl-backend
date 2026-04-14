package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
)

// UserProfileToProto converts a domain User to the proto UserProfileResponse.
func UserProfileToProto(user *orchestrator.User) *mobilev1.UserProfileResponse {
	subscriptionName := user.Subscription.Name
	if subscriptionName == "" {
		subscriptionName = orchestrator.DefaultSubscriptionName
	}
	subscriptionCode := user.Subscription.Code
	if subscriptionCode == "" {
		subscriptionCode = orchestrator.DefaultSubscriptionCode
	}

	resp := &mobilev1.UserProfileResponse{
		Email:                    user.Email,
		FirebaseUid:              user.Subject,
		Nickname:                 user.Nickname,
		Name:                     user.Name,
		Surname:                  user.Surname,
		AvatarUrl:                stringOrEmpty(user.AvatarURL),
		CountryCode:              stringOrEmpty(user.CountryCode),
		CreatedAt:                timestamppb.New(user.CreatedAt),
		IsDeleted:                user.DeletedAt != nil,
		IsBanned:                 user.IsBanned,
		HasAgreedWithTerms:       user.HasAgreedWithTerms,
		HasAgreedWithPrivacyPolicy: user.HasAgreedWithPrivacyPolicy,
		Preferences: &mobilev1.UserPreferencesResponse{
			PlatformTheme:    stringOrEmpty(user.Preferences.PlatformTheme),
			PlatformLanguage: stringOrEmpty(user.Preferences.PlatformLanguage),
			CurrencyCode:     stringOrEmpty(user.Preferences.CurrencyCode),
		},
		Subscription: &mobilev1.UserSubscriptionResponse{
			Name: subscriptionName,
			Code: subscriptionCode,
		},
	}

	if user.DateOfBirth != nil {
		resp.DateOfBirth = timestamppb.New(*user.DateOfBirth)
	}
	if user.Subscription.ExpiresAt != nil {
		resp.Subscription.ExpiresAt = timestamppb.New(*user.Subscription.ExpiresAt)
	}

	return resp
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
