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
	return err == ErrUserWrongFirebaseUID
}

// IsErrUserBanned checks if the error is a user banned error.
// This is a convenience function for the common case of checking
// for a specific user error code.
func IsErrUserBanned(err *Error) bool {
	return err != nil && err.Code == ErrCodeUserBanned
}
