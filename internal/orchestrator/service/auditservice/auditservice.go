// Package auditservice provides MCP audit logging operations.
package auditservice

import (
	"context"

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

// New creates a new audit service. Panics if repo is nil.
func New(r repo) Service {
	if r == nil {
		panic("auditservice: repo is nil")
	}
	return &service{repo: r}
}

func (s *service) WriteLog(ctx context.Context, sess *dbr.Session, entry *auditrepo.AuditLogRow) error {
	return s.repo.WriteLog(ctx, sess, entry)
}
