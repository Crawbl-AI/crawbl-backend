---
title: "Native Delegate Agents: Manager + Wally"
type: feat
date: 2026-03-31
---

# Native Delegate Agents: Manager + Wally

## Overview

Replace the current "fake multi-agent" approach (appending `system_prompt` strings per webhook request) with ZeroClaw's native `[agents.<name>]` config system. Introduce a **Manager + Sub-Agent** architecture where:

- The **Manager** is ZeroClaw's base agent (defined via SOUL.md), receives all swarm messages, and delegates to specialists via the built-in `delegate` tool.
- **Wally** is the first sub-agent — a versatile generalist defined as a native `[agents.wally]` config entry with his own personality files via `skills_directory`.

All changes are in crawbl-backend. Zero changes needed in the ZeroClaw fork.

## Problem Statement

Today, "multi-agent" in Crawbl is a prompt-level illusion:

1. `BuildConfigTOML()` in `internal/zeroclaw/toml.go` emits no `[agents.*]` sections
2. `BuildAgentsMarkdown()` in `internal/zeroclaw/markdown.go` hardcodes two role descriptions
3. `SendText()` in `internal/userswarm/client/userswarm.go` sends `system_prompt` per request ("You are a Research agent...")
4. ZeroClaw's native delegate engine (`src/tools/delegate.rs`, `src/tools/swarm.rs`) sits idle

