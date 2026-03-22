# Crawbl Backend

## Purpose

Build the Go middleware/orchestrator for Crawbl.

This service sits between the Flutter app and each user's ZeroClaw swarm. It owns routing, auth, orchestration, integrations, billing controls, and auditability.

## Core Responsibilities

- Authenticate users and issue/validate platform sessions
- Provision `UserSwarm` resources and ZeroClaw deployments in shared runtime namespaces
- Proxy chat/task requests to the correct user swarm
- Route LLM requests across Ollama and cloud providers
- Expose integration adapters for Gmail, Calendar, Asana, and future apps
- Store and manage user OAuth tokens server-side
- Enforce rate limits, plans, and usage attribution
- Record audit logs for tool usage and write actions
- Later: broker A2A communication between user swarms

## Rules

- Treat this service as the control plane, not a thin API wrapper
- Keep LLM provider credentials in the backend, not in ZeroClaw pods
- Default model access is platform-managed; add BYOK later for power users
- Connected app credentials are per-user and must be revocable
- Read actions may auto-execute after consent; write actions require approval by default
- Adapters expose narrow capabilities, not raw unrestricted API passthrough
- Cross-user A2A must go through backend mediation, never direct cross-namespace pod access

## Design Priorities

- Clear typed contracts between mobile, backend, and ZeroClaw
- Idempotent provisioning and retries
- Secure secret storage and token refresh
- Structured logs, audit trails, and per-user usage accounting
- Provider-agnostic LLM routing
- Small, composable services and packages over framework-heavy abstractions

## MVP Focus

1. Auth and user provisioning
2. ZeroClaw request proxy
3. Gmail and Google Calendar adapters
4. LLM routing
5. Read-first integrations, then ask-before-write flows
