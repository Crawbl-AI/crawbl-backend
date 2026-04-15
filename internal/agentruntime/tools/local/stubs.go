package local

// This file holds Phase 1 stub implementations for the 17 runtime-local
// tools in the catalog that are NOT exercised by the US-AR-014 e2e gate.
// They exist so that US-AR-008's agent wiring can bind every catalog entry
// without a nil pointer, and so that agents calling an unimplemented tool
// during e2e get a deterministic typed error instead of a crash.
//
// Each stub returns ErrNotImplemented (defined in web_fetch.go). Later
// stories replace them one at a time; adding a real implementation is a
// drop-in replacement — no catalog changes, no agent rebinding.
//
// Tools that bridge to the orchestrator MCP server are handled in
// tools/mcp/, not here.

import "context"

// CallNotImplementedTool is the universal fallback used by runner/workflow.go
// when it binds a stubbed tool. Every stub wrapper below delegates to it so
// there is a single source of truth for the error message shape. Agents see
// a typed error rather than a silent failure.
func CallNotImplementedTool(ctx context.Context, name string, _ any) (string, error) {
	_ = ctx
	return "", wrapNotImplemented(name)
}

// wrapNotImplemented builds a friendly error message tagged with the tool
// name so an agent's failure trace points at exactly which tool is
// missing an implementation.
func wrapNotImplemented(name string) error {
	return &notImplementedError{name: name}
}

type notImplementedError struct {
	name string
}

func (e *notImplementedError) Error() string {
	return "tool " + e.name + ": " + ErrNotImplemented.Error()
}

func (e *notImplementedError) Unwrap() error {
	return ErrNotImplemented
}

// StubbedToolNames enumerates the catalog tools that currently fall through
// to CallNotImplementedTool. It is used by US-AR-008's agent wiring to bind
// every stubbed tool in one loop without hard-coding the list in workflow.go.
//
// The order matches internal/agentruntime/tools/catalog.go so the two files
// drift-check cleanly in code review.
func StubbedToolNames() []string {
	return []string{
		"http_request",
		"file_read",
		"file_write",
		"file_edit",
		"glob_search",
		"content_search",
		"cron_add",
		"cron_list",
		"cron_remove",
		"cron_update",
		"cron_run",
		"cron_runs",
		"calculator",
		"weather",
		"image_info",
		"shell",
		"delegate",
	}
}
