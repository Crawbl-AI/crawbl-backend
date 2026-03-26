// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	zeroclawImageConfigPath = "crawbl-infra/images/zeroclaw/image.env"
	zeroclawImageLockFile   = "crawbl-infra/images/zeroclaw/image.lock.env"
	zeroclawMetadataDir     = ".artifacts/zeroclaw"
	zeroclawDefaultPlatform = "linux/amd64"
	zeroclawDefaultTag      = "dev"
)

// zeroclawImageConfig holds the parsed image configuration.
type zeroclawImageConfig struct {
	GitSource       string
	GitRef          string
	DockerTarget    string
	Platform        string
	ImageRepository string
	ImageTag        string
}

// newBuildZeroclawCommand creates the build zeroclaw subcommand.
func newBuildZeroclawCommand() *cobra.Command {
	var tag string
	var platform string
	var push bool

	cmd := &cobra.Command{
		Use:   "zeroclaw",
		Short: "Build ZeroClaw runtime image",
		Long:  "Build the ZeroClaw runtime Docker image using docker buildx. Clones the upstream ZeroClaw repository at a pinned ref and builds with OCI labels.",
		Example: `  crawbl app build zeroclaw --tag v0.5.9
  crawbl app build zeroclaw --tag latest --platform linux/amd64,linux/arm64 --push
  crawbl app build zeroclaw --tag dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validation
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			// Get root directory
			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			// Load image configuration
			configPath := filepath.Join(rootDir, zeroclawImageConfigPath)
			config, err := loadZeroclawImageConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load image config: %w", err)
			}

			// Override tag and platform from flags
			config.ImageTag = tag
			config.Platform = platform

			imageRef := fmt.Sprintf("%s:%s", config.ImageRepository, config.ImageTag)
			metadataFile := filepath.Join(rootDir, zeroclawMetadataDir, fmt.Sprintf("%s.metadata.json", config.ImageTag))

			// Create artifacts directory
			if err := os.MkdirAll(filepath.Dir(metadataFile), 0755); err != nil {
				return fmt.Errorf("failed to create metadata directory: %w", err)
			}

			fmt.Printf("==> Building ZeroClaw %s\n", imageRef)

			// Clone upstream ZeroClaw repo at pinned ref into a temp directory
			workDir, err := cloneZeroClawRepo(config.GitSource, config.GitRef)
			if err != nil {
				return fmt.Errorf("failed to clone ZeroClaw repo: %w", err)
			}
			defer os.RemoveAll(workDir)

			// Get source SHA
			sourceSHA, err := getZeroClawSourceSHA(workDir)
			if err != nil {
				return fmt.Errorf("failed to get source SHA: %w", err)
			}

			// Build the docker command
			buildArgs := []string{
				"buildx", "build",
				"--platform", config.Platform,
				"--target", config.DockerTarget,
				"--label", fmt.Sprintf("org.opencontainers.image.source=%s", config.GitSource),
				"--label", fmt.Sprintf("org.opencontainers.image.revision=%s", sourceSHA),
				"--label", fmt.Sprintf("org.opencontainers.image.version=%s", config.GitRef),
				"--metadata-file", metadataFile,
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			buildArgs = append(buildArgs, workDir)

			// Run docker build
			execCmd := exec.Command("docker", buildArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr

			if err := execCmd.Run(); err != nil {
				return fmt.Errorf("build failed: %w", err)
			}

			// Update lock file if push was successful
			if push {
				if err := updateZeroclawLockFile(rootDir, config, sourceSHA, metadataFile); err != nil {
					return fmt.Errorf("failed to update lock file: %w", err)
				}
				fmt.Printf("✓ Pushed %s\n", imageRef)
			} else {
				fmt.Printf("✓ Built %s locally\n", imageRef)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", zeroclawDefaultTag, "Image tag")
	cmd.Flags().StringVar(&platform, "platform", zeroclawDefaultPlatform, "Build platform")
	cmd.Flags().BoolVar(&push, "push", true, "Push image to registry after build (default: true)")

	return cmd
}

// loadZeroclawImageConfig reads and parses the image configuration file.
func loadZeroclawImageConfig(configPath string) (*zeroclawImageConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &zeroclawImageConfig{}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "ZEROCLAW_GIT_SOURCE":
			config.GitSource = value
		case "ZEROCLAW_GIT_REF":
			config.GitRef = value
		case "ZEROCLAW_DOCKER_TARGET":
			config.DockerTarget = value
		case "ZEROCLAW_PLATFORM":
			config.Platform = value
		case "ZEROCLAW_IMAGE_REPOSITORY":
			config.ImageRepository = value
		case "ZEROCLAW_IMAGE_TAG":
			config.ImageTag = value
		}
	}

	// Validate required fields
	if config.GitSource == "" {
		return nil, fmt.Errorf("missing required config: ZEROCLAW_GIT_SOURCE")
	}
	if config.GitRef == "" {
		return nil, fmt.Errorf("missing required config: ZEROCLAW_GIT_REF")
	}
	if config.DockerTarget == "" {
		return nil, fmt.Errorf("missing required config: ZEROCLAW_DOCKER_TARGET")
	}
	if config.Platform == "" {
		return nil, fmt.Errorf("missing required config: ZEROCLAW_PLATFORM")
	}
	if config.ImageRepository == "" {
		return nil, fmt.Errorf("missing required config: ZEROCLAW_IMAGE_REPOSITORY")
	}

	return config, nil
}

// cloneZeroClawRepo clones the upstream ZeroClaw repository at the pinned ref.
func cloneZeroClawRepo(gitSource, gitRef string) (string, error) {
	workDir, err := os.MkdirTemp("", "zeroclaw-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	srcDir := filepath.Join(workDir, "src")

	fmt.Printf("==> Cloning %s at %s\n", gitSource, gitRef)

	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", gitRef, gitSource, srcDir)
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return srcDir, nil
}

// getZeroClawSourceSHA returns the git SHA of the cloned repository.
func getZeroClawSourceSHA(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get source SHA: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// updateZeroclawLockFile reads the metadata file and writes the lock file.
func updateZeroclawLockFile(rootDir string, config *zeroclawImageConfig, sourceSHA, metadataFile string) error {
	// Read metadata file to get image digest
	metadata, err := os.ReadFile(metadataFile)
	if err != nil {
		return fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadataMap map[string]interface{}
	if err := json.Unmarshal(metadata, &metadataMap); err != nil {
		return fmt.Errorf("failed to parse metadata file: %w", err)
	}

	imageDigest, ok := metadataMap["containerimage.digest"].(string)
	if !ok || imageDigest == "" {
		return fmt.Errorf("missing image digest in metadata file")
	}

	buildTimestamp := time.Now().UTC().Format(time.RFC3339)

	// Build lock file contents
	var sb strings.Builder
	sb.WriteString("# Last successfully pushed ZeroClaw image for Crawbl.\n")
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_SOURCE_SHA=%s\n", sourceSHA))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_SOURCE_REF=%s\n", config.GitRef))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_DOCKER_TARGET=%s\n", config.DockerTarget))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_PLATFORM=%s\n", config.Platform))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_IMAGE_REPOSITORY=%s\n", config.ImageRepository))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_IMAGE_TAG=%s\n", config.ImageTag))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_IMAGE_DIGEST=%s\n", imageDigest))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_IMAGE_REF=%s@%s\n", config.ImageRepository, imageDigest))
	sb.WriteString(fmt.Sprintf("ZEROCLAW_LOCKED_AT=%s\n", buildTimestamp))

	// Write to temp file first, then move atomically
	lockFilePath := filepath.Join(rootDir, zeroclawImageLockFile)
	tmpLockFile, err := os.CreateTemp(filepath.Dir(lockFilePath), "image.lock.env.*")
	if err != nil {
		return fmt.Errorf("failed to create temp lock file: %w", err)
	}

	if _, err := tmpLockFile.WriteString(sb.String()); err != nil {
		tmpLockFile.Close()
		os.Remove(tmpLockFile.Name())
		return fmt.Errorf("failed to write temp lock file: %w", err)
	}

	if err := tmpLockFile.Close(); err != nil {
		os.Remove(tmpLockFile.Name())
		return fmt.Errorf("failed to close temp lock file: %w", err)
	}

	if err := os.Rename(tmpLockFile.Name(), lockFilePath); err != nil {
		os.Remove(tmpLockFile.Name())
		return fmt.Errorf("failed to move lock file: %w", err)
	}

	fmt.Printf("✓ Image lock updated at %s\n", lockFilePath)

	return nil
}
