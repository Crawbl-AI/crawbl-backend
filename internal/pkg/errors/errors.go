package errors

import pkgerrors "github.com/pkg/errors"

type ErrorType int

const (
	ServerError ErrorType = iota
	BusinessError
)

const (
	ErrCodeUnauthorized         = "AUTH0001"
	ErrCodeInvalidToken         = "AUTH0002"
	ErrCodeUserDeleted          = "USR0001"
	ErrCodeUserNotFound         = "USR0002"
	ErrCodeWorkspaceNotFound    = "WSP0001"
	ErrCodeAgentNotFound        = "AGT0001"
	ErrCodeConversationNotFound = "CHT0001"
	ErrCodeMessageNotFound      = "MSG0001"
	ErrCodeRuntimeNotReady      = "RTM0001"
	ErrCodeUnsupportedMessage   = "MSG0002"
)

var (
	ErrUnauthorized         = NewBusinessError("Unauthorized", ErrCodeUnauthorized)
	ErrInvalidToken         = NewBusinessError("Invalid token", ErrCodeInvalidToken)
	ErrUserDeleted          = NewBusinessError("User is deleted", ErrCodeUserDeleted)
	ErrUserNotFound         = NewBusinessError("User not found", ErrCodeUserNotFound)
	ErrWorkspaceNotFound    = NewBusinessError("Workspace not found", ErrCodeWorkspaceNotFound)
	ErrAgentNotFound        = NewBusinessError("Agent not found", ErrCodeAgentNotFound)
	ErrConversationNotFound = NewBusinessError("Conversation not found", ErrCodeConversationNotFound)
	ErrMessageNotFound      = NewBusinessError("Message not found", ErrCodeMessageNotFound)
	ErrRuntimeNotReady      = NewBusinessError("Assistant is still starting", ErrCodeRuntimeNotReady)
	ErrUnsupportedMessage   = NewBusinessError("Only text messages are supported right now", ErrCodeUnsupportedMessage)

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

type Error struct {
	Message string    `json:"message"`
	Code    string    `json:"code,omitempty"`
	Err     error     `json:"-"`
	Type    ErrorType `json:"-"`
}

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

func NewServerError(err error) *Error {
	if err == nil {
		panic("server error cannot be nil")
	}

	return &Error{
		Err:  err,
		Type: ServerError,
	}
}

func NewServerErrorText(message string) *Error {
	if message == "" {
		panic("server error message cannot be empty")
	}

	return &Error{
		Err:  pkgerrors.New(message),
		Type: ServerError,
	}
}

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

func IsBusinessError(err *Error) bool {
	return err != nil && err.Type == BusinessError
}

func IsServerError(err *Error) bool {
	return err != nil && err.Type == ServerError
}

func IsCode(err *Error, code string) bool {
	return err != nil && err.Type == BusinessError && err.Code == code
}

func PublicMessage(err *Error) string {
	if err == nil {
		return ""
	}
	if err.Type == BusinessError {
		return err.Message
	}
	return "internal server error"
}
