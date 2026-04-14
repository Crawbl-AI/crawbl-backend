package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/ptr"
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
		rt := &mobilev1.WorkspaceRuntimeResponse{
			Verified: workspace.Runtime.Verified,
		}
		if s := string(workspace.Runtime.Status); s != "" {
			rt.Status = ptr.Of(s)
		}
		if workspace.Runtime.Phase != "" {
			rt.Phase = ptr.Of(workspace.Runtime.Phase)
		}
		resp.Runtime = rt
	}

	return resp
}

// EnrichWorkspaceRuntime attaches workspace summary data to the runtime.
func EnrichWorkspaceRuntime(resp *mobilev1.WorkspaceResponse, summary *orchestrator.WorkspaceSummary) {
	if resp.Runtime == nil || summary == nil {
		return
	}
	resp.Runtime.TotalAgents = int32(summary.TotalAgents) // #nosec G115 -- agent count per workspace fits in int32; proto uses int32 so the JSON encoding is a number, not a string.
	if summary.LastMessagePreview != nil {
		resp.Runtime.LastMessagePreview = &mobilev1.LastMessagePreviewResponse{
			Text:       summary.LastMessagePreview.Text,
			SenderName: summary.LastMessagePreview.SenderName,
			Timestamp:  timestamppb.New(summary.LastMessagePreview.Timestamp),
		}
	}
}
