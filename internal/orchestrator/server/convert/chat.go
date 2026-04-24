package convert

import (
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/ptr"
)

// ConversationToProto converts a domain Conversation to the proto response.
func ConversationToProto(conversation *orchestrator.Conversation) *mobilev1.ConversationResponse {
	resp := &mobilev1.ConversationResponse{
		Id:          conversation.ID,
		Type:        string(conversation.Type),
		Title:       conversation.Title,
		CreatedAt:   timestamppb.New(conversation.CreatedAt),
		UpdatedAt:   timestamppb.New(conversation.UpdatedAt),
		UnreadCount: int32(conversation.UnreadCount), // #nosec G115 -- unread count fits in int32
	}
	if conversation.Agent != nil {
		resp.Agent = AgentToProto(conversation.Agent)
	}
	if conversation.LastMessage != nil {
		resp.LastMessage = MessageToProto(conversation.LastMessage)
	}
	return resp
}

// MessageToProto converts a domain Message to the proto response.
func MessageToProto(message *orchestrator.Message) *mobilev1.MessageResponse {
	resp := &mobilev1.MessageResponse{
		Id:             message.ID,
		ConversationId: message.ConversationID,
		Role:           string(message.Role),
		Content:        MessageContentToProto(message.Content),
		Status:         string(message.Status),
		CreatedAt:      timestamppb.New(message.CreatedAt),
		UpdatedAt:      timestamppb.New(message.UpdatedAt),
		LocalId:        message.LocalID,
		Attachments:    AttachmentsToProto(message.Attachments),
	}
	if message.Agent != nil {
		resp.Agent = AgentToProto(message.Agent)
	}
	return resp
}

// MessageContentToProto converts domain MessageContent to the proto response.
func MessageContentToProto(content orchestrator.MessageContent) *mobilev1.MessageContentPayload {
	resp := &mobilev1.MessageContentPayload{
		Type:        string(content.Type),
		Text:        content.Text,
		Title:       content.Title,
		Description: content.Description,
		Tool:        content.Tool,
		Query:       content.Query,
		TaskPreview: content.TaskPreview,
	}
	if s := string(content.State); s != "" {
		resp.State = ptr.Of(s)
	}
	if content.Status != "" {
		resp.Status = ptr.Of(content.Status)
	}
	if content.SelectedActionID != nil {
		resp.SelectedActionId = content.SelectedActionID
	}
	resp.Actions = actionsToProto(content.Actions)
	resp.Turns = questionsToProto(content.Turns)
	resp.Answers = questionAnswersToProto(content.Answers)
	resp.Args = argsToStructProto(content.Args)
	resp.From = ContentAgentToProto(content.From)
	resp.To = ContentAgentToProto(content.To)
	// Type-guard the variant-specific flat fields so the wire payload
	// only carries artifact fields on artifact content, workflow fields
	// on workflow content, etc. proto3 already omits zero values with
	// the current MarshalOptions, but the explicit guard documents
	// intent and keeps the domain/wire mapping unambiguous for future
	// readers.
	switch content.Type {
	case orchestrator.MessageContentTypeArtifact:
		resp.ArtifactId = content.ArtifactID
		resp.Version = int32(content.Version) // #nosec G115 -- version fits in int32
		resp.AgentSlug = content.AgentSlug
		resp.AgentName = content.AgentName
		resp.ContentPreview = content.ContentPreview
	case orchestrator.MessageContentTypeWorkflow:
		resp.WorkflowId = content.WorkflowID
		resp.WorkflowName = content.WorkflowName
		resp.ExecutionId = content.ExecutionID
		resp.AgentSlug = content.AgentSlug
		resp.AgentName = content.AgentName
	}
	return resp
}

// actionsToProto converts a slice of domain ActionItems to proto responses.
func actionsToProto(actions []orchestrator.ActionItem) []*mobilev1.ActionItemResponse {
	if len(actions) == 0 {
		return nil
	}
	result := make([]*mobilev1.ActionItemResponse, 0, len(actions))
	for _, action := range actions {
		result = append(result, &mobilev1.ActionItemResponse{
			Id:    action.ID,
			Label: action.Label,
			Style: string(action.Style),
		})
	}
	return result
}

// questionsToProto converts a slice of domain QuestionTurns to proto payloads.
func questionsToProto(turns []orchestrator.QuestionTurn) []*mobilev1.QuestionTurnPayload {
	if len(turns) == 0 {
		return nil
	}
	result := make([]*mobilev1.QuestionTurnPayload, 0, len(turns))
	for _, t := range turns {
		turn := &mobilev1.QuestionTurnPayload{
			Index: int32(t.Index), // #nosec G115 -- turn index fits in int32 NOSONAR
			Label: t.Label,
		}
		turn.Questions = make([]*mobilev1.QuestionItemPayload, 0, len(t.Questions))
		for _, q := range t.Questions {
			opts := make([]*mobilev1.QuestionOptionPayload, 0, len(q.Options))
			for _, o := range q.Options {
				opts = append(opts, &mobilev1.QuestionOptionPayload{Id: o.ID, Label: o.Label})
			}
			turn.Questions = append(turn.Questions, &mobilev1.QuestionItemPayload{
				Id:          q.ID,
				Prompt:      q.Prompt,
				Mode:        string(q.Mode),
				Options:     opts,
				AllowCustom: q.AllowCustom,
			})
		}
		result = append(result, turn)
	}
	return result
}

