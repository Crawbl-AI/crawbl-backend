package server

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

const testSigningKey = "test-key-for-agentruntime-interceptor-xxxxxxxxxxxx"

// TestHMACAuth_Unary exercises the unary interceptor's accept/reject behavior
// against every failure mode we expect in production: missing metadata,
// missing header, wrong scheme, empty token, invalid signature, wrong key,
// and happy path.
func TestHMACAuth_Unary(t *testing.T) {
	unary, _ := HMACAuth(testSigningKey)

	validToken := crawblhmac.GenerateToken(testSigningKey, "user-abc", "ws-xyz")
	foreignToken := crawblhmac.GenerateToken("some-other-key", "user-abc", "ws-xyz")

	type wantCode = codes.Code
	cases := []struct {
		name       string
		md         metadata.MD
		wantCode   wantCode
		wantUserID string
		wantWSID   string
	}{
		{
			name:     "no metadata at all",
			md:       nil,
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "metadata without authorization header",
			md:       metadata.Pairs("x-other", "value"),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "empty authorization value",
			md:       metadata.Pairs("authorization", "   "),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "non-bearer scheme rejected",
			md:       metadata.Pairs("authorization", "Basic "+validToken),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "bearer with empty token",
			md:       metadata.Pairs("authorization", "Bearer "),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "malformed token rejected",
			md:       metadata.Pairs("authorization", "Bearer not.a.valid.token"),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "token signed with wrong key rejected",
			md:       metadata.Pairs("authorization", "Bearer "+foreignToken),
			wantCode: codes.Unauthenticated,
		},
		{
			name:       "bare token without Bearer prefix accepted",
			md:         metadata.Pairs("authorization", validToken),
			wantCode:   codes.OK,
			wantUserID: "user-abc",
			wantWSID:   "ws-xyz",
		},
		{
			name:       "Bearer prefix token accepted",
			md:         metadata.Pairs("authorization", "Bearer "+validToken),
			wantCode:   codes.OK,
			wantUserID: "user-abc",
			wantWSID:   "ws-xyz",
		},
		{
			name:       "bearer prefix lowercase accepted",
			md:         metadata.Pairs("authorization", "bearer "+validToken),
			wantCode:   codes.OK,
			wantUserID: "user-abc",
			wantWSID:   "ws-xyz",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.md != nil {
				ctx = metadata.NewIncomingContext(ctx, tc.md)
			}
			var gotPrincipal Principal
			handler := func(ctx context.Context, _ any) (any, error) {
				p, ok := PrincipalFromContext(ctx)
				if !ok {
					t.Fatalf("principal missing from context on happy path")
				}
				gotPrincipal = p
				return "ok", nil
			}
			resp, err := unary(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/test.Test/Method"}, handler)
			if tc.wantCode == codes.OK {
				if err != nil {
					t.Fatalf("expected OK, got error: %v", err)
				}
				if resp != "ok" {
					t.Fatalf("expected handler to run, got %v", resp)
				}
				if gotPrincipal.UserID != tc.wantUserID || gotPrincipal.WorkspaceID != tc.wantWSID {
					t.Fatalf("principal mismatch: got %+v want user_id=%q workspace_id=%q", gotPrincipal, tc.wantUserID, tc.wantWSID)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error with code %v, got nil", tc.wantCode)
			}
			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("expected grpc status error, got %T: %v", err, err)
			}
			if st.Code() != tc.wantCode {
				t.Fatalf("code mismatch: got %v want %v", st.Code(), tc.wantCode)
			}
		})
	}
}
