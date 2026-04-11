// Package authservice — ports.go declares the narrow repository contracts
// this package depends on. Per project convention, interfaces are defined
// at the consumer, not the producer: each method listed here corresponds
// to a call site inside authservice. The concrete *userRepo struct
// exported by internal/orchestrator/repo/userrepo satisfies these
// implicitly via Go's structural typing.
package authservice

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// userStore is the subset of user persistence operations authservice needs.
// Kept package-private because no collaborator outside this package holds
// it. Widening it requires adding a matching call site in the service.
type userStore interface {
	GetBySubject(ctx context.Context, sess orchestratorrepo.SessionRunner, subject string) (*orchestrator.User, *merrors.Error)
	GetUser(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (*orchestrator.User, *merrors.Error)
	CreateUser(ctx context.Context, opts *orchestratorrepo.CreateUserOpts) *merrors.Error
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) *merrors.Error
	SavePushToken(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, pushToken string) *merrors.Error
	ClearPushTokens(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) *merrors.Error
	IsUserDeleted(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (bool, *merrors.Error)
	CheckNicknameExists(ctx context.Context, sess orchestratorrepo.SessionRunner, nickname string) (bool, *merrors.Error)
}
