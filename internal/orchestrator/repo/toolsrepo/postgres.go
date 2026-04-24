// Package toolsrepo provides PostgreSQL-based implementation of the ToolsRepo interface.
// It handles persistence, seeding, and retrieval of agent tool definitions.
package toolsrepo

import (
	"context"
	"strings"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// getToolCategoryMap returns a lazily-initialized lookup map from category ID
// to AgentToolCategory. Built once and reused for all rowToTool calls.
func getToolCategoryMap() map[string]orchestrator.AgentToolCategory {
	toolCategoryMapOnce.Do(func() {
		cats := agentruntimetools.ToolCategories()
		toolCategoryMap = make(map[string]orchestrator.AgentToolCategory, len(cats))
		for _, c := range cats {
			toolCategoryMap[string(c.ID)] = orchestrator.AgentToolCategory{
				ID:       string(c.ID),
				Name:     c.Name,
				ImageURL: c.ImageURL,
			}
		}
	})
	return toolCategoryMap
}

func New() *toolsRepo {
	return &toolsRepo{}
}

func (r *toolsRepo) List(ctx context.Context, sess orchestratorrepo.SessionRunner, limit, offset int, category string) ([]orchestrator.AgentTool, *merrors.Error) {
	query := sess.Select(toolColumns...).
		From("tools").
		OrderAsc("sort_order").
		OrderAsc("name")

	if cat := strings.TrimSpace(category); cat != "" {
		query = query.Where("category = ?", cat)
	}
	if limit > 0 {
		query = query.Limit(uint64(limit))
	}
	if offset > 0 {
		query = query.Offset(uint64(offset))
	}

	var rows []orchestratorrepo.ToolRow
	_, err := query.LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list tools")
	}

	tools := make([]orchestrator.AgentTool, 0, len(rows))
	for _, row := range rows {
		tools = append(tools, rowToTool(row))
	}

	return tools, nil
}

func (r *toolsRepo) Count(ctx context.Context, sess orchestratorrepo.SessionRunner, category string) (int, *merrors.Error) {
	query := sess.Select("COUNT(*)").From("tools")
	if cat := strings.TrimSpace(category); cat != "" {
		query = query.Where("category = ?", cat)
	}

	var count int
	err := query.LoadOneContext(ctx, &count)
	if err != nil {
		return 0, merrors.WrapStdServerError(err, "count tools")
	}

	return count, nil
}

func (r *toolsRepo) GetByNames(ctx context.Context, sess orchestratorrepo.SessionRunner, names []string) ([]orchestrator.AgentTool, *merrors.Error) {
	if len(names) == 0 {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.ToolRow
	_, err := sess.Select(toolColumns...).
		From("tools").
		Where("name IN ?", names).
		OrderAsc("sort_order").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "get tools by names")
	}

	tools := make([]orchestrator.AgentTool, 0, len(rows))
	for _, row := range rows {
		tools = append(tools, rowToTool(row))
	}

	return tools, nil
}

// Seed upserts the provided tool definitions into the database.
// Each tool is identified by its unique name; existing rows are updated in place.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *toolsRepo) Seed(ctx context.Context, sess orchestratorrepo.SessionRunner, tools []orchestratorrepo.ToolRow) *merrors.Error {
	const query = `
INSERT INTO tools (name, display_name, description, category, icon_url, sort_order, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
	display_name = EXCLUDED.display_name,
	description  = EXCLUDED.description,
	category     = EXCLUDED.category,
	icon_url     = EXCLUDED.icon_url,
	sort_order   = EXCLUDED.sort_order`

	for _, tool := range tools {
		_, err := sess.InsertBySql(query,
			tool.Name, tool.DisplayName, tool.Description, tool.Category,
			tool.IconURL, tool.SortOrder, tool.CreatedAt,
		).ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "upsert tool")
		}
	}

	return nil
}

func rowToTool(row orchestratorrepo.ToolRow) orchestrator.AgentTool {
	catMap := getToolCategoryMap()

	var cat orchestrator.AgentToolCategory
	if meta, ok := catMap[row.Category]; ok {
		cat = meta
	} else {
		cat = orchestrator.AgentToolCategory{
			ID:   row.Category,
			Name: row.Category,
		}
	}

	return orchestrator.AgentTool{
		Name:        row.Name,
		DisplayName: row.DisplayName,
		Description: row.Description,
		Category:    cat,
		IconURL:     row.IconURL,
	}
}
