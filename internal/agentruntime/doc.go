// Package agentruntime contains the crawbl-agent-runtime binary's
// implementation packages. The runtime is the in-tree Go replacement for
// the Rust agent runtime and runs one instance per user workspace as a
// per-workspace pod under the existing Metacontroller / UserSwarm flow.
//
// The entry point is crawbl-backend/cmd/crawbl-agent-runtime. This
// package tree holds only the library code that the entry point wires
// together. Every external dependency (ADK-Go, adk-utils-go's OpenAI
// client, google.golang.org/genai, google.golang.org/grpc,
// google.golang.org/protobuf) is imported from exactly one leaf
// subpackage to keep the swap surface small and the rest of the
// codebase free of framework coupling.
//
// Subpackages:
//
//   - config     — CLI + env configuration (workspace ID, MCP endpoint, model, Redis).
//   - server     — gRPC server wiring (Converse bidi stream, health).
//   - runner     — ADK runner construction and workspace blueprint bootstrap.
//   - agents     — concrete Manager / Wally / Eve constructors; instruction text comes from the blueprint.
//   - tools      — tool registry; tools/local/* and tools/mcp/* for local + MCP-bridged tools.
//   - model      — LLM adapters (OpenAI via adk-utils-go).
//   - session    — Redis-backed ADK session.Service implementation.
//   - telemetry  — OpenTelemetry exporter wiring to VictoriaMetrics / VictoriaLogs (follow-up).
//   - storage    — DigitalOcean Spaces client built on aws-sdk-go-v2/service/s3 (follow-up).
//
// Plan reference: .omc/plans/2026-04-05-crawbl-agent-runtime-plan.md §6.
package agentruntime
