// Package protoconv provides bidirectional converters between Go string-based
// domain enums and their canonical proto int32 enum definitions in
// internal/generated/proto/domain/v1. The proto definitions are the single
// source of truth; these converters bridge the Go internal representation
// (strings stored in Postgres and sent over JSON) with the proto enum types.
package protoconv

import domainv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/domain/v1"

// --- AgentStatus ---

var (
	agentStatusToProto = map[string]domainv1.AgentStatus{
		"online":   domainv1.AgentStatus_AGENT_STATUS_ONLINE,
		"reading":  domainv1.AgentStatus_AGENT_STATUS_READING,
		"thinking": domainv1.AgentStatus_AGENT_STATUS_THINKING,
		"pending":  domainv1.AgentStatus_AGENT_STATUS_PENDING,
		"error":    domainv1.AgentStatus_AGENT_STATUS_ERROR,
		"offline":  domainv1.AgentStatus_AGENT_STATUS_OFFLINE,
		"writing":  domainv1.AgentStatus_AGENT_STATUS_WRITING,
	}
	agentStatusFromProto = inverseMapIS(agentStatusToProto)
)

// AgentStatusToProto converts a Go string to the proto enum value.
func AgentStatusToProto(s string) domainv1.AgentStatus {
	if v, ok := agentStatusToProto[s]; ok {
		return v
	}
	return domainv1.AgentStatus_AGENT_STATUS_UNSPECIFIED
}

// AgentStatusFromProto converts a proto enum value to the Go string.
func AgentStatusFromProto(v domainv1.AgentStatus) string {
	return agentStatusFromProto[v]
}

// --- ConversationType ---

var (
	conversationTypeToProto = map[string]domainv1.ConversationType{
		"swarm": domainv1.ConversationType_CONVERSATION_TYPE_SWARM,
		"agent": domainv1.ConversationType_CONVERSATION_TYPE_AGENT,
	}
	conversationTypeFromProto = inverseMapIS(conversationTypeToProto)
)

// ConversationTypeToProto converts a Go string to the proto enum value.
func ConversationTypeToProto(s string) domainv1.ConversationType {
	if v, ok := conversationTypeToProto[s]; ok {
		return v
	}
	return domainv1.ConversationType_CONVERSATION_TYPE_UNSPECIFIED
}

// ConversationTypeFromProto converts a proto enum value to the Go string.
func ConversationTypeFromProto(v domainv1.ConversationType) string {
	return conversationTypeFromProto[v]
}

// --- MessageRole ---

var (
	messageRoleToProto = map[string]domainv1.MessageRole{
		"user":   domainv1.MessageRole_MESSAGE_ROLE_USER,
		"agent":  domainv1.MessageRole_MESSAGE_ROLE_AGENT,
		"system": domainv1.MessageRole_MESSAGE_ROLE_SYSTEM,
	}
	messageRoleFromProto = inverseMapIS(messageRoleToProto)
)

// MessageRoleToProto converts a Go string to the proto enum value.
func MessageRoleToProto(s string) domainv1.MessageRole {
	if v, ok := messageRoleToProto[s]; ok {
		return v
	}
	return domainv1.MessageRole_MESSAGE_ROLE_UNSPECIFIED
}

// MessageRoleFromProto converts a proto enum value to the Go string.
func MessageRoleFromProto(v domainv1.MessageRole) string {
	return messageRoleFromProto[v]
}

// --- MessageStatus ---

var (
	messageStatusToProto = map[string]domainv1.MessageStatus{
		"pending":    domainv1.MessageStatus_MESSAGE_STATUS_PENDING,
		"delivered":  domainv1.MessageStatus_MESSAGE_STATUS_DELIVERED,
		"failed":     domainv1.MessageStatus_MESSAGE_STATUS_FAILED,
		"sent":       domainv1.MessageStatus_MESSAGE_STATUS_SENT,
		"read":       domainv1.MessageStatus_MESSAGE_STATUS_READ,
		"incomplete": domainv1.MessageStatus_MESSAGE_STATUS_INCOMPLETE,
		"silent":     domainv1.MessageStatus_MESSAGE_STATUS_SILENT,
		"delegated":  domainv1.MessageStatus_MESSAGE_STATUS_DELEGATED,
	}
	messageStatusFromProto = inverseMapIS(messageStatusToProto)
)

