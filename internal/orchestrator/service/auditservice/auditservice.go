// Package auditservice provides MCP audit logging operations.
package auditservice

import (
	"context"
	"errors"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
)

// repo is a local alias for the audit persistence interface.
type repo = auditrepo.Repo

// Service provides MCP audit logging operations.
type Service interface {
	WriteLog(ctx context.Context, sess *dbr.Session, entry *auditrepo.AuditLogRow) error
}

type service struct {
	repo repo
}

// New creates a new audit service, returning an error if the repo is nil.
func New(r repo) (Service, error) {
	if r == nil {
		return nil, errors.New("auditservice: repo is required")
	}
	return &service{repo: r}, nil
}

// MustNew wraps New and panics on dependency-validation errors. Intended for
// use from main/init paths where misconfiguration is unrecoverable.
func MustNew(r repo) Service {
	svc, err := New(r)
	if err != nil {
		panic(err)
	}
	return svc
}

func (s *service) WriteLog(ctx context.Context, sess *dbr.Session, entry *auditrepo.AuditLogRow) error {
	return s.repo.WriteLog(ctx, sess, entry)
}
