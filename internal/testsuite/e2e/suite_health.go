package e2e

import (
	"net/http"

	"github.com/gavv/httpexpect/v2"
)

// SuiteHealth tests public endpoints that don't require authentication.
func SuiteHealth() Suite {
	return Suite{
		Name: "health",
		Tests: []Test{
			{"GET /v1/health", testHealth},
			{"GET /v1/legal", testPublicLegal},
		},
	}
}

func testHealth(_ *httpexpect.Expect, pub *httpexpect.Expect, _ map[string]string) {
	pub.GET("/v1/health").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object().
		HasValue("online", true)
}

func testPublicLegal(_ *httpexpect.Expect, pub *httpexpect.Expect, _ map[string]string) {
	obj := pub.GET("/v1/legal").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object()

	obj.Value("terms_of_service").String().Contains("crawbl.com")
	obj.Value("privacy_policy").String().Contains("crawbl.com")
	obj.Value("terms_of_service_version").String().NotEmpty()
	obj.Value("privacy_policy_version").String().NotEmpty()
}
