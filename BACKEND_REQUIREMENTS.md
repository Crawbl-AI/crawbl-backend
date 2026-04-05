# Backend Requirements — Swarm and Agent Systems

Complete product and technical specification for the Crawbl backend, covering the agent swarm, real-time messaging, delegation, workflows, and artifacts.

This document is written for a backend engineer building from scratch. It defines what each feature is, why it exists, and the exact contracts the mobile app consumes.

---

## Table of Contents

1. [Product Context](#1-product-context)
2. [Architecture Overview](#2-architecture-overview)
3. [Agent System](#3-agent-system)
4. [Conversations](#4-conversations)
5. [Messaging](#5-messaging)
6. [Real-Time Transport (Socket.IO)](#6-real-time-transport-socketio)
7. [Streaming Pipeline](#7-streaming-pipeline)
8. [Message Delivery Status](#8-message-delivery-status)
9. [Agent Activity Indicators](#9-agent-activity-indicators)
10. [Agent Tools](#10-agent-tools)
11. [Agent Delegation](#11-agent-delegation)
12. [Artifacts](#12-artifacts)
13. [Workflows](#13-workflows)
14. [Agent Memory](#14-agent-memory)
15. [Enums Reference](#15-enums-reference)
16. [REST API Contract](#16-rest-api-contract)
17. [Socket.IO Event Contract](#17-socketio-event-contract)
18. [Implementation Priorities](#18-implementation-priorities)

---

## 1. Product Context

Crawbl is a mobile-first AI swarm platform. Each user gets a private swarm of AI agents that can search the web, manage integrations, coordinate tasks, and learn over time.

The system has three layers:

- **Mobile app (Flutter)** — the consumer interface. Chat, agent profiles, swarm visualizer.
- **Go orchestrator (backend)** — auth, provisioning, message routing, LLM mediation, integration adapters, real-time event fan-out.
- **Cortex Layer | runtime** — a ~5 MB Rust binary running in an isolated Kubernetes pod per user. Executes agent prompts, tools, and memory.

The orchestrator is the control plane. It decides which agent should respond, routes messages, manages OAuth tokens, and fans out real-time events. The agent runtime never talks to the mobile app directly — every interaction is mediated by the orchestrator.

The mobile app communicates with the orchestrator via REST (CRUD operations) and Socket.IO (real-time events and streaming).

---

## 2. Architecture Overview

```
Mobile App  <--REST/Socket.IO-->  Go Orchestrator  <--internal HTTP-->  Isolated Pod
                                       |                                    |
                                  PostgreSQL                          PVC (memory/sessions)
                                  Redis (pub/sub)
```

Key principles:

- **The orchestrator is the single entry point.** No direct pod-to-pod or client-to-runtime access.
- **Each user gets an isolated runtime.** One UserSwarm CR = one Isolated pod with its own storage.
- **Agents are real entities.** Each swarm has a Manager (base agent) plus named delegate agents (e.g. "Wally") defined in Somewhere.
- **The orchestrator decides WHO answers.** The runtime owns WHAT that agent is (personality, tools, memory).

---

## 3. Agent System

### What is an Agent?

An agent is a named AI personality within a user's swarm. Each agent has its own identity, skills, tools, memory, and LLM configuration. Agents are not generic chatbots — they are specialists.

### Agent Roles

| Role        | Description                                                                                                         |
| ----------- | ------------------------------------------------------------------------------------------------------------------- |
| `manager`   | The base swarm agent. Handles messages when no specific agent is targeted. Coordinates and delegates to sub-agents. |
| `sub-agent` | A specialist delegate (e.g. "Wally" for web research). Has its own system prompt, allowed tools, and skill files.   |

The Manager + sub-agent model is the current design direction, not a hard constraint. The routing and delegation architecture should be flexible enough to support alternative topologies (e.g. peer agents without a central manager, or dynamic role assignment) if the product requires it. This document describes the target vision — the implementation should aim for the best possible solution.

### Agent Lifecycle

Each agent belongs to a workspace. When the workspace runtime is provisioned, agents become available. The orchestrator stores agent metadata (name, slug, role, avatar, status) in PostgreSQL and exposes them via REST.

### Agent Profile

An agent has a summary (for lists) and a detail view (for the profile screen):

**Summary** (used in conversation lists, message bubbles, mentions):

```json
{
  "id": "uuid",
  "name": "Wally",
  "role": "sub-agent",
  "slug": "wally",
  "avatar": "wally_avatar",
  "status": "online"
}
```

**Detail** (used in the agent profile screen):

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "name": "Wally",
  "role": "sub-agent",
  "slug": "wally",
  "description": "Web research specialist with real-time search capabilities.",
  "avatar_url": "https://...",
  "status": "online",
  "sort_order": 1,
  "created_at": "ISO8601",
  "updated_at": "ISO8601",
  "stats": {
    "total_messages": 142
  }
}
```

### Agent Settings

Each agent has configurable LLM settings and system prompt files:

```json
{
  "model": "sonnet",
  "response_length": "auto",
  "prompts": [
    {
      "id": "uuid",
      "name": "IDENTITY.md",
      "description": "Core identity and personality",
      "content": "You are Wally, a web research specialist..."
    }
  ]
}
```

`model` is one of: `flash`, `sonnet`, `opus`. These map to the LLM model the agent uses.
`response_length` is one of: `auto`, `short`, `medium`, `long`. Controls response verbosity.
`prompts` are the system prompt files (IDENTITY.md, TOOLS.md, SOUL.md) that define the agent's behavior.

---

## 4. Conversations

### What is a Conversation?

A conversation is a message thread between a user and one or more agents. It belongs to a workspace.

### Conversation Types

| Type    | Description                                                                                                                 |
| ------- | --------------------------------------------------------------------------------------------------------------------------- |
| `swarm` | The user talks to the entire swarm. The Manager agent routes to the appropriate sub-agent. Multiple agents may participate. |
| `agent` | A 1:1 chat between the user and a specific agent. Only that agent responds.                                                 |

### Conversation Shape

```json
{
  "id": "uuid",
  "type": "swarm",
  "title": "Trip planning",
  "created_at": "ISO8601",
  "updated_at": "ISO8601",
  "unread_count": 3,
  "agent": null,
  "last_message": { "...MessageData" }
}
```

- `agent` is null for swarm conversations, populated for agent conversations.
- `unread_count` tracks messages the user hasn't seen. Decremented when the user marks the conversation as read.
- `last_message` is the most recent message (used for conversation list preview).

### Creating a Conversation

The mobile app needs to create new conversations. This is currently the **#1 blocker** — without it, users cannot start new chats.

```
POST /v1/workspaces/{workspaceId}/conversations
```

```json
{
  "type": "swarm | agent",ZeroClawZeroClaw
  "agent_id": "uuid | null"
}
```

- `agent_id` is required for `agent` type, null for `swarm`.
- Response: the created `ConversationData` object.
- The backend should create a default swarm conversation when a workspace is first provisioned.

---

## 5. Messaging

### What is a Message?

A message is a single entry in a conversation. It has a sender (user, agent, or system), content (text, action card, tool status, etc.), delivery status, and optional attachments/mentions.

### Message Shape

```json
{
  "id": "uuid",
  "conversation_id": "uuid",
  "role": "user | agent | system",
  "content": { "type": "text", "text": "Hello!" },
  "status": "delivered",
  "created_at": "ISO8601",
  "updated_at": "ISO8601",
  "local_id": "client-uuid | null",
  "agent": { "...AgentData | null" },
  "attachments": [],
  "mentions": []
}
```

- `role`: who sent the message.
- `local_id`: client-generated UUID for optimistic send matching. The client creates a message with a local_id, sends it, and expects the backend to echo it back so the client can match and replace the optimistic placeholder.
- `agent`: populated when `role=agent`. Contains the agent summary (id, name, slug, avatar, status).
- `attachments`: file/image/audio/video attachments.
- `mentions`: @-mentions of agents within the text (`agent_id`, `agent_name`, `offset`, `length`).

### Message Content Types

Content is a discriminated union on the `type` field. The backend must support all of these in REST responses and socket events:

#### `text` — Plain text message

The primary content type. Supports markdown rendering on the client.

```json
{ "type": "text", "text": "Here's what I found..." }
```

#### `action_card` — Interactive approval card

Used when an agent needs user approval before taking an action (e.g. sending an email, booking a meeting). The card shows a title, description, and action buttons. Once the user taps a button, `selected_action_id` is set and the card becomes read-only.

```json
{
  "type": "action_card",
  "title": "Send email to John?",
  "description": "Subject: Meeting tomorrow at 3pm\nBody: Hi John, confirming our meeting...",
  "actions": [
    { "id": "approve", "label": "Send", "style": "primary" },
    { "id": "reject", "label": "Cancel", "style": "destructive" }
  ],
  "selected_action_id": null
}
```

`style` is one of: `primary`, `secondary`, `destructive`. Controls button appearance.

Why this exists: Crawbl follows an "ask-before-write" model. Agents can read from connected apps freely, but write actions (sending emails, creating events, posting messages) require explicit user approval. The action card is the approval mechanism.

#### `tool_status` — Agent tool execution indicator

Shown when an agent is actively using a tool (e.g. "Searching the web for..."). Appears as a status pill in the chat that transitions from running to completed/failed.

```json
{
  "type": "tool_status",
  "tool": "web_search",
  "description": "Searching for 'best flights to Paris'",
  "state": "running | completed | failed"
}
```

Why this exists: Users should see what their agents are doing, not just wait for a final answer. Tool status makes agent behavior transparent and builds trust.

#### `system` — System notification

Non-interactive informational message (e.g. "Agent joined the conversation", "Workspace updated").

```json
{ "type": "system", "text": "Wally joined the conversation" }
```

#### `loading` — Placeholder while agent generates

Client-side only. Shown briefly before the first streaming chunk arrives. The backend never sends this — the client synthesizes it when it knows an agent response is incoming.

```json
{ "type": "loading" }
```

#### `delegation` — Agent-to-agent delegation card

Shown when one agent delegates a task to another. Appears as a card showing the delegation flow.

```json
{
  "type": "delegation",
  "from_agent_id": "uuid",
  "to_agent_id": "uuid",
  "status": "delegating | working | completed | failed",
  "task_preview": "Research flight options to Paris under $500"
}
```

Why this exists: In a swarm, the Manager agent may delegate specific tasks to specialist sub-agents. The delegation card makes this coordination visible to the user — they can see that "Manager asked Wally to research flights" rather than just waiting for a generic response.

`status` values:

- `delegating` — Manager is handing off the task
- `working` — Sub-agent is actively working on it
- `completed` — Sub-agent finished successfully
- `failed` — Sub-agent could not complete the task

#### `artifact` — Inline artifact card

Shown when an agent creates or updates a persistent artifact (document, code file, plan, spreadsheet). Artifacts are versioned and persist beyond the conversation.

```json
{
  "type": "artifact",
  "artifact_id": "uuid",
  "title": "Paris Trip Itinerary",
  "version": 2,
  "status": "created | updated | finalized",
  "agent_slug": "wally",
  "content_preview": "Day 1: Arrive at CDG, check into hotel..."
}
```

Why this exists: Agents produce structured outputs that are more than just chat messages. A trip itinerary, a code review document, or a meeting summary should be a first-class artifact that can be versioned, updated, and referenced later — not buried in chat history.

`status` values:

- `created` — first version of the artifact
- `updated` — a new version was produced
- `finalized` — agent marked the artifact as complete

#### `workflow` — Workflow progress tracker

Shown when a multi-step workflow is executing. Displays the workflow name, current step, and progress across all steps.

```json
{
  "type": "workflow",
  "workflow_id": "uuid",
  "workflow_name": "Trip Planning",
  "execution_id": "uuid",
  "status": "running | completed | failed | cancelled",
  "steps": [
    {
      "name": "Research destinations",
      "agent_slug": "wally",
      "status": "completed",
      "step_index": 0
    },
    {
      "name": "Compare flights",
      "agent_slug": "wally",
      "status": "running",
      "step_index": 1
    },
    {
      "name": "Book hotels",
      "agent_slug": "wally",
      "status": "pending",
      "step_index": 2
    }
  ]
}
```

Why this exists: Complex tasks involve multiple steps, often handled by different agents. The workflow card gives the user a birds-eye view of progress — which steps are done, which is active, and what's coming next. This is the foundation for the swarm visualizer (Phase 2).

`status` values for the workflow: `running`, `completed`, `failed`, `cancelled`.
`status` values for each step: `pending`, `running`, `completed`, `failed`, `skipped`.

---

## 6. Real-Time Transport (Socket.IO)

### Why Socket.IO?

REST is used for CRUD operations (list conversations, fetch messages, update settings). Socket.IO handles everything that must be real-time: new messages, streaming responses, agent status changes, delivery receipts, delegation events, workflow progress.

### Connection

The client connects to namespace `/v1` at the backend's base URL. Authentication uses the same headers as REST:

| Header          | Purpose                                        |
| --------------- | ---------------------------------------------- |
| `X-Token`       | Firebase ID token                              |
| `X-Signature`   | HMAC-SHA256(`{token}:{timestamp}`, hmacSecret) |
| `X-Device-Info` | Device model/OS                                |
| `X-Device-ID`   | Unique device identifier                       |
| `X-Version`     | App version                                    |
| `X-Timezone`    | User timezone                                  |
| `X-Timestamp`   | Request timestamp                              |

### Workspace Subscription

After connecting, the client subscribes to workspace events:

```
Client emits: workspace.subscribe { "workspace_ids": ["uuid1", "uuid2"] }
Server responds: workspace.subscribed { "workspace_ids": ["uuid1", "uuid2"] }
```

No events are delivered until the workspace subscription is confirmed. This is the gating mechanism — the backend only fans out events for workspaces the client has subscribed to.

When the user switches workspaces or the app backgrounds, the client emits `workspace.unsubscribe`.

---

## 7. Streaming Pipeline

### Why Streaming?

Agent responses can be long (paragraphs of analysis, detailed plans). Without streaming, the user stares at a loading indicator for 5-30 seconds. With streaming, they see text appearing word-by-word — like watching someone type in real time.

### Flow

The complete lifecycle of a user message:

```
1. User types message, taps send
2. Client creates optimistic message (status=pending, localId=uuid)
3. Client emits socket: message.send { workspaceId, conversationId, localId, content, mentions, attachments }
4. Server acknowledges: message.send.ack { localId, messageId, status: "received" }
   → Client replaces localId with server messageId, sets status=sent
5. Server emits: agent.status { agentId, status: "reading", conversationId }
   → Client shows "Wally is reading..."
6. Server emits: agent.status { agentId, status: "thinking", conversationId }
   → Client shows "Wally is thinking..."
7. Server emits: agent.status { agentId, status: "writing", conversationId }
   → Client shows "Wally is writing..."
8. Server emits: agent.tool { agentId, conversationId, tool: "web_search", status: "running", query: "flights to Paris" }
   → Client shows tool status pill
9. Server emits: agent.tool { agentId, conversationId, tool: "web_search", status: "done" }
   → Client marks tool as completed
10. Server emits: message.chunk { messageId, conversationId, agentId, chunk: "Based on " }
    Server emits: message.chunk { messageId, conversationId, agentId, chunk: "my research, " }
    Server emits: message.chunk { messageId, conversationId, agentId, chunk: "here are the " }
    ... (many chunks)
    → Client accumulates chunks into a StreamingContent, shows progressive text with cursor
11. Server emits: message.done { messageId, conversationId, agentId, status: "delivered" }
    → Client finalizes the message as TextContent, removes cursor
12. Server emits: message.new { message: MessageData }
    → Client receives the complete final message (used for reconciliation)
13. Server emits: agent.status { agentId, status: "online" }
    → Client clears activity indicator
```

### Error Cases

**Send failed:**

```
Server emits: message.send.error { localId, error: "Rate limit exceeded" }
→ Client marks optimistic message as failed, shows retry button
```

**Agent failed mid-stream:**

```
Server emits: message.done { messageId, ..., status: "failed" }
→ Client keeps whatever text was streamed, marks message as failed
```

**Agent interrupted:**

```
Server emits: message.done { messageId, ..., status: "incomplete" }
→ Client keeps streamed text, shows "Response interrupted" label
```

**Agent chose not to respond:**

```
Server emits: message.done { messageId, ..., status: "silent" }
→ Client shows localized system message: "Wally has nothing to say"
```

### REST Fallback

If the socket is disconnected, the client falls back to REST for sending messages:

```
POST /v1/workspaces/{workspaceId}/conversations/{id}/messages
```

This is synchronous — the client waits for the response. No streaming in REST mode.

---

## 8. Message Delivery Status

### Why Delivery Status?

Telegram-style message ticks give users confidence that their messages are being processed. Without them, users repeatedly tap send or think the app is broken.

### Status Progression

```
pending → sent → delivered → read
```

| Status      | Visual           | Meaning                                          |
| ----------- | ---------------- | ------------------------------------------------ |
| `pending`   | Clock icon       | Created on client, not yet sent to server        |
| `sent`      | Single grey tick | Server received the message (`message.send.ack`) |
| `delivered` | Double grey tick | Message was delivered to the agent runtime       |
| `read`      | Double blue tick | Agent has processed/read the message             |

### Terminal Statuses

These statuses end the lifecycle — no further transitions allowed:

| Status       | Visual                               | Meaning                      |
| ------------ | ------------------------------------ | ---------------------------- |
| `failed`     | Red circle with white X              | Send failed or agent error   |
| `incomplete` | No icon, "Response interrupted" text | Agent response was cut short |
| `silent`     | No icon, system message              | Agent decided not to respond |

### Monotonic Guard

Status transitions are strictly monotonic: `pending(0) < sent(1) < delivered(2) < read(3)`. The backend must never emit a status that is lower than the current one. Terminal statuses (`failed`, `incomplete`, `silent`) cannot be overwritten by any other status.

### Socket Event

```
message.status {
  "message_id": "uuid",
  "conversation_id": "uuid",
  "status": "sent | delivered | read",
  "local_id": "uuid | null"
}
```

`local_id` is included so the client can match the status update to optimistic messages that haven't received their server ID yet (i.e. between send and ack).

### Mark as Read

When the user opens a conversation, the client calls:

```
POST /v1/workspaces/{workspaceId}/conversations/{id}/read
→ 204 No Content
```

After this, the backend should:

1. Reset `unread_count` to 0 on the conversation.
2. Emit `message.status { status: "read" }` for each previously unread message in that conversation.

---

## 9. Agent Activity Indicators

### What Are They?

A typing-indicator-style bubble showing what an agent is currently doing: "Wally is reading...", "Wally is thinking...", "Wally is writing...". Appears at the bottom of the chat, similar to messaging apps.

### Agent Statuses

| Status     | When to Emit                                  | User Sees                    |
| ---------- | --------------------------------------------- | ---------------------------- |
| `online`   | Agent is idle and available                   | Nothing (clears indicator)   |
| `reading`  | Agent received the message, loading context   | "Wally is reading..."        |
| `thinking` | Agent is reasoning/planning before responding | "Wally is thinking..."       |
| `writing`  | Agent is actively generating text (streaming) | "Wally is writing..."        |
| `pending`  | Agent runtime is starting up                  | "Wally is pending..."        |
| `error`    | Agent encountered an error                    | "Wally encountered an error" |
| `offline`  | Agent runtime is not running                  | Nothing (clears indicator)   |

### Status Lifecycle During a Response

```
user sends message
  → agent.status { status: "reading" }    // 0-2 seconds: loading context
  → agent.status { status: "thinking" }   // 2-5 seconds: LLM planning
  → agent.status { status: "writing" }    // 5+ seconds: streaming response
  → message.done { ... }                  // finished
  → agent.status { status: "online" }     // back to idle
```

### Scope

`agent.status` events have an optional `conversation_id`:

- **With conversation_id**: The status is specific to that conversation (reading/thinking/writing). The mobile app shows the activity indicator only in that conversation.
- **Without conversation_id**: The status is global (online/offline/error/pending). The mobile app updates the agent's status everywhere (agent list, conversation list, profile).

### Socket Event

```
agent.status {
  "agent_id": "uuid",
  "status": "online | reading | thinking | writing | pending | error | offline",
  "conversation_id": "uuid | null"
}
```

---

## 10. Agent Tools

### What Are Agent Tools?

Tools are capabilities an agent can invoke: web search, fetch URLs, read/write files, access connected apps, shell commands. When an agent uses a tool, the mobile app shows a real-time status indicator in the chat.

### Tool Status Events

When an agent invokes a tool during streaming:

```
agent.tool {
  "agent_id": "uuid",
  "conversation_id": "uuid",
  "tool": "web_search",
  "status": "running | done",
  "query": "best flights to Paris"
}
```

- `running` — tool was invoked, show a "Searching the web..." pill in chat.
- `done` — tool finished, mark the pill as completed.
- `query` — optional human-readable description of what the tool is doing. Empty string if not applicable.

The client synthesizes a `ToolStatusContent` message from this event. Tool messages are keyed by `"{conversationId}_{tool}"` — so if the same tool is invoked multiple times, only the latest status is shown.

### Per-Agent Tool List

Each agent has a set of available tools. The mobile app displays these on the agent profile screen.

```
GET /v1/agents/{id}/tools?limit=8&offset=0
```

```json
{
  "data": [
    {
      "name": "web_search",
      "display_name": "Web Search",
      "description": "Search the web using DuckDuckGo",
      "category": {
        "id": "uuid",
        "name": "Search",
        "image_url": "https://..."
      },
      "icon_url": "https://..."
    }
  ],
  "pagination": {
    "total": 12,
    "limit": 8,
    "offset": 0,
    "has_next": true
  }
}
```

---

## 11. Agent Delegation

### What Is Delegation?

In a swarm conversation, the Manager agent can delegate specific tasks to specialist sub-agents. For example, the Manager receives "Plan a trip to Paris under $500" and delegates the flight research to Wally (web research specialist) and the budget analysis to another agent.

### Why It Matters

Delegation is what makes a swarm more than just a single chatbot. Users see the coordination happening — which agent is doing what — rather than getting a black-box response. This transparency is a core product differentiator.

### Delegation Flow

```
1. User sends message to swarm conversation
2. Manager agent decides to delegate
3. Backend emits: agent.delegation { fromAgentId: "manager-id", toAgentId: "wally-id", status: "delegating", ... }
   → Client shows delegation card in chat
4. Backend creates a delegation MessageContent in the conversation
5. Sub-agent works on the task
6. Backend emits: agent.delegation { ..., status: "working" }
   → Client updates delegation card
7. Sub-agent completes
8. Backend emits: agent.delegation { ..., status: "completed" }
   → Client updates delegation card to completed state
```

### Socket Event

```
agent.delegation {
  "from_agent_id": "uuid",
  "to_agent_id": "uuid",
  "conversation_id": "uuid",
  "status": "delegating | working | completed | failed",
  "message_preview": "Research flight options to Paris under $500",
  "message_id": "uuid"
}
```

### Message Content

The delegation also appears as a message in the conversation with `content.type = "delegation"` (see section 5).

---

## 12. Artifacts

### What Is an Artifact?

An artifact is a persistent, versioned output created by an agent. While chat messages are ephemeral conversation turns, artifacts are structured deliverables: a trip itinerary, a code review, a meeting summary, a budget spreadsheet.

### Why Artifacts?

Without artifacts, valuable agent outputs are buried in chat history. With artifacts, users can reference, update, and version important outputs. An agent can say "I've updated your itinerary (v3)" and the user sees the artifact card update inline in chat.

### Artifact Lifecycle

```
1. Agent creates an artifact during its response
2. Backend emits: artifact.updated { artifactId, title, version: 1, action: "created", ... }
   → Client shows artifact card in chat
3. Later, agent updates the artifact
4. Backend emits: artifact.updated { artifactId, title, version: 2, action: "updated", ... }
   → Client updates the existing artifact card or shows a new one
```

### Socket Event

```
artifact.updated {
  "artifact_id": "uuid",
  "title": "Paris Trip Itinerary",
  "version": 2,
  "action": "created | updated | finalized | deleted",
  "agent_id": "uuid",
  "conversation_id": "uuid",
  "agent_slug": "wally"
}
```

### Message Content

Artifacts also appear as message content with `content.type = "artifact"` (see section 5).

---

## 13. Workflows

### What Is a Workflow?

A workflow is a multi-step task execution plan. When a user asks for something complex ("Plan a weekend trip to Paris under $500"), the Manager breaks it into steps, assigns agents, and tracks progress.

### Why Workflows?

Workflows make complex agent coordination visible. Instead of waiting 30 seconds for a single response, the user sees a progress tracker: "Step 1: Research destinations (done) > Step 2: Compare flights (running) > Step 3: Book hotels (pending)". This is the precursor to the Phase 2 swarm visualizer.

### Workflow Lifecycle

```
1. Manager creates a workflow plan
2. Backend emits: workflow.update { workflowId, status: "running", stepIndex: 0, stepName: "Research destinations", ... }
   → Client shows workflow progress card
3. Each step progresses:
   Backend emits: workflow.update { ..., stepIndex: 1, stepName: "Compare flights", status: "running" }
   → Client updates the progress tracker
4. Workflow completes:
   Backend emits: workflow.update { ..., status: "completed" }
   → Client shows all steps as completed
```

### Socket Event

```
workflow.update {
  "workflow_id": "uuid",
  "execution_id": "uuid",
  "workflow_name": "Trip Planning",
  "status": "running | completed | failed | cancelled",
  "conversation_id": "uuid",
  "step_index": 1,
  "step_name": "Compare flights",
  "agent_slug": "wally",
  "error": ""
}
```

`error` is empty unless `status=failed`, in which case it contains a human-readable error message.

### Message Content

Workflows also appear as message content with `content.type = "workflow"` (see section 5). The message content version includes the full `steps` array for rendering the progress tracker.

---

## 14. Agent Memory

### What Is Agent Memory?

Each agent maintains persistent key-value memories. Memories are facts the agent has learned about the user or context it should remember across conversations: "User prefers window seats", "User's budget for travel is usually $500-1000", "User's timezone is UTC+4".

### Why Memory?

Memory is what makes agents get better over time. A new agent is generic. After a week of use, the agent knows the user's preferences, habits, and context. This creates organic switching costs — the longer you use Crawbl, the more valuable your swarm becomes.

### Memory Shape

```json
{
  "key": "travel_preference_seats",
  "content": "User prefers window seats on flights",
  "category": "core",
  "created_at": "ISO8601",
  "updated_at": "ISO8601"
}
```

- `key`: unique identifier for the memory within the agent.
- `content`: the actual memory content (plain text).
- `category`: grouping for memories. Currently `core` is the primary category.

### CRUD Operations

**List memories** (paginated):

```
GET /v1/agents/{id}/memories?limit=20&offset=0&category=core
```

```json
{
  "data": [ ...AgentMemory ],
  "pagination": {
    "total": 42,
    "limit": 20,
    "offset": 0,
    "has_next": true
  }
}
```

**Create a memory:**

```
POST /v1/agents/{id}/memories
```

```json
{
  "key": "travel_preference_seats",
  "content": "User prefers window seats",
  "category": "core"
}
```

Response: the created `AgentMemory` object.

**Delete a memory:**

```
DELETE /v1/agents/{id}/memories/{key}
→ 204 No Content
```

---

## 15. Enums Reference

All enums used across the system. The backend must serialize these as lowercase snake_case strings.

### AgentStatus

```
online, reading, thinking, writing, pending, error, offline
```

### AgentRole

```
manager, sub-agent
```

Note: `sub-agent` uses a hyphen, serialized as `"sub-agent"` in JSON.

### AgentToolStatus

```
running, done
```

### MessageRole

```
user, agent, system
```

### MessageStatus

```
pending, sent, delivered, read, incomplete, silent, failed
```

### MessageContentType

```
text, action_card, tool_status, system, loading, delegation, artifact, workflow
```

`loading` and `streaming` are client-only — the backend never sends these.

### ConversationType

```
swarm, agent
```

### AttachmentType

```
image, video, audio, file
```

### ActionStyle

```
primary, secondary, destructive
```

### ToolState

```
running, completed, failed
```

### ResponseLength

```
auto, short, medium, long
```

---

## 16. REST API Contract

All endpoints under `/v1/`. JSON field names use `snake_case`. Response envelope: `{ "data": ... }`. Empty mutations return `204`.

### Agents

| Method | Path                                             | Description                        |
| ------ | ------------------------------------------------ | ---------------------------------- |
| GET    | `/v1/workspaces/{workspaceId}/agents`            | List all agents in workspace       |
| GET    | `/v1/agents/{id}`                                | Get agent summary                  |
| GET    | `/v1/agents/{id}/details`                        | Get full agent profile             |
| GET    | `/v1/agents/{id}/settings`                       | Get agent LLM settings and prompts |
| GET    | `/v1/agents/{id}/tools?limit&offset`             | List agent tools (paginated)       |
| GET    | `/v1/agents/{id}/memories?limit&offset&category` | List agent memories (paginated)    |
| POST   | `/v1/agents/{id}/memories`                       | Create a memory                    |
| DELETE | `/v1/agents/{id}/memories/{key}`                 | Delete a memory                    |

### Conversations

| Method | Path                                                   | Description               |
| ------ | ------------------------------------------------------ | ------------------------- |
| GET    | `/v1/workspaces/{workspaceId}/conversations`           | List conversations        |
| GET    | `/v1/workspaces/{workspaceId}/conversations/{id}`      | Get single conversation   |
| POST   | `/v1/workspaces/{workspaceId}/conversations`           | Create a new conversation |
| DELETE | `/v1/workspaces/{workspaceId}/conversations/{id}`      | Delete a conversation     |
| POST   | `/v1/workspaces/{workspaceId}/conversations/{id}/read` | Mark conversation as read |

### Messages

| Method | Path                                                                                | Description                      |
| ------ | ----------------------------------------------------------------------------------- | -------------------------------- |
| GET    | `/v1/workspaces/{workspaceId}/conversations/{id}/messages?scrollId&limit&direction` | List messages (cursor-paginated) |
| POST   | `/v1/workspaces/{workspaceId}/conversations/{id}/messages`                          | Send message (REST fallback)     |
| POST   | `/v1/workspaces/{workspaceId}/messages/{id}/action`                                 | Respond to action card           |

### Pagination

**Cursor-based** (messages, notifications):

```json
{
  "pagination": {
    "next_scroll_id": "string | null",
    "prev_scroll_id": "string | null",
    "has_next": true,
    "has_prev": false
  }
}
```

**Offset-based** (tools, memories):

```json
{
  "pagination": {
    "total": 42,
    "limit": 20,
    "offset": 0,
    "has_next": true
  }
}
```

---

## 17. Socket.IO Event Contract

Namespace: `/v1`

### Client to Server

| Event                   | Payload                                                                                   | Description                       |
| ----------------------- | ----------------------------------------------------------------------------------------- | --------------------------------- |
| `workspace.subscribe`   | `{ "workspace_ids": ["uuid"] }`                                                           | Subscribe to workspace events     |
| `workspace.unsubscribe` | `{ "workspace_ids": ["uuid"] }`                                                           | Unsubscribe from workspace events |
| `message.send`          | `{ "workspace_id", "conversation_id", "local_id", "content", "mentions", "attachments" }` | Send a message                    |

### Server to Client

| Event                  | Payload                                                                                                                             | Description                 |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------- |
| `workspace.subscribed` | `{ "workspace_ids": ["uuid"] }`                                                                                                     | Subscription confirmed      |
| `message.send.ack`     | `{ "local_id", "message_id", "status" }`                                                                                            | Message accepted by server  |
| `message.send.error`   | `{ "local_id", "error" }`                                                                                                           | Message send failed         |
| `message.chunk`        | `{ "message_id", "conversation_id", "agent_id", "chunk" }`                                                                          | Streaming text chunk        |
| `message.done`         | `{ "message_id", "conversation_id", "agent_id", "status" }`                                                                         | Streaming complete          |
| `message.new`          | `{ "message": MessageData }`                                                                                                        | New message in conversation |
| `message.updated`      | `{ "message": MessageData }`                                                                                                        | Existing message updated    |
| `message.status`       | `{ "message_id", "conversation_id", "status", "local_id?" }`                                                                        | Delivery status change      |
| `agent.status`         | `{ "agent_id", "status", "conversation_id?" }`                                                                                      | Agent status change         |
| `agent.tool`           | `{ "agent_id", "conversation_id", "tool", "status", "query?" }`                                                                     | Tool invocation status      |
| `agent.delegation`     | `{ "from_agent_id", "to_agent_id", "conversation_id", "status", "message_preview", "message_id" }`                                  | Agent delegation event      |
| `artifact.updated`     | `{ "artifact_id", "title", "version", "action", "agent_id", "conversation_id", "agent_slug" }`                                      | Artifact created/updated    |
| `workflow.update`      | `{ "workflow_id", "execution_id", "workflow_name", "status", "conversation_id", "step_index", "step_name", "agent_slug", "error" }` | Workflow progress           |

---

## 18. Implementation Priorities

### P1 — Required for Core Flow

| #   | Requirement                      | Description                                                                                          |
| --- | -------------------------------- | ---------------------------------------------------------------------------------------------------- |
| 1   | Create conversation endpoint     | `POST /v1/workspaces/{workspaceId}/conversations` — without this users cannot start new chats        |
| 2   | Mark conversation read           | `POST /v1/workspaces/{workspaceId}/conversations/{id}/read` — without this unread badges never clear |
| 3   | MessageStatus enum               | Add `sent`, `read`, `incomplete`, `silent` to the status enum                                        |
| 4   | `message.status` socket event    | Emit delivery status transitions for Telegram-style ticks                                            |
| 5   | `message.done` extended statuses | Support `incomplete` and `silent` in addition to `delivered` and `failed`                            |
| 6   | AgentStatus enum                 | Replace `busy` with `reading`, `thinking`, `writing`                                                 |
| 7   | Granular agent.status events     | Emit reading/thinking/writing/online during the response lifecycle                                   |

### P2 — Required for Swarm Features

| #   | Requirement             | Description                                                                    |
| --- | ----------------------- | ------------------------------------------------------------------------------ |
| 8   | Delegation content type | Backend emits `delegation` MessageContent and `agent.delegation` socket events |
| 9   | Artifact content type   | Backend emits `artifact` MessageContent and `artifact.updated` socket events   |
| 10  | Workflow content type   | Backend emits `workflow` MessageContent and `workflow.update` socket events    |
| 11  | Agent tools endpoint    | `GET /v1/agents/{id}/tools` with offset pagination                             |
| 12  | Memories pagination     | Add offset pagination to `GET /v1/agents/{id}/memories`                        |
| 13  | Delete conversation     | `DELETE /v1/workspaces/{workspaceId}/conversations/{id}`                       |

### P3 — Not Swarm-Specific but Needed

| #   | Requirement     | Description                                              |
| --- | --------------- | -------------------------------------------------------- |
| 14  | File upload     | Upload endpoint for chat attachments and profile avatars |
| 15  | Logout endpoint | `POST /v1/auth/logout` for backend session invalidation  |
| 16  | Search messages | Full-text search across conversation messages            |
