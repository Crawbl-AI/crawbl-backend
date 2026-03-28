// Package webhook implements the Metacontroller sync/finalize webhook for UserSwarm CRs.
//
// How it works:
//
//  1. Metacontroller watches all UserSwarm CRs in the cluster.
//  2. When a UserSwarm is created, updated, or deleted, Metacontroller POSTs to /sync.
//  3. This webhook receives the UserSwarm CR + its current children, and returns
//     the desired children (ServiceAccount, ConfigMap, PVC, Services, StatefulSet, backup Job).
//  4. Metacontroller diffs desired vs actual and creates/updates/deletes resources.
//  5. On deletion, the finalize hook returns empty children until all are gone.
//
// The webhook is stateless — it has no in-memory state, no informers, no caches.
// All decisions are based purely on the UserSwarm spec and the observed children
// that Metacontroller passes in each request.
//
// File layout:
//
//	server.go    — HTTP server startup and route registration
//	handler.go   — Request parsing, sync/finalize orchestration, status computation
//	children.go  — Pure functions that build each K8s child resource from the UserSwarm spec
//	naming.go    — Resource naming conventions, label builders, spec accessors
package webhook

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// ServerConfig holds everything needed to start the webhook HTTP server.
type ServerConfig struct {
	// Addr is the host:port to listen on (e.g. ":8080").
	Addr string
	// ZeroClawCfgPath is the path to the ZeroClaw operator config YAML
	// (usually mounted from a ConfigMap at /config/zeroclaw.yaml).
	ZeroClawCfgPath string
}

// ListenAndServe loads configuration and starts the HTTP server.
//
// Steps:
//  1. Load ZeroClaw config from disk (controls what goes into the bootstrap ConfigMap).
//  2. Build the webhook Config from environment variables (images, backup settings).
//  3. Register routes: /sync for Metacontroller, /healthz for pod probes.
//  4. Start listening — blocks until the server shuts down or errors.
func ListenAndServe(sc *ServerConfig) error {
	// Step 1: Load ZeroClaw config that controls runtime bootstrap files.
	zcConfig, err := zeroclaw.LoadConfig(sc.ZeroClawCfgPath)
	if err != nil {
		return fmt.Errorf("load zeroclaw config: %w", err)
	}

	// Step 2: Build webhook config from environment.
	cfg := ConfigFromEnv()
	cfg.ZeroClawConfig = zcConfig

	// Step 3: Register routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/sync", NewHandler(cfg))
	mux.HandleFunc("/healthz", Healthz())

	// Step 4: Start server.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	slog.Info("starting metacontroller webhook", "addr", sc.Addr)

	return http.ListenAndServe(sc.Addr, mux)
}
