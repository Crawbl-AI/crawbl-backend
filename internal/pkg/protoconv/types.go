// Package protoconv provides bidirectional converters between Go string-based
// domain enums and their canonical proto int32 enum definitions in
// internal/generated/proto/domain/v1. The proto definitions are the single
// source of truth; these converters bridge the Go internal representation
// (strings stored in Postgres and sent over JSON) with the proto enum types.
package protoconv

import (
	domainv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/domain/v1"
	memoryv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/memory/v1"
)

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

var (
	conversationTypeToProto = map[string]domainv1.ConversationType{
		"swarm": domainv1.ConversationType_CONVERSATION_TYPE_SWARM,
		"agent": domainv1.ConversationType_CONVERSATION_TYPE_AGENT,
	}
	conversationTypeFromProto = inverseMapIS(conversationTypeToProto)
)

var (
	messageRoleToProto = map[string]domainv1.MessageRole{
		"user":   domainv1.MessageRole_MESSAGE_ROLE_USER,
		"agent":  domainv1.MessageRole_MESSAGE_ROLE_AGENT,
		"system": domainv1.MessageRole_MESSAGE_ROLE_SYSTEM,
	}
	messageRoleFromProto = inverseMapIS(messageRoleToProto)
)

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

var (
	actionStyleToProto = map[string]domainv1.ActionStyle{
		"primary":     domainv1.ActionStyle_ACTION_STYLE_PRIMARY,
		"secondary":   domainv1.ActionStyle_ACTION_STYLE_SECONDARY,
		"destructive": domainv1.ActionStyle_ACTION_STYLE_DESTRUCTIVE,
	}
	actionStyleFromProto = inverseMapIS(actionStyleToProto)
)

var (
	toolStateToProto = map[string]domainv1.ToolState{
		"running":   domainv1.ToolState_TOOL_STATE_RUNNING,
		"completed": domainv1.ToolState_TOOL_STATE_COMPLETED,
		"failed":    domainv1.ToolState_TOOL_STATE_FAILED,
	}
	toolStateFromProto = inverseMapIS(toolStateToProto)
)

var (
	attachmentTypeToProto = map[string]domainv1.AttachmentType{
		"image": domainv1.AttachmentType_ATTACHMENT_TYPE_IMAGE,
		"video": domainv1.AttachmentType_ATTACHMENT_TYPE_VIDEO,
		"audio": domainv1.AttachmentType_ATTACHMENT_TYPE_AUDIO,
		"file":  domainv1.AttachmentType_ATTACHMENT_TYPE_FILE,
	}
	attachmentTypeFromProto = inverseMapIS(attachmentTypeToProto)
)

var (
	runtimeStateToProto = map[string]domainv1.RuntimeState{
		"provisioning": domainv1.RuntimeState_RUNTIME_STATE_PROVISIONING,
		"ready":        domainv1.RuntimeState_RUNTIME_STATE_READY,
		"offline":      domainv1.RuntimeState_RUNTIME_STATE_OFFLINE,
		"failed":       domainv1.RuntimeState_RUNTIME_STATE_FAILED,
	}
	runtimeStateFromProto = inverseMapIS(runtimeStateToProto)
)

var (
	responseLengthToProto = map[string]domainv1.ResponseLength{
		"auto":   domainv1.ResponseLength_RESPONSE_LENGTH_AUTO,
		"short":  domainv1.ResponseLength_RESPONSE_LENGTH_SHORT,
		"medium": domainv1.ResponseLength_RESPONSE_LENGTH_MEDIUM,
		"long":   domainv1.ResponseLength_RESPONSE_LENGTH_LONG,
	}
	responseLengthFromProto = inverseMapIS(responseLengthToProto)
)

var (
	questionModeToProto = map[string]domainv1.QuestionMode{
		"single": domainv1.QuestionMode_QUESTION_MODE_SINGLE,
		"multi":  domainv1.QuestionMode_QUESTION_MODE_MULTI,
	}
	questionModeFromProto = inverseMapIS(questionModeToProto)
)

var (
	itemTypeToProto = map[string]domainv1.ItemType{
		"tool": domainv1.ItemType_ITEM_TYPE_TOOL,
		"app":  domainv1.ItemType_ITEM_TYPE_APP,
	}
	itemTypeFromProto = inverseMapIS(itemTypeToProto)
)

var (
	memoryTypeToProto = map[string]memoryv1.MemoryType{
		"decision":   memoryv1.MemoryType_MEMORY_TYPE_DECISION,
		"preference": memoryv1.MemoryType_MEMORY_TYPE_PREFERENCE,
		"milestone":  memoryv1.MemoryType_MEMORY_TYPE_MILESTONE,
		"problem":    memoryv1.MemoryType_MEMORY_TYPE_PROBLEM,
		"emotional":  memoryv1.MemoryType_MEMORY_TYPE_EMOTIONAL,
		"fact":       memoryv1.MemoryType_MEMORY_TYPE_FACT,
		"task":       memoryv1.MemoryType_MEMORY_TYPE_TASK,
	}
	memoryTypeFromProto = inverseMapIS(memoryTypeToProto)
)

var (
	drawerStateToProto = map[string]memoryv1.DrawerState{
		"raw":         memoryv1.DrawerState_DRAWER_STATE_RAW,
		"classifying": memoryv1.DrawerState_DRAWER_STATE_CLASSIFYING,
		"processed":   memoryv1.DrawerState_DRAWER_STATE_PROCESSED,
		"merged":      memoryv1.DrawerState_DRAWER_STATE_MERGED,
		"failed":      memoryv1.DrawerState_DRAWER_STATE_FAILED,
	}
	drawerStateFromProto = inverseMapIS(drawerStateToProto)
)
