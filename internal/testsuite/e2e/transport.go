package e2e

import "net/http"

// authTransport injects auth headers on every outgoing request.
// When e2eToken is set, it sends X-E2E-Token/UID/Email/Name headers
// (for gateway mode). Otherwise, it sends X-Firebase-UID/Email/Name
// (for direct/port-forward mode).
type authTransport struct {
	base     http.RoundTripper
	uid      string
	email    string
	name     string
	e2eToken string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.e2eToken != "" {
		// Gateway mode: e2e bypass headers.
		req.Header.Set("X-E2E-Token", t.e2eToken)
		req.Header.Set("X-E2E-UID", t.uid)
		req.Header.Set("X-E2E-Email", t.email)
		req.Header.Set("X-E2E-Name", t.name)
	} else {
		// Direct mode: Firebase-forwarded headers (port-forward to orchestrator).
		req.Header.Set("X-Firebase-UID", t.uid)
		req.Header.Set("X-Firebase-Email", t.email)
		req.Header.Set("X-Firebase-Name", t.name)
	}

	req.Header.Set("X-Device-Info", "crawbl-e2e-test")
	req.Header.Set("X-Device-ID", "e2e-device-001")
	req.Header.Set("X-Version", "0.0.0+e2e")
	req.Header.Set("X-Timezone", "UTC")
	return t.base.RoundTrip(req)
}
