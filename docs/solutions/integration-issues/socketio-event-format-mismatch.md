---
title: Socket.IO Event Format Mismatch — camelCase vs snake_case
category: integration-issues
component: realtime, socket.io, mobile
severity: high
date_solved: 2026-03-27
symptoms:
  - "SocketService: Failed to parse event (agent.typing) — type 'Null' is not a subtype of type 'String'"
  - "SocketService: Failed to parse event (message.new) — type 'Null' is not a subtype of type 'Map<String, dynamic>'"
  - Socket.IO events received but not parsed by mobile client
tags: [socket.io, realtime, flutter, freezed, json, snake_case]
---

# Socket.IO Event Format Mismatch

## Problem

Mobile client received Socket.IO events but failed to parse them with null cast errors. Two separate issues:

1. **Nested wrapper**: Backend sent `{event: "agent.typing", data: {conversationId, agentId, isTyping}}` but mobile Freezed union expected flat payloads `{conversationId, agentId, isTyping}`.

2. **Case mismatch**: Backend used `camelCase` JSON keys (`conversationId`, `agentId`, `isTyping`) but mobile's `build.yaml` has `field_rename: snake`, so `json_serializable` expected `conversation_id`, `agent_id`, `is_typing`.

## Root Cause

### Issue 1: Nested Wrapper

The Go realtime types wrapped payloads unnecessarily:

```go
// WRONG — nested {event, data} wrapper
type AgentTypingPayload struct {
    Event string          `json:"event"`
    Data  AgentTypingData `json:"data"`
}
```

Mobile's `_onEvent` callback receives the raw Socket.IO argument. It adds `runtimeType` from the event name and passes to Freezed `fromJson`. The nested `data` object meant top-level fields like `conversationId` were null.

### Issue 2: Case Mismatch

```go
// WRONG — camelCase
ConversationID string `json:"conversationId"`

// CORRECT — snake_case matching mobile build.yaml
ConversationID string `json:"conversation_id"`
```

## Solution

Flattened payloads and switched to snake_case in `internal/pkg/realtime/types.go`:

```go
type AgentTypingPayload struct {
    ConversationID string `json:"conversation_id"`
    AgentID        string `json:"agent_id"`
    IsTyping       bool   `json:"is_typing"`
}

type AgentStatusPayload struct {
    AgentID string `json:"agent_id"`
    Status  string `json:"status"`
}

type MessageEventPayload struct {
    Message any `json:"message"`
}
```

Updated `SocketIOBroadcaster` to emit flat payloads directly instead of wrapping in `{event, data}`.

## Investigation Steps

1. Mobile logs showed `Failed to parse event (agent.typing) — type 'Null' is not a subtype of type 'String'`
2. Raw data dump revealed nested `{event, data}` wrapper
3. Checked mobile `_onEvent` — it spreads raw data + adds `runtimeType`, then calls `fromJson`
4. Checked Freezed `SocketEvent` union — expects flat fields at top level
5. Checked mobile `build.yaml` — `field_rename: snake` means all JSON keys must be snake_case
6. Fixed both issues: flattened payloads + snake_case keys

## Prevention

- Always check the mobile's `build.yaml` for `field_rename` setting before defining JSON field names
- Socket.IO event payloads should be flat — the event name is already separate from the data
- When adding new realtime events, verify format with `log('Raw data: $data')` on mobile before writing the parser

## Related

- `internal/pkg/realtime/types.go` — event payload types
- `internal/orchestrator/server/socketio.go` — SocketIOBroadcaster emit methods
- `crawbl-mobile/lib/services/socket/socket_service.dart` — mobile event parsing
- `crawbl-mobile/lib/services/socket/socket_event.dart` — Freezed union definition
