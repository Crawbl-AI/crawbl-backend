// Package server — types_socketio.go declares the constants, configuration types,
// and core structs used by the Socket.IO server implementation.
package server

import (
	"log/slog"

	"github.com/redis/go-redis/v9"
	"github.com/zishang520/socket.io/v2/socket"
)

// socketNamespace is the Socket.IO namespace path that the mobile client connects to.
const socketNamespace = "/v1"

// workspaceRoomPrefix is prepended to workspace IDs to form room names.
const workspaceRoomPrefix = "workspace:"

// SocketIOConfig holds the dependencies for creating a Socket.IO server.
type SocketIOConfig struct {
	// Logger provides structured logging for Socket.IO operations.
	Logger *slog.Logger


	// RedisClient is the Redis client for the pub/sub adapter.
	// Required for cross-pod fan-out in clustered deployments.
	RedisClient *redis.Client
}

// Socket event names for workspace subscription management.
const (
	eventWorkspaceSubscribe   = "workspace.subscribe"
	eventWorkspaceUnsubscribe = "workspace.unsubscribe"
	eventWorkspaceSubscribed  = "workspace.subscribed"
)

// workspaceSubscribePayload is the JSON payload for subscribe/unsubscribe events.
type workspaceSubscribePayload struct {
	WorkspaceIDs []string `json:"workspace_ids"`
}

// SocketIOBroadcaster emits real-time events to connected clients via Socket.IO.
// It broadcasts to workspace-scoped rooms so only clients subscribed to a given
// workspace receive the events. When a Redis adapter is configured, events are
// automatically fanned out across all pods.
type SocketIOBroadcaster struct {
	io     *socket.Server
	logger *slog.Logger
}
