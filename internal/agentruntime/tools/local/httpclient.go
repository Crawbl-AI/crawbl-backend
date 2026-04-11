package local

import (
	"net/http"
	"time"
)

const (
	// toolHTTPRequestTimeout bounds the total time a single outbound tool
	// HTTP call is allowed to run end-to-end, including DNS, connect, TLS,
	// request write, and response read.
	toolHTTPRequestTimeout = 30 * time.Second
	// toolHTTPIdleConnTimeout limits how long an idle keep-alive connection
	// may remain in the shared transport's pool before being closed.
	toolHTTPIdleConnTimeout = 90 * time.Second
	// toolHTTPTLSHandshakeTimeout bounds the TLS handshake phase alone.
	toolHTTPTLSHandshakeTimeout = 10 * time.Second
	// toolHTTPMaxIdleConns caps pooled idle keep-alive connections.
	toolHTTPMaxIdleConns = 10
)

// toolHTTPClient is the shared HTTP client used by every local tool that
// makes outbound requests (web_fetch, web_search, etc.). Defining it once
// with explicit timeouts and transport tuning avoids repeating those
// settings per tool and prevents accidental use of http.DefaultClient,
// which has no timeout.
var toolHTTPClient = &http.Client{
	Timeout: toolHTTPRequestTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        toolHTTPMaxIdleConns,
		IdleConnTimeout:     toolHTTPIdleConnTimeout,
		TLSHandshakeTimeout: toolHTTPTLSHandshakeTimeout,
	},
}
