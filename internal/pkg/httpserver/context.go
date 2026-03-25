package httpserver

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

func ContextWithPrincipal(ctx context.Context, principal *orchestrator.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}

func PrincipalFromContext(ctx context.Context) (*orchestrator.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(*orchestrator.Principal)
	return principal, ok
}

func ContextWithRequestMetadata(ctx context.Context, metadata *RequestMetadata) context.Context {
	return context.WithValue(ctx, requestMetadataContextKey, metadata)
}

func RequestMetadataFromContext(ctx context.Context) (*RequestMetadata, bool) {
	metadata, ok := ctx.Value(requestMetadataContextKey).(*RequestMetadata)
	return metadata, ok
}
