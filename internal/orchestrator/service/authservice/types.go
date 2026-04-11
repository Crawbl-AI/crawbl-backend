// Package authservice provides authentication and user management services for the Crawbl platform.
// It handles user sign-in, sign-up, profile management, legal document acceptance, and push token storage.
// The service implements a hybrid auth pattern where sign-in creates users on first access and
// sign-up returns existing users idempotently.
package authservice

import (
	"context"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagequotarepo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
)

// usageQuotaCreator is the minimal repo surface the authservice needs.
// Defined here at the consumer, per interface-segregation practice.
type usageQuotaCreator interface {
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *usagequotarepo.Row) error
}

// service implements the orchestratorservice.AuthService interface.
type service struct {
	userRepo              orchestratorrepo.UserRepo
	workspaceBootstrapper orchestratorservice.WorkspaceBootstrapper
	legalDocuments        *legalDocumentsConfig
	usageQuotaRepo        usageQuotaCreator
}

// legalDocumentsConfig holds the URLs and versions for legal documents
// that users must accept (Terms of Service and Privacy Policy).
type legalDocumentsConfig struct {
	TermsOfService        string
	PrivacyPolicy         string
	TermsOfServiceVersion string
	PrivacyPolicyVersion  string
}
