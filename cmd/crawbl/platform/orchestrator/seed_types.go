package orchestrator

import "time"

// modelRow is a scan target for the models table used during seed upserts.
type modelRow struct {
	ID          string    `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
}

// toolCategoryRow is a scan target for the tool_categories table used during seed upserts.
type toolCategoryRow struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	ImageURL  string    `db:"image_url"`
	SortOrder int       `db:"sort_order"`
	CreatedAt time.Time `db:"created_at"`
}

// integrationCategoryRow is a scan target for the integration_categories table used during seed upserts.
type integrationCategoryRow struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	ImageURL  string    `db:"image_url"`
	SortOrder int       `db:"sort_order"`
	CreatedAt time.Time `db:"created_at"`
}

// integrationProviderRow is a scan target for the integration_providers table used during seed upserts.
type integrationProviderRow struct {
	Provider    string    `db:"provider"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	IconURL     string    `db:"icon_url"`
	CategoryID  string    `db:"category_id"`
	IsEnabled   bool      `db:"is_enabled"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
}
