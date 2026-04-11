// Package usagequotarepo provides persistence for usage quota assignments.
package usagequotarepo

import (
	"context"
	"time"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Row is the table row for usage_quotas.
type Row struct {
	UserID      string    `db:"user_id"`
	PlanID      string    `db:"plan_id"`
	EffectiveAt time.Time `db:"effective_at"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type postgres struct{}

// New returns a Postgres-backed usage quota repository.
func New() *postgres { return &postgres{} }

// Create inserts a new usage_quotas row assigning a plan to a user.
func (p *postgres) Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *Row) error {
	if row == nil {
		return merrors.ErrInvalidInput
	}
	_, err := sess.InsertInto("usage_quotas").
		Pair("user_id", row.UserID).
		Pair("plan_id", row.PlanID).
		Pair("effective_at", row.EffectiveAt).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "usagequotarepo: create")
	}
	return nil
}
