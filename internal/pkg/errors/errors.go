// Package errors provides structured error types for the Crawbl backend.
// It distinguishes between business errors (client-facing, safe to expose)
// and server errors (internal, logged but not exposed to clients).
//
// Business errors have codes and messages suitable for API responses.
// Server errors wrap internal errors and are sanitized before being sent to clients.
//
// Example usage:
//
//	// Business error (safe to show to client)
//	err := errors.NewBusinessError("User not found", errors.ErrCodeUserNotFound)
//
//	// Server error (internal, not exposed)
//	err := errors.NewServerError(fmt.Errorf("database connection failed"))
package errors

import pkgerrors "github.com/pkg/errors"

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
	ErrCodeUnauthorized = "AUTH0001" // User is not authenticated
	ErrCodeInvalidToken = "AUTH0002" // Token is invalid or expired

	// User error codes (USRxxxx)
	ErrCodeUserDeleted             = "USR0001" // User account has been deleted
	ErrCodeUserNotFound            = "USR0002" // User does not exist
	ErrCodeUserWrongFirebaseUID    = "USR0003" // Firebase UID mismatch during auth
	ErrCodeUserFirebaseUIDMismatch = "USR0004" // Firebase UID does not match expected

	// Workspace error codes (WSPxxxx)
	ErrCodeWorkspaceNotFound = "WSP0001" // Workspace does not exist

	// Agent error codes (AGTxxxx)
	ErrCodeAgentNotFound = "AGT0001" // Agent does not exist

	// Conversation/Chat error codes (CHTxxxx)
	ErrCodeConversationNotFound = "CHT0001" // Conversation does not exist

	// Message error codes (MSGxxxx)
	ErrCodeMessageNotFound    = "MSG0001" // Message does not exist
	ErrCodeUnsupportedMessage = "MSG0002" // Message type is not supported

	// Runtime error codes (RTMxxxx)
	ErrCodeRuntimeNotReady = "RTM0001" // User swarm runtime is not ready
)

