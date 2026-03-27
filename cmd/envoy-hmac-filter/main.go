// Package main implements a proxy-wasm WASM plugin that validates HMAC device
// signatures at the Envoy Gateway edge. It replicates the authentication logic
// from internal/pkg/httpserver/middleware.go (validateDeviceHeaders + GenerateHMAC).
//
// Build: tinygo build -o filter.wasm -scheduler=none -target=wasi .
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

const (
	// maxTimestampAge is the maximum age of a timestamp before it's considered expired.

	maxTimestampAge = 2 * time.Minute

	// maxClockSkew allows for small time differences between client and server clocks.

	maxClockSkew = 30 * time.Second
)

// Required device headers for X-Token authentication.
// Required device headers for X-Token authentication.
var requiredDeviceHeaders = []string{
	"x-timestamp",
	"x-signature",
	"x-device-info",
	"x-version",
}

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

// --- VM Context ---

type vmContext struct {
	types.DefaultVMContext
}

func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{}
}

// --- Plugin Context ---

// pluginConfig is the JSON structure passed via EnvoyExtensionPolicy config field.
type pluginConfig struct {
	HMACSecret  string `json:"hmac_secret"`
	Environment string `json:"environment"`
}

type pluginContext struct {
	types.DefaultPluginContext
	hmacSecret  string
	environment string
}

func (p *pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	if pluginConfigurationSize == 0 {
		proxywasm.LogCriticalf("hmac-filter: plugin configuration is required")
		return types.OnPluginStartStatusFailed
	}

	data, err := proxywasm.GetPluginConfiguration()
	if err != nil {
		proxywasm.LogCriticalf("hmac-filter: failed to read plugin configuration: %v", err)
		return types.OnPluginStartStatusFailed
	}

	var cfg pluginConfig
	// Envoy Gateway may double-encode the config as a JSON string.
	// Try direct unmarshal first, then unwrap the string if needed.
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Try unwrapping a JSON-encoded string
		var raw string
		if err2 := json.Unmarshal(data, &raw); err2 == nil {
			if err3 := json.Unmarshal([]byte(raw), &cfg); err3 != nil {
				proxywasm.LogCriticalf("hmac-filter: failed to parse plugin configuration: %v (unwrap: %v)", err, err3)
				return types.OnPluginStartStatusFailed
			}
		} else {
			proxywasm.LogCriticalf("hmac-filter: failed to parse plugin configuration: %v", err)
			return types.OnPluginStartStatusFailed
		}
	}

	if cfg.HMACSecret == "" {
		proxywasm.LogCriticalf("hmac-filter: hmac_secret is required in plugin configuration")
		return types.OnPluginStartStatusFailed
	}

	p.hmacSecret = cfg.HMACSecret
	p.environment = strings.ToLower(strings.TrimSpace(cfg.Environment))

	proxywasm.LogInfof("hmac-filter: initialized for environment=%s", p.environment)
	return types.OnPluginStartStatusOK
}

func (p *pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	return &httpContext{
		hmacSecret:  p.hmacSecret,
		environment: p.environment,
	}
}

// --- HTTP Context (per-request) ---

type httpContext struct {
	types.DefaultHttpContext
	hmacSecret  string
	environment string
}

// OnHttpRequestHeaders validates HMAC device signatures for X-Token requests.
//
// Flow:
//  1. Skip if environment is local/test (mirrors shouldSkipDeviceAuth)
//  2. Skip if no X-Token header (Bearer auth doesn't need HMAC)
//  3. Check all required device headers are present
//  4. Validate timestamp freshness (2min window, 30s clock skew)
//  5. Validate HMAC-SHA256 signature: HMAC(secret, token+":"+timestamp)
func (ctx *httpContext) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {
	// Skip HMAC validation in local/test environments.
	if ctx.environment == "local" || ctx.environment == "test" {
		return types.ActionContinue
	}

	// Check for X-Token header (mobile auth path).
	// If absent, this is a Bearer auth request — no HMAC needed.
	// JWT SecurityPolicy handles Bearer validation separately.
	xToken, err := proxywasm.GetHttpRequestHeader("x-token")
	if err != nil || xToken == "" {
		return types.ActionContinue
	}

	// X-Token present — validate required device headers.
	headers := make(map[string]string, len(requiredDeviceHeaders))
	for _, h := range requiredDeviceHeaders {
		val, _ := proxywasm.GetHttpRequestHeader(h)
		headers[h] = strings.TrimSpace(val)
	}

	var missing []string
	for _, h := range requiredDeviceHeaders {
		if headers[h] == "" {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		msg := `{"error":"missing required headers: ` + strings.Join(missing, ", ") + `"}`
		_ = proxywasm.SendHttpResponse(401, jsonContentType(), []byte(msg), -1)
		return types.ActionPause
	}

	// Validate timestamp freshness.
	timestamp := headers["x-timestamp"]
	parsedTime, parseErr := time.Parse(time.RFC3339Nano, timestamp)
	if parseErr != nil {
		_ = proxywasm.SendHttpResponse(400, jsonContentType(),
			[]byte(`{"error":"invalid timestamp format: use RFC3339Nano"}`), -1)
		return types.ActionPause
	}

	now := time.Now().UTC()
	age := now.Sub(parsedTime)
	if age > maxTimestampAge || age < -maxClockSkew {
		proxywasm.LogWarnf("hmac-filter: timestamp rejected age=%v", age)
		_ = proxywasm.SendHttpResponse(403, jsonContentType(),
			[]byte(`{"error":"timestamp expired"}`), -1)
		return types.ActionPause
	}

	// Validate HMAC-SHA256 signature.
	signature := headers["x-signature"]
	expectedSig := generateHMAC(ctx.hmacSecret, xToken+":"+timestamp)

	receivedMAC, decErr := hex.DecodeString(strings.TrimSpace(signature))
	if decErr != nil {
		_ = proxywasm.SendHttpResponse(400, jsonContentType(),
			[]byte(`{"error":"invalid signature format"}`), -1)
		return types.ActionPause
	}

	expectedMAC, _ := hex.DecodeString(expectedSig)

	// Constant-time comparison to prevent timing attacks.
	if !hmac.Equal(receivedMAC, expectedMAC) {
		proxywasm.LogWarnf("hmac-filter: signature mismatch")
		_ = proxywasm.SendHttpResponse(403, jsonContentType(),
			[]byte(`{"error":"invalid signature"}`), -1)
		return types.ActionPause
	}

	return types.ActionContinue
}

// --- Helpers ---

// generateHMAC produces an HMAC-SHA256 hex signature.
func generateHMAC(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// jsonContentType returns the content-type header for JSON responses.
func jsonContentType() [][2]string {
	return [][2]string{{"content-type", "application/json"}}
}
