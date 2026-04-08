package errors

// ErrorType represents the category of an error.
// It determines how the error is handled and whether its message
// is safe to expose to clients.
type ErrorType int

// Error type constants define the two categories of errors in the system.
const (
	// ServerError indicates an internal server error that should not be
	// exposed to clients. These errors are logged internally but return
	// a generic "internal server error" message to the client.
	ServerError ErrorType = iota

	// BusinessError indicates a client-facing error with a safe message
	// and error code that can be returned in API responses. These errors
	// represent validation failures, not-found conditions, and other
	// expected business rule violations.
	BusinessError
)

// Error code constants define unique identifiers for each business error.
// These codes are returned to clients and can be used for error handling
// on the client side. Codes are prefixed by domain (AUTH, USR, WSP, etc.).
const (
	// Authentication error codes (AUTHxxxx)
	ErrCodeUnauthorized            = "AUTH0001" // User is not authenticated
	ErrCodeInvalidToken            = "AUTH0002" // Token is invalid or expired
	ErrCodeAccountDeletionDisabled = "AUTH0003" // Account deletion not available in this environment

	// User error codes (USRxxxx)
	ErrCodeUserDeleted             = "USR0001" // User account has been deleted
	ErrCodeUserNotFound            = "USR0002" // User does not exist
	ErrCodeUserWrongFirebaseUID    = "USR0003" // Firebase UID mismatch during auth
	ErrCodeUserFirebaseUIDMismatch = "USR0004" // Firebase UID does not match expected
	ErrCodeUserAlreadyExists       = "USR0005" // User with this subject already exists
	ErrCodeLegalVersionMismatch    = "USR0012" // Legal document version does not match

	// Workspace error codes (WSPxxxx)
	ErrCodeWorkspaceNotFound = "WSP0001" // Workspace does not exist

	// Agent error codes (AGTxxxx)
	ErrCodeAgentNotFound = "AGT0001" // Agent does not exist

	// Conversation/Chat error codes (CHTxxxx)
	ErrCodeConversationNotFound = "CHT0001" // Conversation does not exist

	// Message error codes (MSGxxxx)
	ErrCodeMessageNotFound    = "MSG0001" // Message does not exist
	ErrCodeUnsupportedMessage = "MSG0002" // Message type is not supported

	// Quota error codes (QTAxxxx)
	ErrCodeQuotaExceeded = "QTA0001" // User has exceeded their monthly token quota

	// Runtime error codes (RTMxxxx)
	ErrCodeRuntimeNotReady = "RTM0001" // User swarm runtime is not ready

	// Integration error codes (INTxxxx)
	ErrCodeIntegrationNotConfigured        = "INT0001" // Integration provider is not yet configured
	ErrCodeIntegrationProviderNotSupported = "INT0002" // Provider is not supported
	ErrCodeIntegrationCallbackFailed       = "INT0003" // OAuth callback failed

	// Workflow error codes (WFLxxxx)
	ErrCodeWorkflowNotFound          = "WFL0001" // Workflow definition not found
	ErrCodeWorkflowExecutionNotFound = "WFL0002" // Workflow execution not found
	ErrCodeWorkflowStepNotFound      = "WFL0003" // Workflow step execution not found

	// Artifact error codes (ARTxxxx)
	ErrCodeArtifactNotFound        = "ART0001" // Artifact not found
	ErrCodeArtifactVersionNotFound = "ART0002" // Artifact version not found
)

