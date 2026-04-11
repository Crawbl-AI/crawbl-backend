package middleware

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// ContextWithPrincipal stores the authenticated principal in the context.
// Use PrincipalFromContext to retrieve it later.
func ContextWithPrincipal(ctx context.Context, principal *orchestrator.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}

// PrincipalFromContext retrieves the authenticated principal from the context.
// Returns nil and false if no principal is found.
func PrincipalFromContext(ctx context.Context) (*orchestrator.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(*orchestrator.Principal)
	return principal, ok
}

// ContextWithRequestMetadata stores request metadata in the context.
// Use RequestMetadataFromContext to retrieve it later.
func ContextWithRequestMetadata(ctx context.Context, metadata *RequestMetadata) context.Context {
	return context.WithValue(ctx, requestMetadataContextKey, metadata)
}

// RequestMetadataFromContext retrieves request metadata from the context.
// Returns nil and false if no metadata is found.
func RequestMetadataFromContext(ctx context.Context) (*RequestMetadata, bool) {
	metadata, ok := ctx.Value(requestMetadataContextKey).(*RequestMetadata)
	return metadata, ok
}
