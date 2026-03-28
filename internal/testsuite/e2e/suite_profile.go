package e2e

import (
	"net/http"

	"github.com/gavv/httpexpect/v2"
)

// SuiteProfile tests user profile CRUD and FCM token registration.
func SuiteProfile() Suite {
	return Suite{
		Name: "profile",
		Tests: []Test{
			{"GET /v1/users/profile (defaults)", testProfileDefaults},
			{"PATCH /v1/users (update)", testProfileUpdate},
			{"GET /v1/users/profile (verify update)", testProfileAfterUpdate},
			{"POST /v1/fcm-token", testFCMToken},
		},
	}
}

func testProfileDefaults(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	obj := auth.GET("/v1/users/profile").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object()

	obj.Value("firebase_uid").String().NotEmpty()
	obj.Value("email").String().NotEmpty()
	obj.Value("is_deleted").IsEqual(false)
	obj.Value("is_banned").IsEqual(false)
	obj.Value("subscription").Object().HasValue("code", "freemium")
}

func testProfileUpdate(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	auth.PATCH("/v1/users").
		WithJSON(map[string]any{
			"nickname":      "e2e-nick",
			"name":          "E2E",
			"surname":       "Tester",
			"country_code":  "DE",
			"date_of_birth": "2000-01-15T00:00:00Z",
			"preferences": map[string]any{
				"platform_theme":    "dark",
				"platform_language": "en",
				"currency_code":     "EUR",
			},
		}).
		Expect().
		Status(http.StatusNoContent)
}

func testProfileAfterUpdate(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	obj := auth.GET("/v1/users/profile").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object()

	obj.HasValue("nickname", "e2e-nick")
	obj.HasValue("name", "E2E")
	obj.HasValue("surname", "Tester")
	obj.HasValue("country_code", "DE")

	prefs := obj.Value("preferences").Object()
	prefs.HasValue("platform_theme", "dark")
	prefs.HasValue("platform_language", "en")
	prefs.HasValue("currency_code", "EUR")
}

func testFCMToken(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	auth.POST("/v1/fcm-token").
		WithJSON(map[string]any{"push_token": "e2e-test-push-token"}).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		HasValue("success", true)
}
