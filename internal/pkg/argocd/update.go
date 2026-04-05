// Package argocd provides helpers for updating image tags in crawbl-argocd-apps.
package argocd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

const (
	// RegistryBase is the DigitalOcean container registry base URL.
	RegistryBase = "registry.digitalocean.com/crawbl"
)

// Update holds state for updating image tags in crawbl-argocd-apps.
type Update struct {
	RepoPath string
	Tag      string
}

// RunYQ executes yq -i with the given expression against a file path (relative to RepoPath).
func (u *Update) RunYQ(expr, relPath string) error {
	absPath := filepath.Join(u.RepoPath, relPath)
	cmd := exec.Command("yq", "-i", expr, absPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yq update failed for %s: %w", relPath, err)
	}
	return nil
}

// UpdateOrchestrator updates .image.tag in components/orchestrator/chart/values.yaml.
func (u *Update) UpdateOrchestrator() error {
	out.Step(style.Deploy, "Updating orchestrator image tag to %s", u.Tag)
	return u.RunYQ(fmt.Sprintf(`.image.tag = "%s"`, u.Tag), "components/orchestrator/chart/values.yaml")
}

// UpdatePlatform replaces crawbl-platform image references in the webhook and reaper manifests.
func (u *Update) UpdatePlatform() error {
	out.Step(style.Deploy, "Updating crawbl-platform image tag to %s", u.Tag)

	imageBase := RegistryBase + "/crawbl-platform:"
	files := []string{
		filepath.Join(u.RepoPath, "components/metacontroller/resources/userswarm-webhook.yaml"),
		filepath.Join(u.RepoPath, "components/metacontroller/resources/e2e-reaper-cronjob.yaml"),
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		content := string(data)
		updated := ReplaceImageTag(content, imageBase, u.Tag)
		if err := os.WriteFile(f, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f, err)
		}
	}
	return nil
}

// UpdateAuthFilter replaces the auth-filter image reference in the envoy extension policy.
func (u *Update) UpdateAuthFilter() error {
	out.Step(style.Deploy, "Updating envoy-auth-filter image tag to %s", u.Tag)

	imageBase := RegistryBase + "/envoy-auth-filter:"
	f := filepath.Join(u.RepoPath, "components/envoy-gateway/resources/envoy-extension-policy.yaml")

	data, err := os.ReadFile(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", f, err)
	}
	updated := ReplaceImageTag(string(data), imageBase, u.Tag)
	return os.WriteFile(f, []byte(updated), 0o644)
}

// UpdateAgentRuntime updates the crawbl-agent-runtime image tag in the
// orchestrator chart values and the userswarm webhook manifest. This is
// Phase 2's counterpart to UpdateZeroClaw — once the webhook stops
// shelling out to ZeroClaw and starts scheduling the new agent-runtime
// image, this function owns the per-deploy tag bump.
//
// The orchestrator chart key lives at .config.runtime.agentRuntimeImage
// (added in the Phase 2 argocd-apps change). The webhook env var lives
// at CRAWBL_AGENT_RUNTIME_IMAGE inside userswarm-webhook.yaml (also
// added in Phase 2). Until those CRs land, this function is a no-op
// that logs the intended tag and returns nil so the deploy flow
// exercises end-to-end without blocking on the argocd-apps PR.
func (u *Update) UpdateAgentRuntime() error {
	out.Step(style.Deploy, "Updating crawbl-agent-runtime image tag to %s", u.Tag)

	agentRuntimeImage := fmt.Sprintf("%s/crawbl-agent-runtime:%s", RegistryBase, u.Tag)

	// Best-effort: try to update both locations. If the keys don't
	// exist yet (Phase 2 argocd-apps change not landed), yq will fail
	// with a non-zero exit and ReplaceImageTag returns the original
	// content unchanged — both are acceptable during the transition.
	orchestratorPath := "components/orchestrator/chart/values.yaml"
	if err := u.RunYQ(
		fmt.Sprintf(`.config.runtime.agentRuntimeImage = "%s"`, agentRuntimeImage),
		orchestratorPath,
	); err != nil {
		// Non-fatal during Phase 2 transition — the key may not exist
		// in the chart yet. Log + continue so the webhook update still
		// runs if the chart update fails.
		out.Step(style.Tip, "orchestrator chart agentRuntimeImage key not yet present (Phase 2 argocd-apps PR pending): %v", err)
	}

	webhookPath := filepath.Join(u.RepoPath, "components/metacontroller/resources/userswarm-webhook.yaml")
	data, err := os.ReadFile(webhookPath)
	if err != nil {
		return fmt.Errorf("read userswarm-webhook.yaml: %w", err)
	}
	imageBase := RegistryBase + "/crawbl-agent-runtime:"
	updated := ReplaceImageTag(string(data), imageBase, u.Tag)
	return os.WriteFile(webhookPath, []byte(updated), 0o644)
}

