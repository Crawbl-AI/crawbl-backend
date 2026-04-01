---
title: "Fix E2E Tests After Native Delegate Agents Integration"
type: fix
date: 2026-04-01
---

# Fix E2E Tests After Native Delegate Agents Integration

## Overview

The native delegate agents PR (`a8e84e2`) and subsequent roles-to-slugs refactor (`a4ce39c`) changed how swarm messages are routed and how agent identity is exposed in API responses. The e2e test suite now fails because several scenarios expect every agent reply to carry agent metadata (`data.agent.id`, `data.agent.name`, `data.agent.slug`), but swarm conversation replies no longer attach an agent when the Manager (base agent) handles the message.

## Problem Statement

**Before** the native delegate agents change:
- `resolveResponder()` returned `agents[0]` (first agent) for swarm conversations with no @-mention
- Every reply message had an agent attached, so `data.agent.*` fields were always populated
- Two hardcoded agents existed: researcher + writer

**After** the change:
- `resolveResponder()` returns `nil` for swarm conversations with no @-mention (correct behavior — Manager handles it)
- `persistMessagePair()` sets `replyAgentID = nil` when responder is nil
- The reply message has no `Agent` object → `data.agent` is omitted from JSON
- Only one agent exists now: Wally (slug: `wally`)

**Result**: Any test step that asserts `data.agent.id` / `data.agent.name` / `data.agent.role` is non-empty fails for swarm conversation messages.

## Root Cause Analysis

The issue is in `internal/orchestrator/service/chatservice/messages.go:52-68`:

```go
responder := resolveResponder(conversation, agents, opts.Mentions)

if responder != nil {
    sendOpts.AgentID = responder.Slug
}
// ...
var replyAgentID *string
if responder != nil {
    replyAgentID = &responder.ID
}
```

When the user sends a message in the **swarm conversation** with no @-mention:
1. `resolveResponder()` returns `nil` (correct — Manager handles it)
2. No `agent_id` sent to ZeroClaw (correct — base agent receives it)
3. Reply persisted with `AgentID = nil` → no agent on the response

But ZeroClaw's Manager internally **delegates to Wally** for most tasks. The backend has no way to know which sub-agent actually handled the request because the webhook response (`{"response": "..."}`) doesn't include agent attribution.

## Affected Tests

### Failing (Category A: Missing agent on swarm replies)

| Feature | Scenario | Failing Step | Why |
|---------|----------|--------------|-----|
| `06_chat.feature` | "A user can send a message once the assistant is ready" | `the assistant reply should come from an agent` | Swarm message → nil responder → no agent on reply |
| `09_agent_communication.feature` | "The assistant answers a real planning request" | `the assistant reply should come from an agent` | Same |
| `09_agent_communication.feature` | "Reply metadata identifies the responding agent" | `the assistant reply should come from an agent` | Same |

### Passing (unchanged routing)

| Feature | Why it still works |
|---------|-------------------|
| `10_mentions.feature` | @-mention sets `mentions[0].AgentID` → `resolveResponder` returns Wally |
| `11_agent_conversations.feature` | Direct conversation has `conversation.AgentID` → returns Wally |
| `12_mobile_first_launch.feature` | Only checks `reply should succeed` + `contain text`, not agent metadata |
| `01-05, 07-08, 13-14` | No chat assertions that check agent metadata |

### Potential Runtime Failures (Category B: Config/deployment issues)

If the `[agents.wally]` TOML section has issues (missing `provider`, malformed config), the ZeroClaw pod may fail to start, causing:

| Feature | Scenario | Failing Step | Why |
|---------|----------|--------------|-----|
| `06_chat.feature` | All scenarios | `waits until their assistant is ready` | Runtime never reaches `verified=true` |
| `09_agent_communication.feature` | All scenarios | Same | Same |
| `12_mobile_first_launch.feature` | "A new user completes first launch..." | Same | Same |

## Proposed Solution

**Two-pronged fix: backend default attribution + test alignment.**

### Fix 1: Default Agent Attribution for Swarm Replies (Backend)

When `resolveResponder` returns `nil` for a swarm conversation, the reply should still be attributed to the first available agent for display purposes. The mobile app expects every agent-role message to have agent metadata — this is the API contract.

**File:** `internal/orchestrator/service/chatservice/messages.go`

```go
// After resolving the responder, default to first agent for swarm attribution.
responder := resolveResponder(conversation, agents, opts.Mentions)
if responder == nil && len(agents) > 0 {
    // Swarm conversation, no mention — Manager handles it, but attribute
    // the reply to the first agent for mobile display consistency.
    // The Manager delegates to sub-agents internally; we surface the
    // primary agent as the responder since the webhook doesn't report
    // which sub-agent actually handled the request.
    responder = agents[0]
}
```

**Why this is correct:**
- Wally is the only sub-agent; Manager delegates to him for all tasks
- The mobile app contract requires agent metadata on every agent message
- ZeroClaw's webhook response doesn't include agent attribution
- When more agents are added later, a smarter heuristic can replace `agents[0]`

**What NOT to send:** The `agent_id` in the webhook should still be empty for swarm messages (Manager should handle routing). Only the reply attribution changes:

```go
// Route to the correct agent for the webhook
webhookResponder := resolveResponder(conversation, agents, opts.Mentions)

// For display: attribute reply to an agent even for swarm messages
displayResponder := webhookResponder
if displayResponder == nil && len(agents) > 0 {
    displayResponder = agents[0]
}

// Call ZeroClaw with webhook routing (nil = Manager handles)
sendOpts := &userswarmclient.SendTextOpts{
    Runtime:   runtimeState,
    Message:   opts.Content.Text,
    SessionID: conversation.ID,
}
if webhookResponder != nil {
    sendOpts.AgentID = webhookResponder.Slug
}

// ... after getting reply ...

// Persist with display attribution
return s.persistMessagePair(ctx, opts, conversation, agents, displayResponder, replyText)
```

