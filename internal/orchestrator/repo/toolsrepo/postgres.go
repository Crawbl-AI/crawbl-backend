package toolsrepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

var categoryMeta = map[string]orchestrator.AgentToolCategory{
	"search":       {ID: "search", Name: "Search", ImageURL: "https://cdn.crawbl.com/categories/search.png"},
	"files":        {ID: "files", Name: "Files", ImageURL: "https://cdn.crawbl.com/categories/files.png"},
	"memory":       {ID: "memory", Name: "Memory", ImageURL: "https://cdn.crawbl.com/categories/memory.png"},
	"scheduling":   {ID: "scheduling", Name: "Scheduling", ImageURL: "https://cdn.crawbl.com/categories/scheduling.png"},
	"notification": {ID: "notification", Name: "Notification", ImageURL: "https://cdn.crawbl.com/categories/notification.png"},
	"context":      {ID: "context", Name: "Context", ImageURL: "https://cdn.crawbl.com/categories/context.png"},
	"utility":      {ID: "utility", Name: "Utility", ImageURL: "https://cdn.crawbl.com/categories/utility.png"},
	"integration":  {ID: "integration", Name: "Integration", ImageURL: "https://cdn.crawbl.com/categories/integration.png"},
	"shell":        {ID: "shell", Name: "Shell", ImageURL: "https://cdn.crawbl.com/categories/shell.png"},
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
	cat, ok := categoryMeta[row.Category]
	if !ok {
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