// Predefined business errors for common error conditions.
// These can be used directly or compared against using IsCode.
var (
	// Authentication errors
	ErrUnauthorized = NewBusinessError("Unauthorized", ErrCodeUnauthorized)
	ErrInvalidToken = NewBusinessError("Invalid token", ErrCodeInvalidToken)

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

	// Runtime errors
	ErrRuntimeNotReady = NewBusinessError("Assistant is still starting", ErrCodeRuntimeNotReady)

	// Validation errors (server-side, not exposed to clients)
	ErrInvalidInput    = NewServerErrorText("invalid input provided")
	ErrNilPrincipal    = NewServerErrorText("principal is required")
	ErrEmptySubject    = NewServerErrorText("principal subject is required")
	ErrEmptyEmail      = NewServerErrorText("principal email is required")
	ErrNilUser         = NewServerErrorText("user is required")
	ErrNilWorkspace    = NewServerErrorText("workspace is required")
	ErrNilAgent        = NewServerErrorText("agent is required")
	ErrNilConversation = NewServerErrorText("conversation is required")
	ErrNilMessage      = NewServerErrorText("message is required")
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

// Error implements the error interface.
// For business errors, it returns the Message field directly.
// For server errors, it returns the underlying error's message,
// or the Message field if no underlying error exists.
// Returns an empty string for nil receivers.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Type == BusinessError {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// NewBusinessError creates a new business error with the given message and code.
// Business errors are safe to expose to clients and should be used for
// expected error conditions like validation failures, not-found errors,
// and authorization failures.
//
// Both message and code must be non-empty; the function panics otherwise.
//
// Example:
//
//	err := NewBusinessError("User not found", ErrCodeUserNotFound)
func NewBusinessError(message, code string) *Error {
	if message == "" || code == "" {
		panic("business error message and code are required")
	}

	return &Error{
		Message: message,
		Code:    code,
		Type:    BusinessError,
	}
}

// NewServerError creates a new server error wrapping the given error.
// Server errors are internal errors that should not be exposed to clients.
// The wrapped error may contain sensitive information like stack traces
// or database details.
//
// The err parameter must not be nil; the function panics otherwise.
//
// Example:
//
//	err := NewServerError(fmt.Errorf("database connection failed"))
func NewServerError(err error) *Error {
	if err == nil {
		panic("server error cannot be nil")
	}

	return &Error{
		Err:  err,
		Type: ServerError,
	}
}

// NewServerErrorText creates a new server error with the given message.
// This is a convenience function for creating server errors from a text
// message without an existing error to wrap.
//
// The message parameter must not be empty; the function panics otherwise.
//
// Example:
//
//	err := NewServerErrorText("failed to process request")
func NewServerErrorText(message string) *Error {
	if message == "" {
		panic("server error message cannot be empty")
	}

	return &Error{
		Err:  pkgerrors.New(message),
		Type: ServerError,
	}
}

// WrapServerError wraps an existing Error with additional context message.
// For business errors, the original error is returned unchanged since
// business errors should not have their messages modified.
// For server errors, the underlying error is wrapped with the message.
//
// Both err and message must be non-nil/non-empty; the function panics otherwise.
//
// Example:
//
//	err := NewServerError(fmt.Errorf("db error"))
//	wrapped := WrapServerError(err, "failed to save user")
func WrapServerError(err *Error, message string) *Error {
	if err == nil {
		panic("cannot wrap nil app error")
	}
	if message == "" {
		panic("wrap message cannot be empty")
	}

	if err.Type == BusinessError {
		return err
	}

	return &Error{
		Err:  pkgerrors.Wrap(err.Err, message),
		Type: ServerError,
	}
}

// WrapStdServerError wraps a standard error with additional context message,
// returning a server Error. Use this when wrapping errors from external
// packages or standard library functions.
//
// Both err and message must be non-nil/non-empty; the function panics otherwise.
//
// Example:
//
//	err := WrapStdServerError(dbErr, "failed to query users")
func WrapStdServerError(err error, message string) *Error {
	if err == nil {
		panic("cannot wrap nil error")
	}
	if message == "" {
		panic("wrap message cannot be empty")
	}

	return &Error{
		Err:  pkgerrors.Wrap(err, message),
		Type: ServerError,
	}
}

// IsBusinessError returns true if the error is a business error.
// Business errors have safe client-facing messages and error codes.
// Returns false for nil errors.
func IsBusinessError(err *Error) bool {
	return err != nil && err.Type == BusinessError
}

// IsServerError returns true if the error is a server error.
// Server errors are internal and should not be exposed to clients.
// Returns false for nil errors.
func IsServerError(err *Error) bool {
	return err != nil && err.Type == ServerError
}

// IsCode checks if the error is a business error with the specified code.
// This is useful for programmatic error handling on specific error cases.
// Returns false for nil errors or non-business errors.
//
// Example:
//
//	if IsCode(err, ErrCodeUserNotFound) {
//	    // Handle user not found case
//	}
func IsCode(err *Error, code string) bool {
	return err != nil && err.Type == BusinessError && err.Code == code
}

// PublicMessage returns a client-safe error message.
// For business errors, returns the error message directly.
// For server errors, returns a generic "internal server error" message
// to avoid exposing internal details.
// Returns an empty string for nil errors.
func PublicMessage(err *Error) string {
	if err == nil {
		return ""
	}
	if err.Type == BusinessError {
		return err.Message
	}
	return "internal server error"
}

// IsErrUserNotFound checks if the error is a user not found error.
// This is a convenience function for the common case of checking
// for a specific user error code.
func IsErrUserNotFound(err *Error) bool {
	return err != nil && err.Code == ErrCodeUserNotFound
}

// IsErrUserDeleted checks if the error is a user deleted error.
// This is a convenience function for the common case of checking
// for a specific user error code.
func IsErrUserDeleted(err *Error) bool {
	return err != nil && err.Code == ErrCodeUserDeleted
}

// IsErrUserFirebaseUIDMismatch checks if the error is a Firebase UID mismatch error.
// This is a convenience function for the common case of checking
// for a specific user error code.
func IsErrUserFirebaseUIDMismatch(err *Error) bool {
	return err != nil && err.Code == ErrCodeUserFirebaseUIDMismatch
}

// IsErrUserWrongFirebaseUID checks if the error indicates user found by email
// but Firebase UID mismatch. This is a server error that occurs during
// authentication when the email matches but the Firebase UID differs.
func IsErrUserWrongFirebaseUID(err *Error) bool {
	return err != nil && err.Type == ServerError && err.Err != nil && err.Err.Error() == "user found by email but Firebase UID does not match"
}
