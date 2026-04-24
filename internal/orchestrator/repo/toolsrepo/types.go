package toolsrepo

import (
	"sync"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

type toolsRepo struct{}

var toolColumns = []any{
	"name",
	"display_name",
	"description",
	"category",
	"icon_url",
	"sort_order",
	"created_at",
}

var (
	toolCategoryMap     map[string]orchestrator.AgentToolCategory
	toolCategoryMapOnce sync.Once
)
