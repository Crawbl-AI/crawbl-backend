// Package webhook turns a UserSwarm CR plus Metacontroller's observed children
// into the complete desired runtime shape for that user.
//
// The package is intentionally arranged in the same order as the request flow:
//  1. surface.go   — process startup and HTTP route registration
//  2. intake.go    — request decoding / response encoding
//  3. flow.go      — reconciliation and finalization decisions
//  4. blueprint_*.go — pure child-resource builders
//  5. identity.go  — naming, defaults, labels, and spec accessors
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// Run starts the UserSwarm Metacontroller webhook.
//
// The package has exactly one public entrypoint on purpose. Everything else
// is internal package machinery so callers only need to say "run the webhook
// at this address".
func Run(ctx context.Context, cfg *ListenConfig) error {
	runtimeCfg := runtimeConfigFromEnv()

	mux := http.NewServeMux()
	mux.HandleFunc("/sync", syncSurface(runtimeCfg))
	mux.HandleFunc("/healthz", healthz())

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	slog.Info("starting userswarm webhook", "addr", cfg.Addr, "agent_runtime_image", runtimeCfg.AgentRuntimeImage)

	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)

		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		slog.Info("stopping userswarm webhook")
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("userswarm webhook shutdown failed", "error", err)
		}
	}()

	err := httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	<-shutdownDone
	return nil
}

// runtimeConfigFromEnv expands the deploy-time environment into the runtime
// configuration used during reconciliation. Every field has a well-defined
// env var; there is no config file to load because the agent runtime pod
// takes all of its settings from CLI flags + the envSecretRef Secret we
// project into it.
func runtimeConfigFromEnv() *runtimeConfig {
	return &runtimeConfig{
		AgentRuntimeImage:        envOrDefault("CRAWBL_AGENT_RUNTIME_IMAGE", "registry.digitalocean.com/crawbl/crawbl-agent-runtime:dev"),
		OrchestratorGRPCEndpoint: os.Getenv("CRAWBL_ORCHESTRATOR_ENDPOINT"),
		MCPEndpoint:              os.Getenv("CRAWBL_MCP_ENDPOINT"),
		PostgresHost:             os.Getenv("CRAWBL_DATABASE_HOST"),
		PostgresPort:             os.Getenv("CRAWBL_DATABASE_PORT"),
		PostgresUser:             os.Getenv("CRAWBL_DATABASE_USER"),
		PostgresName:             os.Getenv("CRAWBL_DATABASE_NAME"),
		PostgresSchema:           os.Getenv("CRAWBL_DATABASE_SCHEMA"),
		PostgresSSLMode:          os.Getenv("CRAWBL_DATABASE_SSLMODE"),
		RedisAddr:                os.Getenv("CRAWBL_REDIS_ADDR"),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// syncSurface returns the HTTP handler for the /sync Metacontroller webhook.
// It decodes the sync request, drives the reconciliation, and encodes the response.
func syncSurface(cfg *runtimeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req syncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Error("syncSurface: failed to decode request", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var swarm crawblv1alpha1.UserSwarm
		if err := json.Unmarshal(req.Parent, &swarm); err != nil {
			slog.Error("syncSurface: failed to unmarshal UserSwarm", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		resp := driveSync(&req, &swarm, cfg)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("syncSurface: failed to encode response", "error", err)
		}
	}
}

func healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}
}
