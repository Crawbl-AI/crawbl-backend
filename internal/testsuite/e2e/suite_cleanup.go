package e2e

import (
	"net/http"

	"github.com/gavv/httpexpect/v2"
)

// SuiteCleanup tests account deletion (the final destructive step).
func SuiteCleanup() Suite {
	return Suite{
		Name: "cleanup",
		Tests: []Test{
			{"DELETE /v1/auth/delete", testDeleteAccount},
			{"GET /v1/users/profile (verify soft delete)", testProfileSoftDeleted},
		},
	}
}

func testDeleteAccount(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	auth.DELETE("/v1/auth/delete").
		WithJSON(map[string]any{
			"reason":      "e2e-cleanup",
			"description": "automated e2e test cleanup",
		}).
		Expect().
		Status(http.StatusNoContent)
}

func testProfileSoftDeleted(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
	resp := auth.GET("/v1/users/profile").
		Expect()

	raw := resp.Raw()
	if raw != nil && raw.StatusCode == http.StatusForbidden {
		return // hard delete path — 403 is acceptable
	}

	// Soft delete — profile returns 200 with is_deleted=true.
	resp.Status(http.StatusOK).
		JSON().Object().
		Value("data").Object().
		HasValue("is_deleted", true)
}
