package orchestrator

import "time"

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
