package toolsrepo

import (
	"context"
	"strings"
	"sync"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

var (
	toolCategoryMap     map[string]orchestrator.AgentToolCategory
	toolCategoryMapOnce sync.Once
)

// getToolCategoryMap returns a lazily-initialized lookup map from category ID
// to AgentToolCategory. Built once and reused for all rowToTool calls.
func getToolCategoryMap() map[string]orchestrator.AgentToolCategory {
	toolCategoryMapOnce.Do(func() {
		cats := seed.ToolCategories()
		toolCategoryMap = make(map[string]orchestrator.AgentToolCategory, len(cats))
		for _, c := range cats {
			toolCategoryMap[c.ID] = orchestrator.AgentToolCategory{
				ID:       c.ID,
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
	if sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	query := sess.Select(orchestratorrepo.Columns(toolColumns...)...).
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
	if sess == nil {
		return 0, merrors.ErrInvalidInput
	}

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
	if sess == nil || len(names) == 0 {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.ToolRow
	_, err := sess.Select(orchestratorrepo.Columns(toolColumns...)...).
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

func (r *toolsRepo) Seed(ctx context.Context, sess orchestratorrepo.SessionRunner, tools []orchestratorrepo.ToolRow) *merrors.Error {
	if sess == nil {
		return merrors.ErrInvalidInput
	}

	for _, tool := range tools {
		var existing orchestratorrepo.ToolRow
		err := sess.Select(orchestratorrepo.Columns(toolColumns...)...).
			From("tools").
			Where("name = ?", tool.Name).
			LoadOneContext(ctx, &existing)
		switch {
		case err == nil:
			_, err = sess.Update("tools").
				Set("display_name", tool.DisplayName).
				Set("description", tool.Description).
				Set("category", tool.Category).
				Set("icon_url", tool.IconURL).
				Set("sort_order", tool.SortOrder).
				Where("name = ?", tool.Name).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update tool")
			}
		case database.IsRecordNotFoundError(err):
			_, err = sess.InsertInto("tools").
				Pair("name", tool.Name).
				Pair("display_name", tool.DisplayName).
				Pair("description", tool.Description).
				Pair("category", tool.Category).
				Pair("icon_url", tool.IconURL).
				Pair("sort_order", tool.SortOrder).
				Pair("created_at", tool.CreatedAt).
				ExecContext(ctx)
			if err != nil {
				if database.IsRecordExistsError(err) {
					_, err = sess.Update("tools").
						Set("display_name", tool.DisplayName).
						Set("description", tool.Description).
						Set("category", tool.Category).
						Set("icon_url", tool.IconURL).
						Set("sort_order", tool.SortOrder).
						Where("name = ?", tool.Name).
						ExecContext(ctx)
					if err != nil {
						return merrors.WrapStdServerError(err, "update tool after duplicate insert")
					}
					continue
				}
				return merrors.WrapStdServerError(err, "insert tool")
			}
		default:
			return merrors.WrapStdServerError(err, "select tool by name for seed")
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
