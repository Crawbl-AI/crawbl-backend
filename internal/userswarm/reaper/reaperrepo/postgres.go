// Package reaperrepo provides persistence queries used by the reaper cleanup job.
package reaperrepo

import (
	"context"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// UserRow holds the minimal user fields needed by the reaper.
type UserRow struct {
	ID        string     `db:"id"`
	Subject   string     `db:"subject"`
	Email     string     `db:"email"`
	CreatedAt time.Time  `db:"created_at"`
	DeletedAt *time.Time `db:"deleted_at"`
}

// Repo provides user persistence queries used by the reaper cleanup job.
type Repo struct{}

// New returns a Postgres-backed reaper repository.
func New() *Repo { return &Repo{} }

// FindUserByID looks up a single user row by primary key, including soft-deleted
// users. Returns (nil, nil) when no row exists so the caller can distinguish
// "missing" from "error".
func (r *Repo) FindUserByID(ctx context.Context, sess database.SessionRunner, id string) (*UserRow, error) {
	if id == "" {
		return nil, nil
	}
	var rows []UserRow
	_, err := sess.Select("id", "subject", "email", "created_at", "deleted_at").
		From("users").
		Where("id = ?", id).
		Limit(1).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// CountActiveByID returns the count of non-soft-deleted users with the given ID.
// A count > 0 means the user is alive and active.
func (r *Repo) CountActiveByID(ctx context.Context, sess database.SessionRunner, id string) (int, error) {
	var count int
	err := sess.Select("COUNT(*)").
		From("users").
		Where("id = ? AND deleted_at IS NULL", id).
		LoadOneContext(ctx, &count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// SoftDeleteUser marks a user as deleted by setting deleted_at and updated_at to
// the current UTC time. The WHERE clause includes "deleted_at IS NULL" so that a
// concurrent or prior soft-delete is a safe no-op rather than overwriting the
// original deletion timestamp.
func (r *Repo) SoftDeleteUser(ctx context.Context, sess database.SessionRunner, id string) error {
	now := time.Now().UTC()
	_, err := sess.Update("users").
		Set("deleted_at", now).
		Set("updated_at", now).
		Where("id = ? AND deleted_at IS NULL", id).
		ExecContext(ctx)
	return err
}
