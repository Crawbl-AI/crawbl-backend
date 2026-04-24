package protoconv

import (
	"testing"

	domainv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/domain/v1"
)

// TestAgentStatusRoundTrip verifies every proto AgentStatus value survives
// a round-trip through the converter maps. A new proto value without a
// matching Go string constant causes this test to fail, keeping the
// two definitions in sync.
func TestAgentStatusRoundTrip(t *testing.T) {
	for value, name := range domainv1.AgentStatus_name {
		if value == 0 { // skip UNSPECIFIED
			continue
		}
		protoVal := domainv1.AgentStatus(value)
		goStr := AgentStatusFromProto(protoVal)
		if goStr == "" {
			t.Errorf("AgentStatus proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		backToProto := AgentStatusToProto(goStr)
		if backToProto != protoVal {
			t.Errorf("AgentStatus round-trip failed: proto %d → %q → proto %d", protoVal, goStr, backToProto)
		}
	}
}

func TestConversationTypeRoundTrip(t *testing.T) {
	for value, name := range domainv1.ConversationType_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.ConversationType(value)
		goStr := ConversationTypeFromProto(protoVal)
		if goStr == "" {
			t.Errorf("ConversationType proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if ConversationTypeToProto(goStr) != protoVal {
			t.Errorf("ConversationType round-trip failed for %s", name)
		}
	}
}

func TestMessageRoleRoundTrip(t *testing.T) {
	for value, name := range domainv1.MessageRole_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.MessageRole(value)
		goStr := MessageRoleFromProto(protoVal)
		if goStr == "" {
			t.Errorf("MessageRole proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if MessageRoleToProto(goStr) != protoVal {
			t.Errorf("MessageRole round-trip failed for %s", name)
		}
	}
}

func TestMessageStatusRoundTrip(t *testing.T) {
	for value, name := range domainv1.MessageStatus_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.MessageStatus(value)
		goStr := MessageStatusFromProto(protoVal)
		if goStr == "" {
			t.Errorf("MessageStatus proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if MessageStatusToProto(goStr) != protoVal {
			t.Errorf("MessageStatus round-trip failed for %s", name)
		}
	}
}

func TestMessageContentTypeRoundTrip(t *testing.T) {
	for value, name := range domainv1.MessageContentType_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.MessageContentType(value)
		goStr := MessageContentTypeFromProto(protoVal)
		if goStr == "" {
			t.Errorf("MessageContentType proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if MessageContentTypeToProto(goStr) != protoVal {
			t.Errorf("MessageContentType round-trip failed for %s", name)
		}
	}
}

func TestActionStyleRoundTrip(t *testing.T) {
	for value, name := range domainv1.ActionStyle_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.ActionStyle(value)
		goStr := ActionStyleFromProto(protoVal)
		if goStr == "" {
			t.Errorf("ActionStyle proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if ActionStyleToProto(goStr) != protoVal {
			t.Errorf("ActionStyle round-trip failed for %s", name)
		}
	}
}

func TestToolStateRoundTrip(t *testing.T) {
	for value, name := range domainv1.ToolState_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.ToolState(value)
		goStr := ToolStateFromProto(protoVal)
		if goStr == "" {
			t.Errorf("ToolState proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if ToolStateToProto(goStr) != protoVal {
			t.Errorf("ToolState round-trip failed for %s", name)
		}
	}
}

func TestAttachmentTypeRoundTrip(t *testing.T) {
	for value, name := range domainv1.AttachmentType_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.AttachmentType(value)
		goStr := AttachmentTypeFromProto(protoVal)
		if goStr == "" {
			t.Errorf("AttachmentType proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if AttachmentTypeToProto(goStr) != protoVal {
			t.Errorf("AttachmentType round-trip failed for %s", name)
		}
	}
}

func TestRuntimeStateRoundTrip(t *testing.T) {
	for value, name := range domainv1.RuntimeState_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.RuntimeState(value)
		goStr := RuntimeStateFromProto(protoVal)
		if goStr == "" {
			t.Errorf("RuntimeState proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if RuntimeStateToProto(goStr) != protoVal {
			t.Errorf("RuntimeState round-trip failed for %s", name)
		}
	}
}

func TestResponseLengthRoundTrip(t *testing.T) {
	for value, name := range domainv1.ResponseLength_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.ResponseLength(value)
		goStr := ResponseLengthFromProto(protoVal)
		if goStr == "" {
			t.Errorf("ResponseLength proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if ResponseLengthToProto(goStr) != protoVal {
			t.Errorf("ResponseLength round-trip failed for %s", name)
		}
	}
}

func TestQuestionModeRoundTrip(t *testing.T) {
	for value, name := range domainv1.QuestionMode_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.QuestionMode(value)
		goStr := QuestionModeFromProto(protoVal)
		if goStr == "" {
			t.Errorf("QuestionMode proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if QuestionModeToProto(goStr) != protoVal {
			t.Errorf("QuestionMode round-trip failed for %s", name)
		}
	}
}

func TestItemTypeRoundTrip(t *testing.T) {
	for value, name := range domainv1.ItemType_name {
		if value == 0 {
			continue
		}
		protoVal := domainv1.ItemType(value)
		goStr := ItemTypeFromProto(protoVal)
		if goStr == "" {
			t.Errorf("ItemType proto value %s (%d) has no Go string mapping", name, value)
			continue
		}
		if ItemTypeToProto(goStr) != protoVal {
			t.Errorf("ItemType round-trip failed for %s", name)
		}
	}
}
