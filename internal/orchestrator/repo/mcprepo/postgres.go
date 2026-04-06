package mcprepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
)

type postgres struct{}

// New returns a Postgres-backed MCP repository.
func New() Repo { return &postgres{} }

func (p *postgres) GetUserByID(ctx context.Context, sess *dbr.Session, userID string) (*UserRow, error) {
	var row UserRow
	err := sess.Select("id", "email", "nickname", "name", "surname", "country_code", "created_at").
		From("users").
		Where("id = ?", userID).
		LoadOneContext(ctx, &row)
	if err != nil {
		return nil, fmt.Errorf("mcprepo: get user %s: %w", userID, err)
	}
	return &row, nil
}

func (p *postgres) GetUserPreferences(ctx context.Context, sess *dbr.Session, userID string) (*UserPreferencesRow, error) {
	var row UserPreferencesRow
	err := sess.Select("platform_theme", "platform_language", "currency_code").
		From("user_preferences").
		Where("user_id = ?", userID).
		LoadOneContext(ctx, &row)
	if err != nil {
		return nil, fmt.Errorf("mcprepo: get preferences for user %s: %w", userID, err)
	}
	return &row, nil
}

func (p *postgres) GetPushToken(ctx context.Context, sess *dbr.Session, userID string) (string, error) {
	var token string
	err := sess.Select("push_token").
		From("user_push_tokens").
		Where("user_id = ?", userID).
		LoadOneContext(ctx, &token)
	if err != nil {
		return "", fmt.Errorf("mcprepo: get push token for user %s: %w", userID, err)
	}
	return token, nil
}

func (p *postgres) SearchMessages(ctx context.Context, sess *dbr.Session, conversationID, query string, limit int) ([]MessageSearchRow, error) {
	pattern := "%" + sanitizeLike(query) + "%"
	var rows []MessageSearchRow
	_, err := sess.Select("id", "role", "content::text as content", "created_at").
		From("messages").
		Where("conversation_id = ?", conversationID).
		Where("content::text ILIKE ?", pattern).
		OrderDir("created_at", false).
		Limit(uint64(limit)).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("mcprepo: search messages in conversation %s: %w", conversationID, err)
	}
	return rows, nil
}

func (p *postgres) CreateAgentMessage(ctx context.Context, sess *dbr.Session, row *AgentMessageRow) error {
	_, err := sess.InsertInto("agent_messages").
		Pair("id", row.ID).
		Pair("workspace_id", row.WorkspaceID).
		Pair("conversation_id", row.ConversationID).
		Pair("from_agent_id", row.FromAgentID).
		Pair("from_agent_slug", row.FromAgentSlug).
		Pair("to_agent_id", row.ToAgentID).
		Pair("to_agent_slug", row.ToAgentSlug).
		Pair("request_text", row.RequestText).
		Pair("status", row.Status).
		Pair("depth", row.Depth).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("mcprepo: create agent message %s: %w", row.ID, err)
	}
	return nil
}

func (p *postgres) UpdateAgentMessageCompleted(ctx context.Context, sess *dbr.Session, id, responseText string, durationMs int64) error {
	_, err := sess.Update("agent_messages").
		Set("status", "completed").
		Set("response_text", responseText).
		Set("duration_ms", durationMs).
		Set("completed_at", time.Now().UTC()).
		Where("id = ?", id).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("mcprepo: complete agent message %s: %w", id, err)
	}
	return nil
}

func (p *postgres) UpdateAgentMessageFailed(ctx context.Context, sess *dbr.Session, id, errMsg string, durationMs int64) error {
	_, err := sess.Update("agent_messages").
		Set("status", "failed").
		Set("error_message", errMsg).
		Set("duration_ms", durationMs).
		Set("completed_at", time.Now().UTC()).
		Where("id = ?", id).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("mcprepo: fail agent message %s: %w", id, err)
	}
	return nil
}

func (p *postgres) GetMaxAgentMessageDepth(ctx context.Context, sess *dbr.Session, workspaceID, conversationID string) (int, error) {
	var depth int
	err := sess.Select("COALESCE(MAX(depth), -1)").
		From("agent_messages").
		Where("workspace_id = ? AND conversation_id = ? AND status IN ('pending', 'running')",
			workspaceID, conversationID).
		LoadOneContext(ctx, &depth)
	if err != nil {
		return -1, fmt.Errorf("mcprepo: get max agent message depth: %w", err)
	}
	return depth, nil
}

func (p *postgres) UpdateArtifactStatus(ctx context.Context, sess *dbr.Session, artifactID, status string) error {
	_, err := sess.Update("artifacts").
		Set("status", status).
		Set("updated_at", time.Now().UTC()).
		Where("id = ?", artifactID).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("mcprepo: update artifact %s status to %s: %w", artifactID, status, err)
	}
	return nil
}

// sanitizeLike escapes LIKE wildcards in user input to prevent injection.
func sanitizeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