This means all agents share one model, all have access to all 26 tools, no delegation chains, no swarm orchestration, and no per-agent personality depth.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  ZeroClaw Pod (1 per user)                          │
│                                                     │
│  Base Agent = "Manager"                             │
│  ├─ SOUL.md      — Manager identity + delegation    │
│  ├─ IDENTITY.md  — First-person self-reference      │
│  ├─ TOOLS.md     — All 26 tools incl. delegate      │
│  │                                                  │
│  └─ config.toml                                     │
│      └─ [agents.wally]                              │
│          ├─ system_prompt, model, agentic            │
│          ├─ allowed_tools = [filtered subset]        │
│          └─ skills_directory = "agents/wally"        │
│              ├─ personality.md                       │
│              ├─ guidelines.md                        │
│              └─ domain.md                            │
└─────────────────────────────────────────────────────┘
```

### Message Flow

**Swarm conversation** (user talks to the swarm):
1. Backend sends webhook with **no `agent_id`** → Manager (base agent) handles
2. Manager reads the message, decides: "this matches Wally's domain"
3. Manager calls `delegate(agent="wally", prompt="...")` internally
4. Wally runs his agentic loop (web_search, etc.), returns result to Manager
5. Manager formats and returns the response to the user

**Direct agent conversation** (user talks to Wally 1:1):
1. Backend sends webhook with `agent_id="wally"`
2. ZeroClaw routes directly to Wally's session/memory namespace
3. Wally responds directly (bypasses Manager)

**Adding a new agent later** (e.g., "Coder"):
1. Add `[agents.coder]` to operator config → pod restarts
2. Manager auto-discovers Coder via `delegate` tool — no Manager update needed
3. Add "Coder" to `DefaultAgents` in Go → bootstrap creates DB row + conversation

### What the Mobile App Sees

| Conversation | Type | Backend Behavior |
|-------------|------|-----------------|
| "Swarm" | swarm | No `agent_id` sent → Manager handles, delegates internally |
| "Wally" | agent | `agent_id="wally"` → Wally responds directly |

**Postgres `agents` table** (Manager is NOT a row — it's the base agent):

| sort_order | name | role |
|------------|------|------|
| 0 | Wally | wally |

## Implementation

### Files Changed

| File | Change |
|------|--------|
| `internal/zeroclaw/types.go` | Add `DelegateAgentConfig` struct, add `Agents map[string]DelegateAgentConfig` to `BootstrapConfig`, add `ZeroClawAgent` to operator YAML types, add `"agents"` to `managedKeys` |
| `internal/zeroclaw/toml.go` | Extend `BuildConfigTOML()` to populate `cfg.Agents` from `ZeroClawConfig`, with provider/model fallback from workspace defaults |
| `internal/zeroclaw/markdown.go` | Update `BuildSoulMarkdown()` with Manager identity + delegation awareness, add `BuildAgentSkillFiles()` for per-agent personality `.md` files, remove hardcoded `BuildAgentsMarkdown()` |
| `internal/zeroclaw/bootstrap.go` | Add full-section replacement for `"agents"` in `mergeManaged()`, add `EnsureAgentSkills()` to write per-agent skill files to PVC |
| `internal/orchestrator/types.go` | Change `DefaultAgents` from researcher+writer to just Wally |
| `chatservice/agents.go` | `resolveResponder` returns `nil` for swarm (no mention) instead of `agents[0]` |
| `chatservice/messages.go` | Handle `nil` responder — send webhook with no `agent_id` and no `system_prompt` |
| `config/samples/zeroclaw.yaml` | Add `agents` map with `wally` entry |
| `internal/userswarm/webhook/blueprint_runtime.go` | No change needed — agent skill files go on PVC via init container, not ConfigMap subPaths |

### 1. New Types (`internal/zeroclaw/types.go`)

**TOML-side config (emitted into config.toml):**

```go
// DelegateAgentConfig defines a sub-agent in ZeroClaw's native delegate system.
// Maps to ZeroClaw's Rust DelegateAgentConfig in src/config/schema.rs.
type DelegateAgentConfig struct {
    Model          string   `toml:"model,omitempty"`
    SystemPrompt   string   `toml:"system_prompt,omitempty"`
    Agentic        bool     `toml:"agentic"`
    AllowedTools   []string `toml:"allowed_tools,omitempty"`
    SkillsDir      string   `toml:"skills_directory,omitempty"`
}
```

**BootstrapConfig addition:**

```go
// Add to BootstrapConfig struct
Agents map[string]DelegateAgentConfig `toml:"agents,omitempty"`
```

**Operator-side config (loaded from zeroclaw.yaml):**

```go
// ZeroClawAgent defines an agent in the operator config.
type ZeroClawAgent struct {
    SystemPrompt string   `yaml:"systemPrompt"`
    Agentic      bool     `yaml:"agentic"`
    AllowedTools []string `yaml:"allowedTools,omitempty"`
}
```

**ZeroClawConfig addition:**

```go
// Add to ZeroClawConfig struct
Agents map[string]ZeroClawAgent `yaml:"agents,omitempty"`
```

**managedKeys addition — full-section replacement:**

```go
// New set for sections that are replaced entirely (not per-key merged).
var managedFullReplaceSections = map[string]bool{
    "agents": true,
}
```

**Produces TOML:**

```toml
[agents.wally]
model = "claude-sonnet-4-6"
system_prompt = "You are Wally, a versatile assistant agent in the Crawbl swarm."
agentic = true
allowed_tools = ["web_search", "web_fetch", "file_read", "file_write", "memory_recall", "memory_store"]
skills_directory = "agents/wally"
```

**Design notes:**
- `provider` and `model` are **required** by ZeroClaw's Rust deserializer. If operator YAML omits them, `BuildConfigTOML()` fills from the UserSwarm spec's `defaultProvider`/`defaultModel`. This fallback is explicit, not implicit.
- Only 5 fields emitted. ZeroClaw uses `#[serde(default)]` for all others (`max_depth`, `max_iterations`, `timeout_secs`, etc.) — omitted fields get Rust defaults. Add them when a real use case demands per-agent overrides.

### 2. Config Generation (`internal/zeroclaw/toml.go`)

Extend `BuildConfigTOML()` to populate `cfg.Agents`:

```go
// After building the base config, populate delegate agents.
if len(zc.Agents) > 0 {
    cfg.Agents = make(map[string]DelegateAgentConfig, len(zc.Agents))
    for name, agent := range zc.Agents {
        cfg.Agents[name] = DelegateAgentConfig{
            // Fill provider/model from workspace defaults if omitted.
            Model:        coalesce(agent.Model, cfg.DefaultModel),
            SystemPrompt: agent.SystemPrompt,
            Agentic:      agent.Agentic,
            AllowedTools: agent.AllowedTools,
            SkillsDir:    fmt.Sprintf("agents/%s", name),
        }
    }
}
```

Provider handling: ZeroClaw's `DelegateAgentConfig` requires `provider`. Since all agents currently share the workspace's default provider, emit it explicitly:

