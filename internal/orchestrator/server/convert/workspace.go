package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
)

// WorkspaceToProto converts a domain Workspace to the proto response.
func WorkspaceToProto(workspace *orchestrator.Workspace) *mobilev1.WorkspaceResponse {
	resp := &mobilev1.WorkspaceResponse{
		Id:        workspace.ID,
		Name:      workspace.Name,
		CreatedAt: timestamppb.New(workspace.CreatedAt),
		UpdatedAt: timestamppb.New(workspace.UpdatedAt),
	}

	if workspace.Runtime != nil {
		resp.Runtime = &mobilev1.WorkspaceRuntimeResponse{
			Status:   string(workspace.Runtime.Status),
			Phase:    workspace.Runtime.Phase,
			Verified: workspace.Runtime.Verified,
		}
	}

	return resp
}

// EnrichWorkspaceRuntime attaches workspace summary data to the runtime.
func EnrichWorkspaceRuntime(resp *mobilev1.WorkspaceResponse, summary *orchestrator.WorkspaceSummary) {
	if resp.Runtime == nil || summary == nil {
		return
	}
	resp.Runtime.TotalAgents = int64(summary.TotalAgents)
	if summary.LastMessagePreview != nil {
		resp.Runtime.LastMessagePreview = &mobilev1.LastMessagePreviewResponse{
			Text:       summary.LastMessagePreview.Text,
			SenderName: summary.LastMessagePreview.SenderName,
			Timestamp:  timestamppb.New(summary.LastMessagePreview.Timestamp),
		}
	}
}
