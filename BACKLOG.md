# Backend Backlog

---

## P0 - Known Issues (Production)

| ID  | Topic                        | Description                                                                                                                                                         | Impact                                      |
| --- | ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| 1   | Redis session token overflow | ADK Redis session accumulates conversation history indefinitely. After ~50 messages, hits OpenAI 272K token limit. Requires manual Redis key flush to unblock user. **Orchestrator side fixed** (v0.12.0 MemPalace: token-budgeted context injection, 14K char cap). **Runtime side still open**: ADK session in Redis still accumulates unbounded history. | Users get silent responses, cannot use chat |
| 2   | Context window management    | ~~`buildConversationContext` injects last 20 messages~~ **Fixed** (v0.12.0): rewritten as memory-first, token-budgeted (L0+L1 + budgeted messages). **Still open**: ADK session keeps full history internally in the runtime. No truncation or sliding window on the runtime side. | Compounds issue #1                          |

---

## P1 - Critical Features

| ID  | Topic                     | Description                                                                                                             | Reason                           |
| --- | ------------------------- | ----------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| 4   | Context window truncation | Runtime must cap ADK session to fit model's context limit. Options: sliding window, summarization, or hard truncation. **Note**: orchestrator context injection is now capped (v0.12.0 MemPalace), but the runtime's internal ADK session still needs truncation. | Blocks all long conversations    |
| 5   | New swarm conversation    | Users can only have one swarm conversation per workspace. Need `POST /conversations` with `type: swarm` to start fresh. | No way to reset bloated sessions |
| 6   | File upload endpoint      | `POST /v1/uploads` returns 501. Need S3/Spaces upload with presigned URLs.                                              | Users cannot send images/files   |
| 7   | Message search            | `GET .../messages/search` returns 501. Need full-text search across conversation messages.                              | Users cannot find old messages   |

---

## P2 - Important Improvements

| ID  | Topic                         | Description                                                                                                                                                                 | Reason                                  |
| --- | ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------- |
| 8   | Agent status `reading`        | Defined in enum but never emitted during streaming pipeline. Spec says it should appear before `thinking`.                                                                  | Incomplete status lifecycle             |
| 9   | Artifact socket events        | `artifact.updated` event defined and broadcaster method exists, but not verified end-to-end with mobile.                                                                    | Mobile handlers registered but untested |
| 10  | Workflow socket events        | `workflow.*` events defined but not verified end-to-end.                                                                                                                    | Mobile handlers registered but untested |
| 11  | Tool grouping support         | Backend persists each tool call as a separate `tool_status` message. Mobile groups consecutive tools from same agent. Consider backend-side grouping or a `group_id` field. | Cleaner data model                      |
| 12  | CQRS handler migration        | `ChatReader`/`ChatWriter` interfaces defined but handlers still use the fat `ChatService`. Gradually migrate handlers to declare narrower dependency.                       | Interface segregation incomplete        |
| 13  | Logout endpoint               | `POST /v1/auth/logout` handler exists but may not invalidate all sessions.                                                                                                  | Backend session cleanup                 |
| 14  | Connected accounts API        | `GET /v1/integrations/{provider}/accounts` and `DELETE .../accounts/{id}` — mobile ready, backend endpoints needed.                                                         | App services screen blocked             |
| 15  | Action card agent integration | `RespondToActionCard` handler exists but no agent actually emits action cards yet. Need ask-before-write flow (email, calendar).                                            | Approval UX not functional              |
| 16  | System message type           | `MessageContentTypeSystem` defined but never emitted. Should fire on agent join, workspace events.                                                                          | Missing system notifications            |
| 17  | Notification list endpoint    | `GET /v1/notifications` — mobile ready, backend endpoint needed.                                                                                                            | Empty notifications tab                 |
| 18  | Notification preferences      | `GET/PATCH /v1/notifications/preferences` — mobile ready.                                                                                                                   | Dead settings link                      |

---

## P3 - Tech Debt

