// Package e2e — HTTP transport plumbing for step definitions.
// Every outbound request flows through doRequest which stamps
// tc.lastStatus + tc.lastBody for downstream assertion steps.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"google.golang.org/protobuf/proto"
)

// abbreviatedBody returns at most maxBodyDisplayLen characters of body
// as a trimmed string, suitable for embedding in error messages without
// flooding logs.
func abbreviatedBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > maxBodyDisplayLen {
		return text[:maxBodyDisplayLen]
	}
	return text
}

// doProtoRequest marshals a proto message to JSON and sends it via HTTP,
// recording tc.lastStatus and tc.lastBody exactly like doRequest.
func (tc *testContext) doProtoRequest(method, path, alias string, msg proto.Message) (string, error) { //nolint:unparam // string return kept for future assertions
	path = tc.interpolatePath(path)
	url := tc.cfg.BaseURL + path

	b, err := marshalProtoJSON(msg)
	if err != nil {
		return "", fmt.Errorf("marshal proto: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
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
