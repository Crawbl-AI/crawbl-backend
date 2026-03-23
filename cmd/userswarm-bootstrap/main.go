package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

func main() {
	var bootstrapConfigPath string
	var liveConfigPath string
	var workspacePath string

	flag.StringVar(&bootstrapConfigPath, "bootstrap-config", "/bootstrap/config.toml", "Path to the rendered bootstrap config.toml.")
	flag.StringVar(&liveConfigPath, "live-config", "/zeroclaw-data/.zeroclaw/config.toml", "Path to the live PVC-backed ZeroClaw config.toml.")
	flag.StringVar(&workspacePath, "workspace", "/zeroclaw-data/workspace", "Path to the PVC-backed ZeroClaw workspace.")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(liveConfigPath), 0o700); err != nil {
		log.Fatalf("create config dir: %v", err)
	}
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		log.Fatalf("create workspace dir: %v", err)
	}
	if err := zeroclaw.EnsureManagedConfig(bootstrapConfigPath, liveConfigPath); err != nil {
		log.Fatalf("bootstrap userswarm config: %v", err)
	}
}
