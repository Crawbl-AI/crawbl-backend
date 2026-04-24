// Package socketio declares the constants, configuration types,
// and core structs used by the Socket.IO server implementation.
package socketio

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/gocraft/dbr/v2"
	"github.com/redis/go-redis/v9"
	"github.com/zishang520/socket.io/v2/socket"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// socketSession holds per-socket cancellation state for in-flight dispatch goroutines.
// A single cancel func is kept per socket; each new dispatch replaces the previous one
// after cancelling it. The disconnect handler (registered once at connect time) calls
// cancelCurrent to stop any in-flight dispatch when the client disconnects.
type socketSession struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// setCancelFunc replaces the active cancel func, calling the previous one first so
// that any goroutine still running for an earlier message is cancelled immediately.
func (ss *socketSession) setCancelFunc(cancel context.CancelFunc) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.cancel != nil {
		ss.cancel()
	}
	ss.cancel = cancel
}

// cancelCurrent cancels the currently active dispatch (if any).
func (ss *socketSession) cancelCurrent() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.cancel != nil {
		ss.cancel()
		ss.cancel = nil
	}
}

// socketData is stored in socket.Data() after the connection handler runs.
// It bundles the authenticated principal with the per-socket cancellation session
// so both are accessible from any event handler on the socket.
type socketData struct {
	Principal *orchestrator.Principal
	Session   *socketSession
}

// socketNamespace is the Socket.IO namespace path that the mobile client connects to.
const socketNamespace = "/v1"

// workspaceRoomPrefix is prepended to workspace IDs to form room names.
const workspaceRoomPrefix = "workspace:"

// workspaceOwnerChecker is the minimal repo surface needed to verify that a
// given workspace belongs to the authenticated user before joining its room.
// Defined at the consumer (socketio package) per interface-segregation.
type workspaceOwnerChecker interface {
	// GetByID returns the workspace only when it exists and userID matches.
	// Any error (not-found or server error) must be treated as "not authorised".
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)

	// ListOwnedByUser returns the subset of workspaceIDs owned by userID as a
	// set for O(1) membership tests. Issues a single SELECT ... WHERE id IN (...)
	// query regardless of how many IDs are requested.
	ListOwnedByUser(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string, workspaceIDs []string) (map[string]struct{}, *merrors.Error)
}

// chatSender is the subset of chat service methods socketio calls into.
// Consumer-side narrowing keeps SIP compliance and shields us from growing
// a wider producer interface.
type chatSender interface {
	SendMessage(ctx context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error)

	// RespondToQuestions records user answers to a questions message and
	// broadcasts message.updated plus a synthesized follow-up message.new.
	RespondToQuestions(ctx context.Context, opts *orchestratorservice.RespondToQuestionsOpts) (*orchestrator.Message, *merrors.Error)
}

// authResolver is the subset of auth service methods socketio calls
// into — only the subject → user lookup on message.send.
type authResolver interface {
	GetBySubject(ctx context.Context, opts *orchestratorservice.GetUserBySubjectOpts) (*orchestrator.User, *merrors.Error)
}

// workspaceAuthorizer is the subset of workspace service methods
// socketio calls into — only the owner check before message dispatch.
type workspaceAuthorizer interface {
	GetByID(ctx context.Context, opts *orchestratorservice.GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

// Config holds the dependencies for creating a Socket.IO server.
type Config struct {
	// Logger provides structured logging for Socket.IO operations.
	Logger *slog.Logger

	// RedisClient is the Redis client for the pub/sub adapter.
	// Required for cross-pod fan-out in clustered deployments.
	RedisClient redis.UniversalClient

	// DB is the database connection for creating per-request sessions.
	// Required for workspace ownership checks and message.send handling.
	// When nil, workspace.subscribe skips the ownership check (dev/test only).
	DB *dbr.Connection

	// WorkspaceRepo verifies workspace ownership on workspace.subscribe.
	// When nil, the ownership check is skipped (dev/test only).
	WorkspaceRepo workspaceOwnerChecker

	// ChatService handles message sending and agent interactions.
	// Required for message.send handling. Nil disables chat over WebSocket.
	ChatService chatSender

	// AuthService resolves users from authenticated principals.
	// Required for message.send handling. Nil disables chat over WebSocket.
	AuthService authResolver

	// WorkspaceService verifies workspace ownership before message dispatch.
	// Required for message.send handling. Nil disables chat over WebSocket.
	WorkspaceService workspaceAuthorizer
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

// Compile-time interface satisfaction check.
var _ realtime.Broadcaster = (*Broadcaster)(nil)

// Socket event names for chat messaging over WebSocket.
const (
	eventMessageSend    = "message.send"
	eventMessageSendAck = "message.send.ack"
	eventMessageSendErr = "message.send.error"
)

// Socket event names for answer submission over WebSocket.
const (
	eventMessageAnswers    = "message.answers"
	eventMessageAnswersAck = "message.answers.ack"
	eventMessageAnswersErr = "message.answers.error"
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

// messageAnswersAckPayload is the JSON payload for the message.answers.ack event to the client.
type messageAnswersAckPayload struct {
	LocalID   string `json:"local_id"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

// messageAnswersErrPayload is the JSON payload for the message.answers.error event to the client.
type messageAnswersErrPayload struct {
	LocalID   string `json:"local_id"`
	MessageID string `json:"message_id"`
	Error     string `json:"error"`
}

// errInternalAnswersProcessing is the wire-safe fallback shown to clients when
// a non-business error reaches the socket boundary. The detailed cause is
// logged server-side and never leaked to the client.
const errInternalAnswersProcessing = "failed to process answers"

// errInvalidAnswersPayload is returned by parseMessageAnswersPayload when the
// raw Socket.IO argument cannot be cast to map[string]any.
var errInvalidAnswersPayload = errors.New("invalid answers payload")

// answersHandler handles the message.answers Socket.IO event.
// Service fields use the consumer-side interfaces declared in types.go
// so this package never imports the producer AuthService/ChatService contracts.
type answersHandler struct {
	db          *dbr.Connection
	chatService chatSender
	authService authResolver
	logger      *slog.Logger
}

// messageHandler holds the dependencies for handling message.send events.
// Service fields use the consumer-side interfaces declared in types.go
// so this package never imports the producer AuthService/ChatService/
// WorkspaceService contracts.
type messageHandler struct {
	db               *dbr.Connection
	chatService      chatSender
	authService      authResolver
	workspaceService workspaceAuthorizer
	logger           *slog.Logger
}

// workspaceSubscribeHandlerOpts groups the inputs for registerWorkspaceSubscribeHandler.
type workspaceSubscribeHandlerOpts struct {
	socket        *socket.Socket
	sd            *socketData
	logger        *slog.Logger
	subject       string
	db            *dbr.Connection
	workspaceRepo workspaceOwnerChecker
	authService   authResolver
}

// resolveAuthorisedWorkspacesOpts groups the inputs for resolveAuthorisedWorkspaces.
type resolveAuthorisedWorkspacesOpts struct {
	logger        *slog.Logger
	socket        *socket.Socket
	userSubject   string
	ids           []string
	db            *dbr.Connection
	workspaceRepo workspaceOwnerChecker
	authService   authResolver
}

// connectionHandlerCfg groups the parameters for registerConnectionHandler so
// the signature stays under the project's 4-5 param limit.
type connectionHandlerCfg struct {
	nsp           socket.Namespace
	logger        *slog.Logger
	db            *dbr.Connection
	workspaceRepo workspaceOwnerChecker
	authService   authResolver
	shutdownCtx   context.Context
}
