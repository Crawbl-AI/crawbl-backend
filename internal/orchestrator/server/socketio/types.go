// Package socketio declares the constants, configuration types,
// and core structs used by the Socket.IO server implementation.
package socketio

import (
	"log/slog"

	"github.com/gocraft/dbr/v2"
	"github.com/redis/go-redis/v9"
	"github.com/zishang520/socket.io/v2/socket"

	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
)

// socketNamespace is the Socket.IO namespace path that the mobile client connects to.
const socketNamespace = "/v1"

// workspaceRoomPrefix is prepended to workspace IDs to form room names.
const workspaceRoomPrefix = "workspace:"

// Config holds the dependencies for creating a Socket.IO server.
type Config struct {
	// Logger provides structured logging for Socket.IO operations.
	Logger *slog.Logger

	// RedisClient is the Redis client for the pub/sub adapter.
	// Required for cross-pod fan-out in clustered deployments.
	RedisClient *redis.Client

	// DB is the database connection for creating per-request sessions.
	// Required for message.send handling. Nil disables chat over WebSocket.
	DB *dbr.Connection

	// ChatService handles message sending and agent interactions.
	// Required for message.send handling. Nil disables chat over WebSocket.
	ChatService orchestratorservice.ChatService

	// AuthService resolves users from authenticated principals.
	// Required for message.send handling. Nil disables chat over WebSocket.
	AuthService orchestratorservice.AuthService
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

// Broadcaster emits real-time events to connected clients via Socket.IO.
// It broadcasts to workspace-scoped rooms so only clients subscribed to a given
// workspace receive the events. When a Redis adapter is configured, events are
// automatically fanned out across all pods.
type Broadcaster struct {
	io     *socket.Server
	logger *slog.Logger
}

// Socket event names for chat messaging over WebSocket.
const (
	eventMessageSend    = "message.send"
	eventMessageSendAck = "message.send.ack"
	eventMessageSendErr = "message.send.error"
)

// messageSendPayload is the JSON payload for the message.send event from the client.
type messageSendPayload struct {
	WorkspaceID    string                  `json:"workspace_id"`
	ConversationID string                  `json:"conversation_id"`
	Content        messageSendContent      `json:"content"`
	Mentions       []messageSendMention    `json:"mentions"`
	LocalID        string                  `json:"local_id"`
	Attachments    []messageSendAttachment `json:"attachments"`
}

// messageSendContent is the content field within a message.send payload.
type messageSendContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// messageSendMention is an @-mention within a message.send payload.
type messageSendMention struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Offset    int    `json:"offset"`
	Length    int    `json:"length"`
}

// messageSendAttachment is a file attachment within a message.send payload.
type messageSendAttachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	MIMEType string `json:"mime_type,omitempty"`
}

// messageSendAckPayload is the JSON payload for the message.send.ack event to the client.
type messageSendAckPayload struct {
	LocalID        string `json:"local_id"`
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
}

// messageSendErrPayload is the JSON payload for the message.send.error event to the client.
type messageSendErrPayload struct {
	LocalID        string `json:"local_id"`
	ConversationID string `json:"conversation_id"`
	Error          string `json:"error"`
}