// Predefined business errors for common error conditions.
// These can be used directly or compared against using IsCode.
var (
	// Authentication errors
	ErrUnauthorized            = NewBusinessError("Unauthorized", ErrCodeUnauthorized)
	ErrInvalidToken            = NewBusinessError("Invalid token", ErrCodeInvalidToken)
	ErrAccountDeletionDisabled = NewBusinessError("Account deletion is not available", ErrCodeAccountDeletionDisabled)

	// User errors
	ErrUserDeleted             = NewBusinessError("User is deleted", ErrCodeUserDeleted)
	ErrUserNotFound            = NewBusinessError("User not found", ErrCodeUserNotFound)
	ErrUserWrongFirebaseUID    = NewServerErrorText("user found by email but Firebase UID does not match")
	ErrUserFirebaseUIDMismatch = NewBusinessError("Firebase UID mismatch", ErrCodeUserFirebaseUIDMismatch)

	// Workspace errors
	ErrWorkspaceNotFound = NewBusinessError("Workspace not found", ErrCodeWorkspaceNotFound)

	// Agent errors
	ErrAgentNotFound = NewBusinessError("Agent not found", ErrCodeAgentNotFound)

	// Conversation errors
	ErrConversationNotFound = NewBusinessError("Conversation not found", ErrCodeConversationNotFound)

	// Message errors
	ErrMessageNotFound    = NewBusinessError("Message not found", ErrCodeMessageNotFound)
	ErrUnsupportedMessage = NewBusinessError("Only text messages are supported right now", ErrCodeUnsupportedMessage)

	// Quota errors
	ErrQuotaExceeded = NewBusinessError("Monthly token quota exceeded", ErrCodeQuotaExceeded)

	// Runtime errors
	ErrRuntimeNotReady = NewBusinessError("Assistant is still starting", ErrCodeRuntimeNotReady)

	// Integration errors
	ErrIntegrationNotConfigured        = NewBusinessError("Integration is not yet available", ErrCodeIntegrationNotConfigured)
	ErrIntegrationProviderNotSupported = NewBusinessError("Provider is not supported", ErrCodeIntegrationProviderNotSupported)
	ErrIntegrationCallbackFailed       = NewBusinessError("OAuth token exchange failed", ErrCodeIntegrationCallbackFailed)

	// User errors (continued)
	ErrUserAlreadyExists    = NewBusinessError("User already exists", ErrCodeUserAlreadyExists)
	ErrLegalVersionMismatch = NewBusinessError("Legal document version does not match current version", ErrCodeLegalVersionMismatch)

	// Workflow errors
	ErrWorkflowNotFound          = NewBusinessError("Workflow definition not found", ErrCodeWorkflowNotFound)
	ErrWorkflowExecutionNotFound = NewBusinessError("Workflow execution not found", ErrCodeWorkflowExecutionNotFound)
	ErrWorkflowStepNotFound      = NewBusinessError("Workflow step execution not found", ErrCodeWorkflowStepNotFound)

	// Artifact errors
	ErrArtifactNotFound        = NewBusinessError("Artifact not found", ErrCodeArtifactNotFound)
	ErrArtifactVersionNotFound = NewBusinessError("Artifact version not found", ErrCodeArtifactVersionNotFound)

	// Validation errors (server-side, not exposed to clients)
	ErrInvalidInput             = NewServerErrorText("invalid input provided")
	ErrNilPrincipal             = NewServerErrorText("principal is required")
	ErrEmptySubject             = NewServerErrorText("principal subject is required")
	ErrEmptyEmail               = NewServerErrorText("principal email is required")
	ErrNilUser                  = NewServerErrorText("user is required")
	ErrNilWorkspace             = NewServerErrorText("workspace is required")
	ErrNilAgent                 = NewServerErrorText("agent is required")
	ErrNilConversation          = NewServerErrorText("conversation is required")
	ErrNilMessage               = NewServerErrorText("message is required")
	ErrNicknameGenerationFailed = NewServerErrorText("failed to generate unique nickname after max attempts")
)

// Error represents a structured error with type discrimination.
// It implements the standard error interface while providing
// additional context for error handling and API responses.
type Error struct {
	// Message is the human-readable error message.
	// For business errors, this is safe to return to clients.
	// For server errors, this may contain sensitive information.
	Message string `json:"message"`

	// Code is a unique error identifier for business errors.
	// It allows clients to programmatically handle specific error cases.
	// Empty for server errors.
	Code string `json:"code,omitempty"`

	// Err is the underlying wrapped error for server errors.
	// This is not serialized to JSON and is used internally for
	// error chaining and stack traces.
	Err error `json:"-"`

	// Type indicates whether this is a business or server error.
	// This determines how the error message is exposed to clients.
	Type ErrorType `json:"-"`
}
