package chatservice

import (
	"context"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// RespondToActionCard records the user's selection for an action card message.
// It fetches the message by ID, sets the selected_action_id in its content,
// updates the timestamp, and persists the change.
func (s *service) RespondToActionCard(ctx context.Context, opts *orchestratorservice.RespondToActionCardOpts) (*orchestrator.Message, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	// Verify workspace ownership before allowing action card response.
	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	msg, mErr := s.messageRepo.GetByID(ctx, opts.Sess, opts.MessageID)
	if mErr != nil {
		return nil, mErr
	}

	msg.Content.SelectedActionID = &opts.ActionID
	msg.UpdatedAt = time.Now().UTC()

	if mErr := s.messageRepo.Save(ctx, opts.Sess, msg); mErr != nil {
		return nil, mErr
	}

	return msg, nil
}
