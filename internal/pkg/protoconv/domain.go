package protoconv

import domainv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/domain/v1"

// --- AgentStatus ---

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