// questionAnswersToProto converts a slice of domain QuestionAnswers to proto payloads.
func questionAnswersToProto(answers []orchestrator.QuestionAnswer) []*mobilev1.QuestionAnswerPayload {
	if len(answers) == 0 {
		return nil
	}
	result := make([]*mobilev1.QuestionAnswerPayload, 0, len(answers))
	for _, a := range answers {
		result = append(result, &mobilev1.QuestionAnswerPayload{
			QuestionId: a.QuestionID,
			OptionIds:  append([]string(nil), a.OptionIDs...),
			CustomText: a.CustomText,
		})
	}
	return result
}

// argsToStructProto converts a map[string]any to a structpb.Struct.
// Returns nil if the map is nil or conversion fails.
func argsToStructProto(args map[string]any) *structpb.Struct {
	if args == nil {
		return nil
	}
	s, err := structpb.NewStruct(args)
	if err != nil {
		return nil
	}
	return s
}

// ContentAgentToProto converts a domain ContentAgent to the proto type.
func ContentAgentToProto(ca *orchestrator.ContentAgent) *mobilev1.ContentAgent {
	if ca == nil {
		return nil
	}
	out := &mobilev1.ContentAgent{
		Id:     ca.ID,
		Name:   ca.Name,
		Role:   ca.Role,
		Slug:   ca.Slug,
		Avatar: ca.Avatar,
	}
	if s := string(ca.Status); s != "" {
		out.Status = ptr.Of(s)
	}
	return out
}

// AttachmentsToProto converts domain Attachments to the proto response.
func AttachmentsToProto(attachments []orchestrator.Attachment) []*mobilev1.AttachmentResponse {
	if len(attachments) == 0 {
		return []*mobilev1.AttachmentResponse{}
	}
	result := make([]*mobilev1.AttachmentResponse, 0, len(attachments))
	for _, a := range attachments {
		ar := &mobilev1.AttachmentResponse{
			Id:       a.ID,
			Name:     a.Name,
			Url:      a.URL,
			Type:     string(a.Type),
			Size:     float64(a.Size),
			MimeType: a.MIMEType,
		}
		if a.Duration != nil {
			ar.Duration = int32Ptr(int32(*a.Duration)) // #nosec G115 -- duration in seconds fits in int32
		}
		result = append(result, ar)
	}
	return result
}

// AttachmentsToDomain converts proto attachment responses to domain Attachments.
func AttachmentsToDomain(attachments []*mobilev1.AttachmentResponse) []orchestrator.Attachment {
	if len(attachments) == 0 {
		return []orchestrator.Attachment{}
	}
	result := make([]orchestrator.Attachment, 0, len(attachments))
	for _, a := range attachments {
		att := orchestrator.Attachment{
			ID:       a.GetId(),
			Name:     a.GetName(),
			URL:      a.GetUrl(),
			Type:     orchestrator.AttachmentType(a.GetType()),
			Size:     int64(a.GetSize()),
			MIMEType: a.GetMimeType(),
		}
		if a.Duration != nil {
			d := int(*a.Duration)
			att.Duration = &d
		}
		result = append(result, att)
	}
	return result
}

// MentionsToDomain converts proto mention payloads to domain Mentions.
func MentionsToDomain(mentions []*mobilev1.MentionPayload) []orchestrator.Mention {
	if len(mentions) == 0 {
		return nil
	}
	result := make([]orchestrator.Mention, 0, len(mentions))
	for _, m := range mentions {
		result = append(result, orchestrator.Mention{
			AgentID:   m.GetAgentId(),
			AgentName: m.GetAgentName(),
			Offset:    int(m.GetOffset()),
			Length:    int(m.GetLength()),
		})
	}
	return result
}

// MessageContentToDomain converts a proto MessageContentPayload to domain MessageContent.
func MessageContentToDomain(payload *mobilev1.MessageContentPayload) (orchestrator.MessageContent, *merrors.Error) {
	if payload == nil {
		return orchestrator.MessageContent{}, merrors.ErrInvalidInput
	}
	contentType := orchestrator.MessageContentType(strings.TrimSpace(payload.GetType()))
	if contentType == "" {
		contentType = orchestrator.MessageContentTypeText
	}

	content := orchestrator.MessageContent{
		Type:             contentType,
		Text:             payload.GetText(),
		Title:            payload.GetTitle(),
		Description:      payload.GetDescription(),
		SelectedActionID: payload.SelectedActionId,
		Tool:             payload.GetTool(),
		State:            orchestrator.ToolState(payload.GetState()),
	}
	if len(payload.GetActions()) > 0 {
		content.Actions = make([]orchestrator.ActionItem, 0, len(payload.GetActions()))
		for _, action := range payload.GetActions() {
			content.Actions = append(content.Actions, orchestrator.ActionItem{
				ID:    action.GetId(),
				Label: action.GetLabel(),
				Style: orchestrator.ActionStyle(action.GetStyle()),
			})
		}
	}

	if content.Type != orchestrator.MessageContentTypeText {
		return orchestrator.MessageContent{}, merrors.ErrUnsupportedMessage
	}
	if strings.TrimSpace(content.Text) == "" {
		return orchestrator.MessageContent{}, merrors.ErrEmptyMessageText
	}
	if len(content.Text) > MaxMessageTextLength {
		return orchestrator.MessageContent{}, merrors.NewBusinessError("message text exceeds maximum allowed length", merrors.ErrCodeMessageTextTooLong)
	}

	return content, nil
}

func int32Ptr(v int32) *int32 {
	return &v
}
