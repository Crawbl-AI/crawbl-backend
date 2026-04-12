package chatservice

import (
	"context"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// RespondToActionCard records the user's selection for an action card message.
// It fetches the message by ID, sets the selected_action_id in its content,
// updates the timestamp, and persists the change.
func (s *Service) RespondToActionCard(ctx context.Context, opts *orchestratorservice.RespondToActionCardOpts) (*orchestrator.Message, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	// Verify workspace ownership before allowing action card response.
	if _, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	msg, mErr := s.messageRepo.GetByID(ctx, sess, opts.MessageID)
	if mErr != nil {
		return nil, mErr
	}

	// Verify the message belongs to the verified workspace by checking its
	// conversation is scoped to that workspace. GetByID filters by workspaceID,
	// so a mismatch returns not-found which we surface as unauthorized to
	// avoid leaking that the message exists in another workspace.
	if _, mErr := s.conversationRepo.GetByID(ctx, sess, opts.WorkspaceID, msg.ConversationID); mErr != nil {
		return nil, merrors.ErrUnauthorized
	}

	msg.Content.SelectedActionID = &opts.ActionID
	msg.UpdatedAt = time.Now().UTC()

	if mErr := s.messageRepo.Save(ctx, sess, msg); mErr != nil {
		return nil, mErr
	}

	return msg, nil
}