| ID  | Topic                             | Description                                                                                                                                                      | Reason                 |
| --- | --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| 19  | `SendMessageOpts` mutation        | `persistUserMessage` mutates `opts.UserMessageID`, `StatusDeliveredOnce`, `StatusReadOnce`. Should be owned by `streamSession` only. Currently both write to it. | Shared mutable state   |
| 20  | Protobuf regen on every push      | Pre-push hook regenerates protobuf bindings, causing dirty tree if bindings differ.                                                                              | Slows down pushes      |
| 21  | Non-deterministic delegation card | `finalizeStreams` picks delegatee via random map iteration. With 2+ sub-agents, the delegation card's "to" agent is non-deterministic.                           | Edge case correctness  |
| 22  | Fire-and-forget goroutines        | `completeDelegation` and `updateDelegationSummary` run as `go func()` with no error propagation. DB failures are silent.                                         | Silent audit data loss |
| 23  | `helpers.go` is a grab-bag        | Contains types, pure functions, AND service methods with DB access. Service methods should live near their callers.                                              | Code organization      |
| 24  | Missing test coverage             | Only `TestNormalizeRuntimeMessage` exists. No tests for streaming pipeline, delegation, tool tracking, or finalization.                                          | Regression risk        |
| 25  | KG entity ID normalization lossy  | `entityID()` strips apostrophes and lowercases — `O'Brien` and `OBrien` collide. Could silently merge unrelated entities.                                       | Data integrity         |
| 26  | `splitIntoSegments` recompiles    | Three `regexp.Compile` calls inside function body, recompiled on every `Classify` call. Should be package-level vars.                                           | Performance            |
| 27  | BFS loads full graph per call     | `buildNodes` queries all drawers for a workspace with no caching. At 10K drawers, loads thousands of rows per traverse/tunnel/stats call.                       | Performance            |
| 28  | `tokenEstimate` unused            | Defined in `l0_identity.go` but never called. Was intended for budget enforcement but not integrated.                                                           | Dead code              |
| 29  | Diary agent name not sanitized    | `wing_{agentName}` constructed without normalizing agent name. Spaces or special chars create inconsistent wing names.                                          | Data consistency       |

---

## Done (for reference)

| ID  | Topic                           | Version          | Notes                                                                         |
| --- | ------------------------------- | ---------------- | ----------------------------------------------------------------------------- |
| D1  | Tool status persistence         | v0.9.0           | `tool_status` messages persisted in DB with `tool`, `state`, `query`, `args`  |
| D2  | Delegation content fields       | v0.8.0           | `from`/`to` nested agent objects in delegation messages and socket events     |
| D3  | Delegation DTO fix              | v0.8.0           | `MessageContentPayload` includes `from`/`to`/`status`/`task_preview`          |
| D4  | `created_at` on socket events   | v0.10.0          | `agent.tool` and `agent.delegation` include server timestamps                 |
| D5  | `conversationId` on ack/error   | v0.11.0          | `message.send.ack` and `message.send.error` include `conversation_id`         |
| D6  | Delegation before tools         | v0.11.3          | `resolveStream` called before `handleToolCall` for correct ordering           |
| D7  | Manager status after delegation | v0.11.4          | Emits `agent.status { Manager: online }` after delegation handoff             |
| D8  | Message doubling fix            | v0.1.1 (runtime) | ADK replays final text — runtime now skips ChunkEvents for final events       |
| D9  | Agent-runtime tag namespacing   | v0.1.0           | CLI uses `agent-runtime/v*` tags separate from platform `v*`                  |
| D10 | Remove Claude release notes     | v0.1.7           | `gh release create --generate-notes` replaces Claude CLI dependency           |
| D11 | Tool name constants             | v0.1.6           | 30+ tool names as typed constants in `catalog.go` with `ToolQueryField` map   |
| D12 | StreamSession refactor          | v0.11.5          | 992-line monolith split into 6 files with StreamSession struct                |
| D13 | Interface Segregation           | v0.11.5          | `ChatService` = `ChatReader` + `ChatWriter` composition                       |
| D14 | Startup seeding                 | v0.1.6           | All reference data (tools, models, categories) seeded on boot via dbr builder |

---

## Summary

| Priority          | Count  |
| ----------------- | ------ |
| P0 - Known Issues | 3      |
| P1 - Critical     | 4      |
| P2 - Important    | 11     |
| P3 - Tech Debt    | 11     |
| Done              | 14     |
| **Total**         | **43** |
