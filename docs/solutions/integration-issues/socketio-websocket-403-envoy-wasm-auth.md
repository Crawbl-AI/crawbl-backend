---
title: Socket.IO WebSocket Upgrade Blocked by WASM Auth Filter (403)
category: integration-issues
component: envoy-auth-filter, socket.io, envoy-gateway
severity: high
date_solved: 2026-03-28
symptoms:
  - "WebSocketException: Connection was not upgraded to websocket, HTTP status code: 403"
  - Mobile Socket.IO clients cannot establish real-time connections
  - WASM auth filter rejects upgrade due to missing HMAC device headers
  - SecurityPolicy JWT validation never reached because WASM filter runs first
tags: [socketio, websocket, envoy-gateway, wasm-filter, auth, 403, mobile]
---

# Socket.IO WebSocket Upgrade Blocked by WASM Auth Filter (403)

## Problem

Mobile Socket.IO clients received HTTP 403 when trying to connect via WebSocket to `https://dev.api.crawbl.com/socket.io/`. The error was:

```
WebSocketException: Connection to 'https://dev.api.crawbl.com:0/socket.io/?workspaceId=...&EIO=4&transport=websocket#' was not upgraded to websocket, HTTP status code: 403
```

## Root Cause

The WASM auth filter (`cmd/envoy-auth-filter/main.go`) implements this request flow in `OnHttpRequestHeaders`:

1. Skip if local/test environment
2. Check for `X-Token` header — if present, validate HMAC device headers (`x-timestamp`, `x-signature`, `x-device-info`, `x-version`)
3. Missing HMAC headers → 401, expired timestamp → 403, bad HMAC signature → 403

The mobile Socket.IO client sends `X-Token` (Firebase JWT) during the WebSocket upgrade handshake but does **not** send HMAC device headers — those are only attached to REST API calls. Because `X-Token` was present but the HMAC device headers were absent, the filter rejected the upgrade with 403.

**Execution order matters:** The WASM filter runs before the SecurityPolicy JWT validation. Since the WASM filter rejected the request, JWT validation never ran — even though the JWT config was correct.

## Solution

Added a WebSocket upgrade bypass **before** the `X-Token` check in `OnHttpRequestHeaders`:

```go
// Skip HMAC for WebSocket upgrade requests.
// Socket.IO connections use JWT validation (SecurityPolicy) at the edge
// and the Socket.IO server's own auth middleware as backstop.
if upgrade, err := proxywasm.GetHttpRequestHeader("upgrade"); err == nil && strings.EqualFold(upgrade, "websocket") {
    return types.ActionContinue
}
```

Inserted after the environment skip check and before the `X-Token`/HMAC validation logic.

**Auth layers after the fix:**

| Layer | WebSocket Upgrade | REST API Call |
|---|---|---|
| WASM auth filter | Skip (passes through) | HMAC device header validation |
| SecurityPolicy | JWT validated from X-Token | JWT validated from X-Token |
| Socket.IO middleware | Reads X-Firebase-UID header | N/A (HTTP handlers) |

WebSocket connections remain fully authenticated — only the HMAC device-header check (REST-specific) is bypassed.

## Investigation Steps

1. Mobile logs showed `HTTP status code: 403` on the Socket.IO WebSocket upgrade attempt
2. Checked SecurityPolicy configuration — it returns 401 for JWT validation failures, not 403, ruling it out
3. Inspected the WASM filter code — confirmed it returns 403 for timestamp expiry and signature mismatch
4. Confirmed the Socket.IO client sends `X-Token` during the upgrade handshake but omits HMAC device headers
5. Added the WebSocket `Upgrade` header bypass before the `X-Token` check — fix confirmed

## Prevention

- **Enumerate all request types at design time.** Before writing edge auth filters, list every protocol that will traverse the listener: REST, WebSocket upgrades, gRPC, SSE, health probes, CORS preflights. Each has distinct header semantics.
- **Treat `Upgrade: websocket` as a first-class code path.** WebSocket upgrades look like normal HTTP requests but carry different headers. Any Envoy filter must check for upgrades early and branch accordingly.
- **Map auth responsibility per request type.** Document which layer (WASM filter, SecurityPolicy, application middleware) handles auth for each request type. Overlap and gaps both cause failures.
- **Test WebSocket upgrades through the full auth pipeline.** Unit test the WASM filter with simulated upgrade requests. Integration test with a real WebSocket client (wscat, websocat) through Envoy.
- **Log filter decisions.** When the WASM filter skips or rejects a request, log the reason. Envoy access logs alone won't tell you which filter caused a 403.

## Related

- `cmd/envoy-auth-filter/main.go` — WASM auth filter source
- `crawbl-docs/docs/security/wasm-auth-filter.md` — Auth filter documentation
- `docs/solutions/integration-issues/socketio-event-format-mismatch.md` — Related Socket.IO issue (event format)
- `crawbl-argocd-apps/components/platform-resources/resources/envoy-extension-policy.yaml` — Filter deployment config
- `crawbl-argocd-apps/components/orchestrator/resources/security-policy.yaml` — JWT SecurityPolicy
