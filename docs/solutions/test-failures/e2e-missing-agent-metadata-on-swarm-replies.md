---
title: "E2E Tests Fail: Missing Agent Metadata on Swarm Conversation Replies"
category: test-failures
tags: [e2e, godog, agent-routing, swarm, chat, native-delegate-agents]
module: chatservice
symptoms:
  - "e2e assertion `the assistant reply should come from an agent` fails"
  - "data.agent.id is empty on swarm message replies"
  - "06_chat.feature and 09_agent_communication.feature fail"
date_solved: 2026-04-01
severity: high
---

# E2E Tests Fail: Missing Agent Metadata on Swarm Conversation Replies

## Problem

After merging the **native delegate agents** PR (`a8e84e2`) and the **roles-to-slugs refactor** (`a4ce39c`), e2e tests that assert agent metadata on chat replies fail for swarm conversations.

**Error observed:**
```
JSON data.agent.id: expected non-empty value
```

**Affected features:**
- `06_chat.feature` — "A user can send a message once the assistant is ready"
- `09_agent_communication.feature` — "The assistant answers a real planning request"
- `09_agent_communication.feature` — "Reply metadata identifies the responding agent"

## Root Cause

`resolveResponder()` in `internal/orchestrator/service/chatservice/agents.go` was changed to return `nil` for swarm conversations with no @-mention. This is **correct for webhook routing** — the Manager (ZeroClaw's base agent) should handle swarm messages without an explicit `agent_id`.

However, `SendMessage()` in `messages.go` used the same `responder` variable for both:
1. **Webhook routing** — what `agent_id` to send to ZeroClaw (nil = Manager handles)
2. **Reply attribution** — what agent metadata to persist on the reply message

When `responder` was nil, the reply was persisted with `AgentID = nil`, so the JSON response omitted `data.agent` entirely. The mobile app and e2e tests expect every agent-role message to carry agent metadata.

**Before (broken):**
```
resolveResponder() → nil (swarm, no mention)
    → sendOpts.AgentID = "" (correct — Manager handles)
    → replyMessage.AgentID = nil (broken — no agent on response)
    → data.agent is omitted from JSON
    → e2e "come from an agent" assertion fails
```

## Solution

Split the responder into two variables:
- `responder` — webhook routing (unchanged, nil for swarm)
- `displayResponder` — reply attribution (falls back to `agents[0]` for swarm)

**File:** `internal/orchestrator/service/chatservice/messages.go`

```go
// Route to the correct agent for the ZeroClaw webhook.
responder := resolveResponder(conversation, agents, opts.Mentions)

// For display: attribute the reply to an agent even for swarm messages.
displayResponder := responder
if displayResponder == nil && len(agents) > 0 {
    displayResponder = agents[0]
}

// Webhook uses original responder (nil = Manager handles)
if responder != nil {
    sendOpts.AgentID = responder.Slug
}

// Broadcaster signals and persistence use displayResponder
s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, displayResponder.ID, ...)
return s.persistMessagePair(ctx, opts, conversation, agents, displayResponder, replyText)
```

**Why `agents[0]`?** Wally is the only sub-agent. The Manager delegates to Wally internally, but ZeroClaw's webhook response (`{"response": "..."}`) doesn't include which sub-agent handled the request. Attributing to `agents[0]` is the correct heuristic for now.

## What Stays Unchanged

- `resolveResponder()` still returns nil for swarm — correct for webhook routing
- @-mention routing works as before (mention → specific agent)
- Direct agent conversations work as before (conversation.AgentID → agent)
- No feature file or test code changes needed

## Prevention

- **When changing routing logic**, verify that the API response contract (agent metadata on messages) is maintained. The mobile app depends on `data.agent.*` being present on every agent-role message.
- **Separate concerns** when a single variable serves multiple purposes (webhook routing vs display attribution). Name them explicitly.
- **The e2e suite catches this** — the `assistantReplyShouldComeFromAgent` step is the guard. Don't weaken it.

## Related

- Plan: `docs/plans/2026-03-31-feat-native-delegate-agents-plan.md`
- Plan: `docs/plans/2026-04-01-fix-e2e-tests-after-native-delegate-agents-plan.md`
- Commit: `a8e84e2` (native delegate agents merge)
- Commit: `a4ce39c` (roles-to-slugs refactor)
- Fix commit: `c91129c` (displayResponder fix)

## Future Note

When more agents are added, the `agents[0]` heuristic should be replaced with either:
1. ZeroClaw webhook returning `{"response": "...", "agent": "wally"}` for precise attribution
2. An explicit "default display agent" configuration