### Fix 2: Test Assertion Alignment

Even with Fix 1, the tests should be more precise about what they check:

**`06_chat.feature`** — Keep `the assistant reply should come from an agent` (now passes with Fix 1)

**`09_agent_communication.feature`** — Keep as-is (now passes with Fix 1). Consider renaming to better reflect what's tested (swarm-level agent replies).

No feature file changes needed if Fix 1 is implemented.

### Fix 3: Verify Runtime Config (Diagnostic)

Before running e2e, verify the deployed config is correct:

```bash
# Check a running pod's config.toml has valid [agents.wally]
kubectl exec -n userswarms <pod> -c zeroclaw -- cat /zeroclaw-data/workspace/config.toml | grep -A 5 'agents.wally'

# Check pod is running (not CrashLoopBackOff)
kubectl get pods -n userswarms --field-selector=status.phase!=Running

# Check UserSwarm CR status
kubectl get userswarms -A -o wide
```

If pods are crashing, the TOML config is likely the issue:
- Verify `provider` is present (required by ZeroClaw's Rust deserializer)
- Verify `model` is present
- Check `BuildConfigTOML()` fills defaults from workspace spec

## Implementation

### Phase 1: Diagnose (30 min)

Run the e2e suite to capture exact failure output:

```bash
make test-e2e 2>&1 | tee /tmp/e2e-output.txt
```

Categorize failures:
- **Category A** (missing agent): Fix 1 resolves these
- **Category B** (runtime timeout): Fix 3 diagnostics needed first

### Phase 2: Backend Fix (1 file, ~15 lines)

**`internal/orchestrator/service/chatservice/messages.go`:**

Split `resolveResponder` result into webhook routing vs display attribution:
- Webhook routing: `nil` for swarm (Manager handles) — unchanged
- Display attribution: first agent for swarm — new

Refactor the signal/persist flow to use `displayResponder` for:
- `EmitAgentStatus` / `EmitAgentTyping` broadcasts
- `persistMessagePair` agent attribution

And `webhookResponder` for:
- `sendOpts.AgentID` (what gets sent to ZeroClaw)

### Phase 3: Verify (10 min)

Run the full e2e suite again:

```bash
make test-e2e
```

All 14 feature files should pass. Key scenarios to watch:
- `06_chat.feature` — swarm message now has agent metadata ✓
- `09_agent_communication.feature` — same ✓
- `10_mentions.feature` — mention still routes to Wally ✓
- `11_agent_conversations.feature` — direct conversation still works ✓
- `12_mobile_first_launch.feature` — full journey passes ✓

## Files Changed

| File | Change | Lines |
|------|--------|-------|
| `internal/orchestrator/service/chatservice/messages.go` | Split responder into webhook vs display; default display to `agents[0]` for swarm | ~15 lines |

## Acceptance Criteria

- [ ] `make test-e2e` passes all 14 feature files
- [ ] Swarm conversation replies include `data.agent.id`, `data.agent.name`, `data.agent.slug` (attributed to Wally)
- [ ] @-mention routing still works (Wally responds when @-mentioned)
- [ ] Direct agent conversation routing unchanged
- [ ] ZeroClaw webhook still receives no `agent_id` for swarm messages (Manager handles)
- [ ] No changes to feature files — the existing test expectations are correct

## Risk Analysis

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Fix 1 masks a real routing bug where Manager fails to delegate | Low | Medium | Monitor ZeroClaw logs for delegation failures; add agent attribution to webhook response later |
| Runtime pods crashing due to TOML config issues | Medium | High | Phase 1 diagnostic; verify config.toml on a live pod before code changes |
| `agents[0]` heuristic breaks when more agents are added | Low | Low | Replace with explicit "default display agent" config when the second agent ships |
| Existing test users have stale researcher/writer agents in DB | Low | Low | `ensureDefaultAgents` upserts by slug; old agents remain but are harmless |

## Future Considerations

- **Webhook agent attribution**: ZeroClaw could return `{"response": "...", "agent": "wally"}` to tell the backend which sub-agent actually handled the request. This would make attribution precise instead of heuristic.
- **Multi-agent display**: When more agents are added, the default attribution heuristic (`agents[0]`) should be replaced with explicit logic or ZeroClaw-side attribution.
- **Manager as a visible agent**: If the product wants to show "Manager answered this", add a Manager row to the agents table. Currently Manager is invisible to the mobile app.

## References

### Changed by Native Delegate Agents PR

| File | Key Change |
|------|------------|
| `internal/orchestrator/service/chatservice/agents.go:50-52` | `resolveResponder` returns `nil` for swarm (was `agents[0]`) |
| `internal/orchestrator/service/chatservice/messages.go:67` | Uses `responder.Slug` instead of `responder.Role` for `agent_id` |
| `internal/orchestrator/types.go:601` | `DefaultAgents` now only Wally |

### Test Infrastructure

| File | Purpose |
|------|---------|
| `internal/testsuite/e2e/steps_product.go:545-556` | `assistantReplyShouldComeFromAgent` — checks `data.agent.*` fields |
| `internal/testsuite/e2e/steps_product.go:558-559` | `assistantReplyShouldComeFromSpecificAgent` — checks `data.agent.slug` |
| `test-features/06_chat.feature:16` | "the assistant reply should come from an agent" |
| `test-features/09_agent_communication.feature:13,19` | Same assertion |
