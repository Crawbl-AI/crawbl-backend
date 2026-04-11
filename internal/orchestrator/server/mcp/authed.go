package mcp

import (
	"context"
	"fmt"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool handler identity errors — returned when the MCP bearer token did not
// carry the expected user/workspace context. They are lowercase, noun-phrase
// errors per the error-string convention.
var (
	errNoWorkspaceIdentity = fmt.Errorf("unauthorized: no workspace identity")
	errNoUserIdentity      = fmt.Errorf("unauthorized: no user identity")
)

// authedToolFn is the business logic signature for MCP tools that only need
// a workspace identity. The adapter resolves the workspace ID and opens a
// fresh dbr session before calling fn.
type authedToolFn[I any, O any] func(
	ctx context.Context,
	sess *dbr.Session,
	workspaceID string,
	input I,
) (*sdkmcp.CallToolResult, O, error)

// authedToolUserFn is the business logic signature for MCP tools that need
// both user and workspace identity (e.g. artifact, chat, send_message_to_agent).
type authedToolUserFn[I any, O any] func(
	ctx context.Context,
	sess *dbr.Session,
	userID string,
	workspaceID string,
	input I,
) (*sdkmcp.CallToolResult, O, error)

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