// UpdateZeroClaw updates the zeroclaw image reference in orchestrator values and the webhook manifest.
func (u *Update) UpdateZeroClaw() error {
	out.Step(style.Deploy, "Updating zeroclaw image ref to %s", u.Tag)

	zeroClawImage := fmt.Sprintf("%s/zeroclaw:%s", RegistryBase, u.Tag)

	// 1. Update .config.runtime.image in orchestrator chart values.
	if err := u.RunYQ(
		fmt.Sprintf(`.config.runtime.image = "%s"`, zeroClawImage),
		"components/orchestrator/chart/values.yaml",
	); err != nil {
		return err
	}

	// 2. Replace ZEROCLAW_DEFAULT_IMAGE value in userswarm-webhook.yaml.
	webhookPath := filepath.Join(u.RepoPath, "components/metacontroller/resources/userswarm-webhook.yaml")
	data, err := os.ReadFile(webhookPath)
	if err != nil {
		return fmt.Errorf("read userswarm-webhook.yaml: %w", err)
	}
	imageBase := RegistryBase + "/zeroclaw:"
	updated := ReplaceImageTag(string(data), imageBase, u.Tag)
	return os.WriteFile(webhookPath, []byte(updated), 0o644)
}

// PullLatest pulls the latest changes from remote before making modifications.
func (u *Update) PullLatest() error {
	out.Step(style.Deploy, "Pulling latest changes from argocd repo")
	cmd := exec.Command("git", "-C", u.RepoPath, "pull", "--rebase")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull --rebase failed: %w", err)
	}
	return nil
}

// CommitAndPush stages all changes, commits with a deploy message, and pushes.
// It is a no-op if there are no staged changes.
func (u *Update) CommitAndPush(component string) error {
	// Stage all changes.
	addCmd := exec.Command("git", "-C", u.RepoPath, "add", "-A")
	addCmd.Stdout = os.Stdout
	addCmd.Stderr = os.Stderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	// Check whether there is anything to commit.
	diffCmd := exec.Command("git", "-C", u.RepoPath, "diff", "--cached", "--quiet")
	if err := diffCmd.Run(); err == nil {
		out.Step(style.Deploy, "No changes to commit for %s", component)
		return nil
	}

	// Commit.
	message := fmt.Sprintf("deploy: update %s to %s", component, u.Tag)
	out.Step(style.Deploy, "Committing: %s", message)
	commitCmd := exec.Command("git", "-C", u.RepoPath, "commit", "-m", message)
	commitCmd.Stdout = os.Stdout
	commitCmd.Stderr = os.Stderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	// Pull with rebase to incorporate any remote changes before pushing.
	rebaseCmd := exec.Command("git", "-C", u.RepoPath, "pull", "--rebase")
	rebaseCmd.Stdout = os.Stdout
	rebaseCmd.Stderr = os.Stderr
	if err := rebaseCmd.Run(); err != nil {
		return fmt.Errorf("git pull --rebase before push failed: %w", err)
	}

	// Push.
	out.Step(style.Deploy, "Pushing changes")
	pushCmd := exec.Command("git", "-C", u.RepoPath, "push")
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}

	out.Success("Pushed %s update to argocd repo", component)
	return nil
}
