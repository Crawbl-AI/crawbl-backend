package httpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

const (
	maxTimestampAge = 2 * time.Minute
	maxClockSkew    = 30 * time.Second
)

func AuthMiddleware(cfg *MiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, source := authTokenFromRequest(r)
			if token == "" {
				WriteErrorResponse(w, http.StatusUnauthorized, merrors.PublicMessage(merrors.ErrUnauthorized))
				return
			}

			if err := validateDeviceHeaders(cfg, r, source, token); err != nil {
				WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
				return
			}

			principal, err := cfg.IdentityVerifier.Verify(r.Context(), token)
			if err != nil {
				WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
				return
			}

			ctx := ContextWithPrincipal(r.Context(), principal)
			ctx = ContextWithRequestMetadata(ctx, &RequestMetadata{
				TokenSource: source,
				DeviceInfo:  strings.TrimSpace(r.Header.Get(XDeviceInfoHeader)),
				DeviceID:    strings.TrimSpace(r.Header.Get(XDeviceIDHeader)),
				Version:     strings.TrimSpace(r.Header.Get(XVersionHeader)),
				Timezone:    strings.TrimSpace(r.Header.Get(XTimezoneHeader)),
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func authTokenFromRequest(r *http.Request) (string, AuthTokenSource) {
	if token := strings.TrimSpace(r.Header.Get(XTokenHeader)); token != "" {
		return token, AuthTokenSourceXToken
	}

	headerValue := strings.TrimSpace(r.Header.Get(AuthorizationHeader))
	if headerValue == "" {
		return "", AuthTokenSourceUnknown
	}

	if strings.HasPrefix(headerValue, BearerPrefix) {
		token := strings.TrimSpace(strings.TrimPrefix(headerValue, BearerPrefix))
		if token != "" {
			return token, AuthTokenSourceAuthorization
		}
	}

	return "", AuthTokenSourceUnknown
}

func validateDeviceHeaders(cfg *MiddlewareConfig, r *http.Request, source AuthTokenSource, token string) error {
	if shouldSkipDeviceAuth(cfg) {
		return nil
	}

	if source != AuthTokenSourceXToken {
		return nil
	}

	if strings.TrimSpace(cfg.HMACSecret) == "" {
		return merrors.ErrUnauthorized
	}

	timestamp := strings.TrimSpace(r.Header.Get(XTimestampHeader))
	signature := strings.TrimSpace(r.Header.Get(XSignatureHeader))
	deviceInfo := strings.TrimSpace(r.Header.Get(XDeviceInfoHeader))
	version := strings.TrimSpace(r.Header.Get(XVersionHeader))
	if timestamp == "" || signature == "" || deviceInfo == "" || version == "" {
		return merrors.ErrUnauthorized
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return merrors.ErrUnauthorized
	}

	now := time.Now().UTC()
	age := now.Sub(parsedTimestamp)
	if age > maxTimestampAge || age < -maxClockSkew {
		return merrors.ErrUnauthorized
	}

	expectedSignature := GenerateHMAC(cfg.HMACSecret, token+":"+timestamp)
	receivedMAC, err := hex.DecodeString(signature)
	if err != nil {
		return merrors.ErrUnauthorized
	}
	expectedMAC, err := hex.DecodeString(expectedSignature)
	if err != nil {
		return merrors.ErrUnauthorized
	}
	if !hmac.Equal(receivedMAC, expectedMAC) {
		return merrors.ErrUnauthorized
	}

	return nil
}

func GenerateHMAC(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func shouldSkipDeviceAuth(cfg *MiddlewareConfig) bool {
	if cfg == nil {
		return true
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Environment)) {
	case EnvironmentLocal, "test":
		return true
	default:
		return false
	}
}
