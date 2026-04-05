package server

// This file previously held stub implementations of
// runtimev1.AgentRuntimeServer (Converse) and runtimev1.MemoryServer.
//
// - The Memory stub was replaced in US-AR-007 by the real memoryServer
//   in memory.go, backed by memory.Store.
// - The AgentRuntime Converse stub was replaced in US-AR-009 by the
//   real converseHandler in converse.go, backed by runner.Runner.
//
// Nothing lives here anymore; the file is kept empty (rather than
// deleted) so git history has a clean single-commit transition for
// reviewers tracing the stub → real swap.
