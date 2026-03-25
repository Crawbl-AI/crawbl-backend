package authservice

import (
	authrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
)

type (
	userRepo              = authrepo.UserRepo
	workspaceBootstrapper = orchestratorservice.WorkspaceBootstrapper
	sessionRunner         = authrepo.SessionRunner
)

type service struct {
	userRepo              authrepo.UserRepo
	workspaceBootstrapper orchestratorservice.WorkspaceBootstrapper
	legalDocuments        *legalDocumentsConfig
}

type legalDocumentsConfig struct {
	TermsOfService        string
	PrivacyPolicy         string
	TermsOfServiceVersion string
	PrivacyPolicyVersion  string
}