// MessageStatusToProto converts a Go string to the proto enum value.
func MessageStatusToProto(s string) domainv1.MessageStatus {
	if v, ok := messageStatusToProto[s]; ok {
		return v
	}
	return domainv1.MessageStatus_MESSAGE_STATUS_UNSPECIFIED
}

// MessageStatusFromProto converts a proto enum value to the Go string.
func MessageStatusFromProto(v domainv1.MessageStatus) string {
	return messageStatusFromProto[v]
}

// --- MessageContentType ---

var (
	messageContentTypeToProto = map[string]domainv1.MessageContentType{
		"text":        domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT,
		"action_card": domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_ACTION_CARD,
		"tool_status": domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_TOOL_STATUS,
		"system":      domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_SYSTEM,
		"loading":     domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_LOADING,
		"delegation":  domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_DELEGATION,
		"artifact":    domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_ARTIFACT,
		"workflow":    domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_WORKFLOW,
		"questions":   domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_QUESTIONS,
	}
	messageContentTypeFromProto = inverseMapIS(messageContentTypeToProto)
)

// MessageContentTypeToProto converts a Go string to the proto enum value.
func MessageContentTypeToProto(s string) domainv1.MessageContentType {
	if v, ok := messageContentTypeToProto[s]; ok {
		return v
	}
	return domainv1.MessageContentType_MESSAGE_CONTENT_TYPE_UNSPECIFIED
}

// MessageContentTypeFromProto converts a proto enum value to the Go string.
func MessageContentTypeFromProto(v domainv1.MessageContentType) string {
	return messageContentTypeFromProto[v]
}

// --- ActionStyle ---

var (
	actionStyleToProto = map[string]domainv1.ActionStyle{
		"primary":     domainv1.ActionStyle_ACTION_STYLE_PRIMARY,
		"secondary":   domainv1.ActionStyle_ACTION_STYLE_SECONDARY,
		"destructive": domainv1.ActionStyle_ACTION_STYLE_DESTRUCTIVE,
	}
	actionStyleFromProto = inverseMapIS(actionStyleToProto)
)

// ActionStyleToProto converts a Go string to the proto enum value.
func ActionStyleToProto(s string) domainv1.ActionStyle {
	if v, ok := actionStyleToProto[s]; ok {
		return v
	}
	return domainv1.ActionStyle_ACTION_STYLE_UNSPECIFIED
}

// ActionStyleFromProto converts a proto enum value to the Go string.
func ActionStyleFromProto(v domainv1.ActionStyle) string {
	return actionStyleFromProto[v]
}

// --- ToolState ---

var (
	toolStateToProto = map[string]domainv1.ToolState{
		"running":   domainv1.ToolState_TOOL_STATE_RUNNING,
		"completed": domainv1.ToolState_TOOL_STATE_COMPLETED,
		"failed":    domainv1.ToolState_TOOL_STATE_FAILED,
	}
	toolStateFromProto = inverseMapIS(toolStateToProto)
)

// ToolStateToProto converts a Go string to the proto enum value.
func ToolStateToProto(s string) domainv1.ToolState {
	if v, ok := toolStateToProto[s]; ok {
		return v
	}
	return domainv1.ToolState_TOOL_STATE_UNSPECIFIED
}

// ToolStateFromProto converts a proto enum value to the Go string.
func ToolStateFromProto(v domainv1.ToolState) string {
	return toolStateFromProto[v]
}

// --- AttachmentType ---

var (
	attachmentTypeToProto = map[string]domainv1.AttachmentType{
		"image": domainv1.AttachmentType_ATTACHMENT_TYPE_IMAGE,
		"video": domainv1.AttachmentType_ATTACHMENT_TYPE_VIDEO,
		"audio": domainv1.AttachmentType_ATTACHMENT_TYPE_AUDIO,
		"file":  domainv1.AttachmentType_ATTACHMENT_TYPE_FILE,
	}
	attachmentTypeFromProto = inverseMapIS(attachmentTypeToProto)
)

// AttachmentTypeToProto converts a Go string to the proto enum value.
func AttachmentTypeToProto(s string) domainv1.AttachmentType {
	if v, ok := attachmentTypeToProto[s]; ok {
		return v
	}
	return domainv1.AttachmentType_ATTACHMENT_TYPE_UNSPECIFIED
}