```go
type DelegateAgentConfig struct {
    Provider       string   `toml:"provider"`              // Always filled, never omitempty
    Model          string   `toml:"model,omitempty"`
    // ...
}
```

### 3. Manager Identity (`internal/zeroclaw/markdown.go`)

**Update `BuildSoulMarkdown()`** to give the base agent Manager identity with delegation awareness:

```go
func BuildSoulMarkdown(sw *crawblv1alpha1.UserSwarm) string {
    return fmt.Sprintf(`# SOUL.md - Who You Are

You are the Manager of user %q's private Crawbl swarm.

## Core Principles
- Start with the answer or action. Do not narrate internal processing.
- Speak naturally. Do not sound like a policy bot or a generic support script.
- Be concise by default, but still sound human and grounded.
- Use tools when needed, but keep tool use invisible in normal replies.
- Be proactive and practical. Offer the next helpful step when it saves time.
- Do not invent facts, hidden actions, or completed work.

## Delegation
You coordinate a team of specialist agents available via the `+"`"+`delegate`+"`"+` tool.

- When a task matches a specialist's domain, delegate to them.
- When delegating, give clear context about what the user needs.
- Handle general queries, coordination, and planning yourself.
- If no specialist fits, do the work yourself — you have all tools available.
- Report the specialist's result naturally, as if you did it yourself.
  Do not say "I delegated to Wally" — just give the answer.
`, sw.Spec.UserID)
}
```

**Remove `BuildAgentsMarkdown()`** — it's superseded by:
1. The Manager's SOUL.md delegation instructions (above)
2. Per-agent personality via `skills_directory` (below)
3. ZeroClaw's `delegate` tool auto-discovering `[agents.*]` from config

**Add `BuildAgentSkillFiles()`** — generates per-agent personality markdown:

```go
// BuildAgentSkillFiles generates personality .md files for each delegate agent.
// These are written to the PVC by the init container, not mounted from ConfigMap.
// ZeroClaw loads them via the skills_directory config for each agent.
func BuildAgentSkillFiles(zc *ZeroClawConfig) map[string]map[string]string {
    result := make(map[string]map[string]string, len(zc.Agents))
    for name := range zc.Agents {
        switch name {
        case "wally":
            result[name] = wallySkillFiles()
        default:
            result[name] = defaultSkillFiles(name)
        }
    }
    return result
}

func wallySkillFiles() map[string]string {
    return map[string]string{
        "personality.md": `# Wally — Personality

I'm Wally, a versatile assistant in the Crawbl swarm.

## Traits
- Resourceful and thorough — I dig deep before answering
- Friendly but direct — I respect the user's time
- Curious — I ask follow-up questions when they'd improve the result
- Honest — I say when I'm uncertain rather than guessing
`,
        "guidelines.md": `# Wally — Guidelines

## How I Work
- Start with the answer, then provide supporting context if needed
- Use web_search and web_fetch proactively for current information
- Cite sources when I find them — the user should be able to verify
- Store important facts in memory for later recall
- Chain tools when needed: search → fetch → analyze → respond

## What I Don't Do
- I don't invent facts or fabricate sources
- I don't narrate my tool usage — I just deliver results
- I don't pad responses with unnecessary caveats
`,
        "domain.md": `# Wally — Domain Expertise

## Strengths
- Research and information gathering
- Clear, structured writing and summarization
- Data analysis and comparison
- Email drafting and professional communication
- Planning and task breakdown
- General knowledge and reasoning
`,
    }
}

func defaultSkillFiles(name string) map[string]string {
    return map[string]string{
        "personality.md": fmt.Sprintf("# %s — Personality\n\nI am %s, a specialist agent in the Crawbl swarm.\n", name, name),
    }
}
```

**Update `BuildBootstrapFiles()`** to drop AGENTS.md:

```go
func BuildBootstrapFiles(sw *crawblv1alpha1.UserSwarm, zc *ZeroClawConfig, opts ...BuildBootstrapFilesOpts) (map[string]string, error) {
    // ...
    return map[string]string{
        "config.toml": configTOML,
        "SOUL.md":     BuildSoulMarkdown(sw),
        "IDENTITY.md": BuildIdentityMarkdown(sw),
        "TOOLS.md":    BuildToolsMarkdown(),
        // AGENTS.md removed — superseded by delegate tool + skills_directory
    }, nil
}
```

