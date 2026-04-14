// Package convert provides domain-to-proto and proto-to-domain converters
// for the orchestrator mobile API. Each function replaces a former dto
// mapper or inline construction in the handler layer.
package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
)

// AgentToProto converts a domain Agent to the proto response.
func AgentToProto(agent *orchestrator.Agent) *mobilev1.AgentResponse {
	if agent == nil {
		return &mobilev1.AgentResponse{}
	}
	return &mobilev1.AgentResponse{
		Id:     agent.ID,
		Name:   agent.Name,
		Role:   agent.Role,
		Slug:   agent.Slug,
		Avatar: agent.AvatarURL,
		Status: string(agent.Status),
	}
}

// AgentDetailToProto converts a domain AgentDetails to the proto response.
func AgentDetailToProto(d *orchestrator.AgentDetails) *mobilev1.AgentDetailResponse {
	resp := &mobilev1.AgentDetailResponse{
		Id:          d.ID,
		WorkspaceId: d.WorkspaceID,
		Name:        d.Name,
		Role:        d.Role,
		Slug:        d.Slug,
		CreatedAt:   timestamppb.New(d.CreatedAt),
		Description: d.Description,
		AvatarUrl:   d.AvatarURL,
		Status:      string(d.Status),
		SortOrder:   int64(d.SortOrder),
		Skills:      []string{},
		Stats: &mobilev1.AgentStatsResponse{
			TotalMessages:         int64(d.Stats.TotalMessages),
			TotalTokensUsed:       d.Stats.TotalTokensUsed,
			TotalPromptTokens:     d.Stats.TotalPromptTokens,
			TotalCompletionTokens: d.Stats.TotalCompletionTokens,
			TotalRequests:         int64(d.Stats.TotalRequests),
		},
	}
	if !d.UpdatedAt.IsZero() {
		t := timestamppb.New(d.UpdatedAt)
		resp.UpdatedAt = t
	}
	return resp
}

// AgentToolToProto converts a domain AgentTool to the proto response.
func AgentToolToProto(t orchestrator.AgentTool) *mobilev1.AgentToolResponse {
	return &mobilev1.AgentToolResponse{
		Name:        t.Name,
		DisplayName: t.DisplayName,
		Description: t.Description,
		Category: &mobilev1.AgentToolCategoryResponse{
			Id:       t.Category.ID,
			Name:     t.Category.Name,
			ImageUrl: t.Category.ImageURL,
		},
		IconUrl: t.IconURL,
	}
}

// OffsetPaginationToProto converts a domain OffsetPagination to the proto response.
func OffsetPaginationToProto(p orchestrator.OffsetPagination) *mobilev1.OffsetPagination {
	return &mobilev1.OffsetPagination{
		Total:   int64(p.Total),
		Limit:   int64(p.Limit),
		Offset:  int64(p.Offset),
		HasNext: p.HasNext,
	}
}
