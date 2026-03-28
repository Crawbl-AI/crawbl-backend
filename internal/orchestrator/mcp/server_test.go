package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

const testSigningKey = "test-mcp-signing-key-for-tests!!"

// TestMCPServerAuth verifies the auth middleware rejects bad tokens and accepts good ones.
func TestMCPServerAuth(t *testing.T) {
	handler := NewHandler(&Deps{
		SigningKey: testSigningKey,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	t.Run("no auth header returns 401", func(t *testing.T) {
		resp, err := http.Post(ts.URL, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, nil)
		req.Header.Set("Authorization", "Bearer invalid.token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("valid token passes auth", func(t *testing.T) {
		token := crawblhmac.GenerateToken(testSigningKey, "user-123", "ws-456")

		// Send MCP initialize request
		initReq := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]any{},
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}
		body, _ := json.Marshal(initReq)

		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		// Should not be 401/403 - the MCP server should process the request
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			respBody, _ := io.ReadAll(resp.Body)
			t.Errorf("auth should have passed, got status %d: %s", resp.StatusCode, string(respBody))
		}
	})
}

// TestMCPServerToolList verifies the server exposes our 5 tools.
func TestMCPServerToolList(t *testing.T) {
	handler := NewHandler(&Deps{
		SigningKey: testSigningKey,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	token := crawblhmac.GenerateToken(testSigningKey, "user-123", "ws-456")

	// Step 1: Initialize session
	sessionID := mcpInit(t, ts.URL, token)
	if sessionID == "" {
		t.Fatal("no session ID returned from initialize")
	}

	// Step 2: Send initialized notification
	mcpNotify(t, ts.URL, token, sessionID, "notifications/initialized")

	// Step 3: List tools
	toolsReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	body, _ := json.Marshal(toolsReq)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list returned %d: %s", resp.StatusCode, string(respBody))
	}

	// The server may return SSE format (event: message\ndata: {...}\n)
	// or plain JSON. Extract the JSON payload from either format.
	jsonPayload := extractJSONFromSSE(respBody)

	// Parse and verify tool names
	var result map[string]any
	if err := json.Unmarshal(jsonPayload, &result); err != nil {
		t.Fatalf("parse response: %v\nbody: %s", err, string(respBody))
	}

	resultField, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result field in response: %s", string(respBody))
	}

	tools, ok := resultField["tools"].([]any)
	if !ok {
		t.Fatalf("no tools field in result: %s", string(respBody))
	}

	expectedTools := map[string]bool{
		"send_push_notification": false,
		"get_user_profile":       false,
		"get_workspace_info":     false,
		"list_conversations":     false,
		"search_past_messages":   false,
	}

	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		name, _ := toolMap["name"].(string)
		if _, exists := expectedTools[name]; exists {
			expectedTools[name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("tool %q not found in tools/list response", name)
		}
	}

	t.Logf("found %d tools, all 5 expected tools present", len(tools))
}

// mcpInit sends an initialize request and returns the session ID.
func mcpInit(t *testing.T, url, token string) string {
	t.Helper()
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":   map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	body, _ := json.Marshal(initReq)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initialize request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	return resp.Header.Get("Mcp-Session-Id")
}

// extractJSONFromSSE extracts the JSON payload from an SSE response body.
// If the body is already plain JSON, returns it as-is.
func extractJSONFromSSE(body []byte) []byte {
	s := string(body)
	// Look for "data: " prefix (SSE format)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			return []byte(strings.TrimPrefix(line, "data: "))
		}
	}
	// Not SSE, return as-is
	return body
}

// mcpNotify sends a JSON-RPC notification (no id, no response expected).
func mcpNotify(t *testing.T, url, token, sessionID, method string) {
	t.Helper()
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	body, _ := json.Marshal(notif)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("notification %s failed: %v", method, err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
}
