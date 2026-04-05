// Command crawbl-agent-runtime is the second binary in the crawbl-backend
// module. It runs as the per-workspace agent runtime pod — one instance per
// user's swarm — and replaces the Rust ZeroClaw runtime in Phase 2.
//
// Runtime responsibilities:
//   - Host a multi-agent swarm (Manager + Wally + Eve) built on ADK-Go
//     (google.golang.org/adk).
//   - Serve a gRPC bidi stream for conversational turns.
//   - Talk back to the orchestrator's MCP server at /mcp/v1 over HMAC-signed
//     HTTP for orchestrator-mediated tools (user profile, agent history, etc.).
//   - Persist all durable state in Postgres via the orchestrator; keep
//     ephemeral caches in /cache (emptyDir) and /tmp (tmpfs); artifacts go to
//     DigitalOcean Spaces via the S3 protocol abstraction.
//   - No per-user PVC anywhere in this process.
//
// This file is currently a Phase 1 iteration 4 skeleton stub that prints a
// startup banner and exits. The full wiring lands in US-AR-003 (config + gRPC
// server) and US-AR-009 (Converse implementation). Do not add logic here
// beyond minimal bootstrap until the subpackages are ready to import.
package main

import (
	"fmt"
	"os"
)

// version is set by the Makefile build target via -ldflags at link time.
// It remains "dev" for local builds that skip the linker flag.
var version = "dev"

func main() {
	fmt.Fprintf(os.Stdout, "crawbl-agent-runtime %s (phase 1 skeleton) — not yet wired\n", version)
	fmt.Fprintln(os.Stdout, "see .omc/plans/2026-04-05-crawbl-agent-runtime-plan.md")
}
