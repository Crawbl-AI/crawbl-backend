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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// Run starts the UserSwarm Metacontroller webhook.
//
// The package has exactly one public entrypoint on purpose. Everything else is
// internal package machinery so callers only need to say "run the webhook at
// this address using this ZeroClaw config file".
func Run(ctx context.Context, cfg *ListenConfig) error {
	zcConfig, err := zeroclaw.LoadConfig(cfg.ZeroClawCfgPath)
	if err != nil {
		return fmt.Errorf("load zeroclaw config: %w", err)
	}

	runtimeCfg := runtimeConfigFromEnv()
	runtimeCfg.ZeroClawConfig = zcConfig

	mux := http.NewServeMux()
	mux.HandleFunc("/sync", syncSurface(runtimeCfg))
	mux.HandleFunc("/healthz", healthz())

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	slog.Info("starting userswarm webhook", "addr", cfg.Addr)

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

	err = httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	<-shutdownDone
	return nil
}

// runtimeConfigFromEnv expands the deploy-time environment into the runtime
// configuration used during reconciliation.
func runtimeConfigFromEnv() *runtimeConfig {
	return &runtimeConfig{
		BootstrapImage:   envOrDefault("USERSWARM_BOOTSTRAP_IMAGE", "registry.digitalocean.com/crawbl/crawbl-platform:dev"),
		MCPEndpoint:      os.Getenv("CRAWBL_MCP_ENDPOINT"),
		MCPSigningKey:    os.Getenv("CRAWBL_MCP_SIGNING_KEY"),
		BackupBucket:     os.Getenv("USERSWARM_BACKUP_BUCKET"),
		BackupRegion:     os.Getenv("USERSWARM_BACKUP_REGION"),
		BackupSecretName: os.Getenv("USERSWARM_BACKUP_SECRET_NAME"),
	}
}

// TODO: THIS SHIT FUNC IS EVERYWHERE, DEDUP IT
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}
}
