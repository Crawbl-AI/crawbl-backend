package e2e

import (
	"net/http"

	"github.com/gavv/httpexpect/v2"
)

// SuiteWorkspaces tests workspace listing, retrieval, and agent listing.
func SuiteWorkspaces() Suite {
	return Suite{
		Name: "workspaces",
		Tests: []Test{
			{"GET /v1/workspaces", testListWorkspaces},
			{"GET /v1/workspaces/{id}", testGetWorkspace},
			{"GET /v1/workspaces/{id}/agents", testListAgents},
		},
	}
}

func testListWorkspaces(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	arr := auth.GET("/v1/workspaces").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Array()

	arr.Length().IsEqual(1)

	ws := arr.Value(0).Object()
	wsID := ws.Value("id").String().NotEmpty().Raw()
	ws.Value("name").String().NotEmpty()
	ws.Value("runtime").Object().Value("status").String().NotEmpty()

	state["workspace_id"] = wsID
}

func testGetWorkspace(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	wsID := state["workspace_id"]

	auth.GET("/v1/workspaces/{id}", wsID).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object().
		HasValue("id", wsID)
}

func testListAgents(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	wsID := state["workspace_id"]

	arr := auth.GET("/v1/workspaces/{id}/agents", wsID).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Array()

	arr.NotEmpty()

	agent := arr.Value(0).Object()
	agentID := agent.Value("id").String().NotEmpty().Raw()
	agent.Value("name").String().NotEmpty()
	agent.Value("role").String().NotEmpty()

	state["agent_id"] = agentID
}