### 4. Init Container: Agent Skill Files (`internal/zeroclaw/bootstrap.go`)

Add `EnsureAgentSkills()` — called by the init container after `EnsureManagedConfig()`:

```go
// EnsureAgentSkills writes per-agent personality files to the PVC.
// Each agent gets a directory at {workspace}/agents/{name}/ containing
// markdown skill files that ZeroClaw loads via the skills_directory config.
//
// Files are overwritten on every boot (operator-managed, not user-managed).
func EnsureAgentSkills(workspaceDir string, agentFiles map[string]map[string]string) error {
    for agentName, files := range agentFiles {
        agentDir := filepath.Join(workspaceDir, "agents", agentName)
        if err := os.MkdirAll(agentDir, 0o755); err != nil {
            return fmt.Errorf("create agent dir %s: %w", agentName, err)
        }
        for filename, content := range files {
            path := filepath.Join(agentDir, filename)
            if err := fileutil.WriteAtomically(path, []byte(content), 0o644); err != nil {
                return fmt.Errorf("write %s/%s: %w", agentName, filename, err)
            }
        }
    }
    return nil
}
```

**Init container command** (`cmd/crawbl/platform/userswarm/bootstrap.go`) gets an additional call:

```go
// After EnsureManagedConfig:
agentFiles := zeroclaw.BuildAgentSkillFiles(zcConfig)
if err := zeroclaw.EnsureAgentSkills(workspaceDir, agentFiles); err != nil {
    return fmt.Errorf("ensure agent skills: %w", err)
}
```

**Backup coverage:** Confirmed safe. The backup job at `cmd/crawbl/platform/userswarm/backup.go` walks `/zeroclaw-data/workspace` recursively and includes all `.md` files. New files at `workspace/agents/wally/*.md` are automatically covered with zero changes.

### 5. Config Merge (`internal/zeroclaw/bootstrap.go`)

Extend `mergeManaged()` with full-section replacement for `"agents"`:

```go
func mergeManaged(live, bootstrap map[string]any) {
    // Full-section replacement for agents (and future: swarms).
    for section := range managedFullReplaceSections {
        if val, ok := bootstrap[section]; ok {
            live[section] = val
        } else {
            delete(live, section)
        }
    }

    // Existing per-key merge for all other managed sections.
    for section, keys := range managedKeys {
        // ... existing logic unchanged ...
    }
}
```

This ensures:
- Operator adds an agent → appears in live config on next restart
- Operator removes an agent → disappears from live config on next restart
- No zombie agents from stale live configs

### 6. Default Agent (`internal/orchestrator/types.go`)

Replace the two hardcoded agents with Wally:

```go
var DefaultAgents = []DefaultAgentBlueprint{
    {
        Name:         "Wally",
        Role:         "wally",
        SystemPrompt: "You are Wally, a versatile assistant agent in the Crawbl swarm. You handle research, writing, analysis, and general help. Be resourceful, thorough, and friendly.",
    },
}
```

