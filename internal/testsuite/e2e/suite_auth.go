package e2e

import (
	"net/http"

	"github.com/gavv/httpexpect/v2"
)

// SuiteAuth tests sign-up and sign-in flows.
func SuiteAuth(cfg *Config) Suite {
	return Suite{
		Name: "auth",
		Tests: []Test{
			{"POST /v1/auth/sign-up", func(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
				auth.POST("/v1/auth/sign-up").
					Expect().
					Status(http.StatusNoContent)
				state["uid"] = cfg.UID
			}},
			{"POST /v1/auth/sign-in", func(auth *httpexpect.Expect, _ *httpexpect.Expect, _ map[string]string) {
				auth.POST("/v1/auth/sign-in").
					Expect().
					Status(http.StatusNoContent)
			}},
		},
	}
}
