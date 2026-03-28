package e2e

import (
	"net/http"

	"github.com/gavv/httpexpect/v2"
)

// SuiteLegal tests the user legal agreement endpoints.
func SuiteLegal() Suite {
	return Suite{
		Name: "legal",
		Tests: []Test{
			{"GET /v1/users/legal (status)", testLegalStatus},
			{"POST /v1/users/legal/accept (terms)", testAcceptTerms},
			{"POST /v1/users/legal/accept (privacy)", testAcceptPrivacy},
			{"GET /v1/users/legal (verify)", testLegalVerify},
		},
	}
}

func testLegalStatus(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	obj := auth.GET("/v1/users/legal").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object()

	obj.Value("terms_of_service_version").String().NotEmpty()
	obj.Value("privacy_policy_version").String().NotEmpty()
}

func testAcceptTerms(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	auth.POST("/v1/users/legal/accept").
		WithJSON(map[string]any{"terms_of_service_version": "v1"}).
		Expect().
		Status(http.StatusNoContent)
}

func testAcceptPrivacy(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	auth.POST("/v1/users/legal/accept").
		WithJSON(map[string]any{"privacy_policy_version": "v1"}).
		Expect().
		Status(http.StatusNoContent)
}

func testLegalVerify(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	obj := auth.GET("/v1/users/legal").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object()

	obj.HasValue("has_agreed_with_terms", true)
	obj.HasValue("has_agreed_with_privacy_policy", true)
}
