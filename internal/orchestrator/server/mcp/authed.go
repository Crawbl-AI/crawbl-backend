package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// authedTool wraps a workspace-scoped MCP tool business function, extracting
// the workspace ID from the invocation context and opening a fresh dbr
// session before delegating to fn. If the workspace ID is missing, it returns
// an unauthorized error without invoking fn.
//
// Use authedToolWithUser for tools that also need the caller's user identity.
func authedTool[I any, O any](deps *Deps, fn authedToolFn[I, O]) sdkmcp.ToolHandlerFor[I, O] {
	var zero O
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input I) (*sdkmcp.CallToolResult, O, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, zero, errNoWorkspaceIdentity
		}
		return fn(ctx, deps.newSession(), workspaceID, input)
	}
}

// authedToolWithUser wraps an MCP tool business function that needs both the
// caller's user identity and the active workspace ID. It enforces that both
// values are present on the invocation context before opening a session and
// delegating to fn.
func authedToolWithUser[I any, O any](deps *Deps, fn authedToolUserFn[I, O]) sdkmcp.ToolHandlerFor[I, O] {
	var zero O
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input I) (*sdkmcp.CallToolResult, O, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, zero, errNoUserIdentity
		}
		return fn(ctx, deps.newSession(), userID, workspaceID, input)
	}
}