Existing workspaces: On next bootstrap, `ensureDefaultAgents()` will create the Wally agent (role `"wally"` doesn't exist yet). The old researcher and writer agents remain in the DB but are harmless — they have no matching `[agents.*]` config entry, so the Manager won't delegate to them. They can be cleaned up in a future migration if desired.

### 7. Routing Change (`chatservice/agents.go`)

`resolveResponder` returns `nil` for swarm conversations with no @-mention:

```go
func resolveResponder(conversation *orchestrator.Conversation, agents []*orchestrator.Agent, mentions []orchestrator.Mention) *orchestrator.Agent {
    // 1. Per-agent conversation — use the conversation's agent
    if conversation.AgentID != nil {
        for _, agent := range agents {
            if agent.ID == *conversation.AgentID {
                return agent
            }
        }
    }

    // 2. Swarm conversation with mentions — use first mentioned agent
    if len(mentions) > 0 {
        for _, agent := range agents {
            if agent.ID == mentions[0].AgentID {
                return agent
            }
        }
    }

    // 3. Swarm with no mention — return nil.
    //    The base agent (Manager) handles it via the webhook with no agent_id.
    return nil
}
```

### 8. Webhook Call (`chatservice/messages.go`)

Handle `nil` responder — send webhook with no `agent_id`:

```go
// In SendMessage(), after resolving responder:
responder := resolveResponder(conversation, agents, mentions)

if responder != nil {
    // Direct agent conversation or @-mentioned agent
    sendOpts.AgentID = responder.Role
    // No system_prompt — ZeroClaw reads from config.toml [agents.<role>]
} else {
    // Swarm conversation, no mention — Manager (base agent) handles
    // No agent_id, no system_prompt — base agent receives the message
}
```

Stop setting `sendOpts.SystemPrompt` entirely. ZeroClaw reads it from `config.toml`.

### 9. Operator Config (`config/samples/zeroclaw.yaml`)

Add `agents` map:

```yaml
# ==============================================================================
# agents  (map[string]ZeroClawAgent)
#
# Defines delegate agents available in every ZeroClaw runtime.
# Maps to [agents.<name>] TOML sections in config.toml.
# The base agent (Manager) is defined by SOUL.md, not here.
#
# Provider and model are filled from defaults if omitted.
# skills_directory is auto-set to "agents/<name>" by BuildConfigTOML.
# ==============================================================================
agents:
  wally:
    systemPrompt: "You are Wally, a versatile assistant agent in the Crawbl swarm. You handle research, writing, analysis, and general help. Be resourceful, thorough, and friendly."
    agentic: true
    allowedTools:
      - web_search
      - web_fetch
      - file_read
      - file_write
      - memory_recall
      - memory_store
```

### 10. Blueprint Cleanup (`internal/userswarm/webhook/blueprint_runtime.go`)

Remove the AGENTS.md subPath mount from the ZeroClaw container:

```go
// Remove this line:
{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/AGENTS.md", SubPath: "AGENTS.md", ReadOnly: true},
```

AGENTS.md is no longer generated. Agent awareness comes from the `delegate` tool reading `[agents.*]` config.

## Config Lifecycle

### Pod Restart Flow (config update)

```
Operator updates zeroclaw.yaml (adds/changes agent)
    ↓
Webhook reconcile → BuildBootstrapFiles() → new ConfigMap (with [agents.wally])
    ↓
Checksum annotation changes → Kubernetes rolling restart
    ↓
Init container runs:
  1. EnsureManagedConfig() → merges [agents.*] into live config.toml
  2. EnsureAgentSkills() → writes agents/wally/*.md to PVC
    ↓
ZeroClaw starts → reads config.toml ONCE → sees [agents.wally]
    ↓
Manager's delegate tool sees Wally. Done.
```

### First Boot (new user)

```
Sign-up → CreateWorkspace → EnsureRuntime → UserSwarm CR created
    ↓
Metacontroller → webhook → ConfigMap + StatefulSet
    ↓
Init container: config.toml written as-is (first boot) + agent skills written
    ↓
ZeroClaw starts → Manager active with Wally available
    ↓
Bootstrap: Wally row created in Postgres + swarm conversation + Wally conversation
```

### Backup Coverage

Confirmed: The backup job at `cmd/crawbl/platform/userswarm/backup.go` walks `/zeroclaw-data/workspace` recursively and includes all `.md` files by extension. Files at `workspace/agents/wally/*.md` are automatically backed up with zero changes to backup logic.

## Acceptance Criteria

- [ ] `config.toml` emitted by backend contains valid `[agents.wally]` section with `provider` (required), `model`, `system_prompt`, `agentic`, `allowed_tools`, `skills_directory`
- [ ] ZeroClaw's `delegate` tool activates and Manager can route to Wally
- [ ] SOUL.md contains Manager identity with delegation instructions
- [ ] Per-agent personality files written to PVC at `workspace/agents/wally/`
- [ ] Wally has `personality.md`, `guidelines.md`, `domain.md` skill files
- [ ] `DefaultAgents` changed to just Wally
- [ ] Swarm conversation routes to Manager (no `agent_id` sent)
- [ ] Direct Wally conversation routes with `agent_id="wally"`
- [ ] `system_prompt` no longer sent per webhook request
- [ ] `mergeManaged()` handles full-section replacement for `"agents"`
- [ ] Existing workspaces gain `[agents.wally]` on next pod restart
- [ ] AGENTS.md removed from ConfigMap and blueprint mounts
- [ ] Agent skill files included in existing backup (no backup code changes)

## Quality Gates

- [ ] Go test: TOML encoding validates `map[string]DelegateAgentConfig` produces `[agents.wally]` sections (not `[[agents]]` array-of-tables)
- [ ] Go test: `mergeManaged` full-section replacement — live with old agents, bootstrap with new agents, verify clean replacement
- [ ] Go test: `BuildConfigTOML` fills `provider`/`model` from workspace defaults when operator YAML omits them
- [ ] Go test: `EnsureAgentSkills` writes correct files to PVC directory structure
- [ ] Integration: UserSwarm reconcile produces valid ConfigMap with `[agents.wally]`

## Risk Analysis

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| TOML encoding mismatch (Go emits wrong format for Rust) | Medium | High | Write encoding test FIRST, validate before anything else |
| `provider`/`model` empty → ZeroClaw crash on TOML parse | Medium | High | Explicit fallback in `BuildConfigTOML()` from workspace defaults; test the empty case |
| Old researcher/writer agents in DB confuse mobile app | Low | Low | They show in agent list but have no config backing; clean up in future migration |
| `resolveResponder` returning nil breaks existing code paths | Medium | Medium | Audit all callers of `resolveResponder`; ensure `SendMessage` handles nil gracefully |
| Agent skill files on PVC not cleaned up when agent removed | Low | Low | Stale `.md` files are harmless; `skills_directory` only loads what config points to |

## Future Additions

These are NOT in scope for this PR. Noted for context only:

- **More agents:** Add entries to `agents` map in operator YAML + `DefaultAgents` in Go. Manager auto-discovers them.
- **Swarm orchestration:** Add `[swarms.<name>]` TOML sections with sequential/parallel/router strategies.
- **Per-agent model selection:** Already supported — set `model` per agent in operator YAML.
- **User-customizable agents:** Would require DB columns for model/tools + a config reconciliation path.
- **Hot-reload:** ZeroClaw currently reads config once at startup. Hot-reload would need Rust-side changes.

## References

### Files to Change

| File | Lines | Purpose |
|------|-------|---------|
| `internal/zeroclaw/types.go` | 80, 171 | `BootstrapConfig` + `managedKeys` |
| `internal/zeroclaw/toml.go` | 38 | `BuildConfigTOML()` |
| `internal/zeroclaw/markdown.go` | 62, 192 | `BuildSoulMarkdown()`, remove `BuildAgentsMarkdown()`, add `BuildAgentSkillFiles()` |
| `internal/zeroclaw/bootstrap.go` | 46, 98 | `EnsureManagedConfig()`, `mergeManaged()`, add `EnsureAgentSkills()` |
| `internal/orchestrator/types.go` | 584 | `DefaultAgents` |
| `chatservice/agents.go` | 31 | `resolveResponder()` |
| `chatservice/messages.go` | 66 | Stop setting `SystemPrompt`, handle nil responder |
| `config/samples/zeroclaw.yaml` | end | Add `agents` section |
| `cmd/crawbl/platform/userswarm/bootstrap.go` | end | Call `EnsureAgentSkills()` |
| `webhook/blueprint_runtime.go` | 134 | Remove AGENTS.md subPath mount |

### ZeroClaw Native Systems (reference, no changes)

- `crawbl-zeroclaw/src/config/schema.rs:551` — `DelegateAgentConfig` Rust struct (provider + model required)
- `crawbl-zeroclaw/src/tools/delegate.rs:1000` — `skills_directory` loading for agentic sub-agents
- `crawbl-zeroclaw/src/tools/delegate.rs:1019` — SystemPromptBuilder sections for delegate agents
- `crawbl-zeroclaw/src/gateway/mod.rs` — Webhook handler; `agent_id` and `system_prompt` are `Option<String>`

### Institutional Learnings

- **Bootstrap command paths must match CLI structure** — grep all generated K8s manifests when restructuring commands
- **Never `MustParse()` on user-supplied values** — always provide defaults before parsing
- **System prompt is APPENDED, never replaced** — config-level `system_prompt` follows the same layering
- **Backup covers all `.md` files recursively** — verified at `cmd/crawbl/platform/userswarm/backup.go`