// AttachmentTypeFromProto converts a proto enum value to the Go string.
func AttachmentTypeFromProto(v domainv1.AttachmentType) string {
	return attachmentTypeFromProto[v]
}

// --- RuntimeState ---

var (
	runtimeStateToProto = map[string]domainv1.RuntimeState{
		"provisioning": domainv1.RuntimeState_RUNTIME_STATE_PROVISIONING,
		"ready":        domainv1.RuntimeState_RUNTIME_STATE_READY,
		"offline":      domainv1.RuntimeState_RUNTIME_STATE_OFFLINE,
		"failed":       domainv1.RuntimeState_RUNTIME_STATE_FAILED,
	}
	runtimeStateFromProto = inverseMapIS(runtimeStateToProto)
)

// RuntimeStateToProto converts a Go string to the proto enum value.
func RuntimeStateToProto(s string) domainv1.RuntimeState {
	if v, ok := runtimeStateToProto[s]; ok {
		return v
	}
	return domainv1.RuntimeState_RUNTIME_STATE_UNSPECIFIED
}

// RuntimeStateFromProto converts a proto enum value to the Go string.
func RuntimeStateFromProto(v domainv1.RuntimeState) string {
	return runtimeStateFromProto[v]
}

// --- ResponseLength ---

var (
	responseLengthToProto = map[string]domainv1.ResponseLength{
		"auto":   domainv1.ResponseLength_RESPONSE_LENGTH_AUTO,
		"short":  domainv1.ResponseLength_RESPONSE_LENGTH_SHORT,
		"medium": domainv1.ResponseLength_RESPONSE_LENGTH_MEDIUM,
		"long":   domainv1.ResponseLength_RESPONSE_LENGTH_LONG,
	}
	responseLengthFromProto = inverseMapIS(responseLengthToProto)
)

// ResponseLengthToProto converts a Go string to the proto enum value.
func ResponseLengthToProto(s string) domainv1.ResponseLength {
	if v, ok := responseLengthToProto[s]; ok {
		return v
	}
	return domainv1.ResponseLength_RESPONSE_LENGTH_UNSPECIFIED
}

// ResponseLengthFromProto converts a proto enum value to the Go string.
func ResponseLengthFromProto(v domainv1.ResponseLength) string {
	return responseLengthFromProto[v]
}

// --- QuestionMode ---

var (
	questionModeToProto = map[string]domainv1.QuestionMode{
		"single": domainv1.QuestionMode_QUESTION_MODE_SINGLE,
		"multi":  domainv1.QuestionMode_QUESTION_MODE_MULTI,
	}
	questionModeFromProto = inverseMapIS(questionModeToProto)
)

// QuestionModeToProto converts a Go string to the proto enum value.
func QuestionModeToProto(s string) domainv1.QuestionMode {
	if v, ok := questionModeToProto[s]; ok {
		return v
	}
	return domainv1.QuestionMode_QUESTION_MODE_UNSPECIFIED
}

// QuestionModeFromProto converts a proto enum value to the Go string.
func QuestionModeFromProto(v domainv1.QuestionMode) string {
	return questionModeFromProto[v]
}

// --- ItemType ---

var (
	itemTypeToProto = map[string]domainv1.ItemType{
		"tool": domainv1.ItemType_ITEM_TYPE_TOOL,
		"app":  domainv1.ItemType_ITEM_TYPE_APP,
	}
	itemTypeFromProto = inverseMapIS(itemTypeToProto)
)

// ItemTypeToProto converts a Go string to the proto enum value.
func ItemTypeToProto(s string) domainv1.ItemType {
	if v, ok := itemTypeToProto[s]; ok {
		return v
	}
	return domainv1.ItemType_ITEM_TYPE_UNSPECIFIED
}

// ItemTypeFromProto converts a proto enum value to the Go string.
func ItemTypeFromProto(v domainv1.ItemType) string {
	return itemTypeFromProto[v]
}

// inverseMapIS builds a reverse map from proto enum value → Go string.
func inverseMapIS[E comparable](m map[string]E) map[E]string {
	out := make(map[E]string, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}
