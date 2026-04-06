package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

func registerHTTPSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^I send a GET request to "([^"]*)" without auth$`, tc.sendGetNoAuth)
	sc.Step(`^user "([^"]*)" sends a GET request to "([^"]*)"$`, tc.userSendsGet)
	sc.Step(`^user "([^"]*)" sends a POST request to "([^"]*)"$`, tc.userSendsPost)
	sc.Step(`^user "([^"]*)" sends a POST request to "([^"]*)" with JSON:$`, tc.userSendsPostJSON)
	sc.Step(`^user "([^"]*)" sends a PATCH request to "([^"]*)" with JSON:$`, tc.userSendsPatchJSON)
	sc.Step(`^user "([^"]*)" sends a DELETE request to "([^"]*)" with JSON:$`, tc.userSendsDeleteJSON)
}

func (tc *testContext) sendGetNoAuth(path string) error {
	url := tc.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, http.NoBody)
	if err != nil {
		return err
	}
	return tc.executeRequest(req)
}

func (tc *testContext) userSendsGet(alias, path string) error {
	path = tc.interpolatePath(path)
	url := tc.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, http.NoBody)
	if err != nil {
		return err
	}
	tc.setAuthHeaders(req, alias)
	return tc.executeRequest(req)
}

func (tc *testContext) userSendsPost(alias, path string) error {
	path = tc.interpolatePath(path)
	url := tc.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(context.Background(), "POST", url, http.NoBody)
	if err != nil {
		return err
	}
	tc.setAuthHeaders(req, alias)
	return tc.executeRequest(req)
}

func (tc *testContext) userSendsPostJSON(alias, path string, body *godog.DocString) error {
	return tc.sendWithBody("POST", alias, path, body.Content)
}

func (tc *testContext) userSendsPatchJSON(alias, path string, body *godog.DocString) error {
	return tc.sendWithBody("PATCH", alias, path, body.Content)
}

func (tc *testContext) userSendsDeleteJSON(alias, path string, body *godog.DocString) error {
	return tc.sendWithBody("DELETE", alias, path, body.Content)
}

func (tc *testContext) sendWithBody(method, alias, path, jsonBody string) error {
	path = tc.interpolatePath(path)
	// Interpolate saved values in JSON body too (e.g. {researcher_id} → actual UUID).
	jsonBody = tc.interpolatePath(jsonBody)
	url := tc.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewBufferString(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	tc.setAuthHeaders(req, alias)
	return tc.executeRequest(req)
}

func (tc *testContext) executeRequest(req *http.Request) error {
	resp, err := tc.http.Do(req)
	if err != nil {
		tc.lastStatus = 0
		tc.lastBody = nil
		return fmt.Errorf("request %s %s failed: %w", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	tc.lastStatus = resp.StatusCode
	tc.lastBody = body
	return nil
}

func (tc *testContext) setAuthHeaders(req *http.Request, alias string) {
	user := tc.users[alias]
	if user == nil {
		return
	}

	if tc.cfg.E2EToken != "" {
		req.Header.Set("X-E2E-Token", tc.cfg.E2EToken)
		req.Header.Set("X-E2E-UID", user.subject)
		req.Header.Set("X-E2E-Email", user.email)
		req.Header.Set("X-E2E-Name", user.name)
	} else {
		req.Header.Set("X-Firebase-UID", user.subject)
		req.Header.Set("X-Firebase-Email", user.email)
		req.Header.Set("X-Firebase-Name", user.name)
	}

	req.Header.Set("X-Device-Info", "crawbl-e2e-godog")
	req.Header.Set("X-Device-ID", "e2e-"+alias)
	req.Header.Set("X-Version", "0.0.0+e2e")
	req.Header.Set("X-Timezone", "UTC")
}

// interpolatePath replaces {key} placeholders with saved values.
func (tc *testContext) interpolatePath(path string) string {
	for k, v := range tc.saved {
		path = strings.ReplaceAll(path, "{"+k+"}", v)
	}
	return path
}

// doRequest is a helper that sends a request and returns the body as string.
// Used by setup steps that don't care about assertions.
func (tc *testContext) doRequest(method, path, alias string, body any) (string, error) {
	path = tc.interpolatePath(path)
	url := tc.cfg.BaseURL + path

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	tc.setAuthHeaders(req, alias)

	resp, err := tc.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request %s %s failed: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	tc.lastStatus = resp.StatusCode
	tc.lastBody = respBody
	return string(respBody), nil
}
