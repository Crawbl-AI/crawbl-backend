package zeroclaw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func minimalSwarm(userID string, port int32) *crawblv1alpha1.UserSwarm {
	return &crawblv1alpha1.UserSwarm{
		Spec: crawblv1alpha1.UserSwarmSpec{
			UserID: userID,
			Runtime: crawblv1alpha1.UserSwarmRuntimeSpec{
				Port: port,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Test 1: TestBuildConfigTOML_EmitsAgentSections
// ---------------------------------------------------------------------------

func TestBuildConfigTOML_EmitsAgentSections(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents = map[string]ZeroClawAgent{
		"wally": {
			SystemPrompt: "test prompt",
			Agentic:      true,
			AllowedTools: []string{"web_search"},
		},
	}

	sw := minimalSwarm("test-user", 42617)

	out, err := BuildConfigTOML(sw, cfg)
	if err != nil {
		t.Fatalf("BuildConfigTOML returned error: %v", err)
	}

	checks := []string{
		"[agents.wally]",
		`system_prompt = "test prompt"`,
		"agentic = true",
		"allowed_tools",
		"provider =",
		`skills_directory = "agents/wally"`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("TOML output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestBuildConfigTOML_AgentInheritsProviderModel
// ---------------------------------------------------------------------------

func TestBuildConfigTOML_AgentInheritsProviderModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents = map[string]ZeroClawAgent{
		"wally": {SystemPrompt: "hello"},
	}

	// Cluster-level defaults (no per-user override yet).
	// BuildConfigTOML hard-codes "openai" / "gpt-5-mini" as the base defaults.
	sw := minimalSwarm("test-user", 42617)

	out, err := BuildConfigTOML(sw, cfg)
	if err != nil {
		t.Fatalf("BuildConfigTOML returned error: %v", err)
	}

	if !strings.Contains(out, `provider = "openai"`) {
		t.Errorf("expected agent section to contain provider = \"openai\"\nfull output:\n%s", out)
	}
	if !strings.Contains(out, `model = "gpt-5-mini"`) {
		t.Errorf("expected agent section to contain model = \"gpt-5-mini\"\nfull output:\n%s", out)
	}

	// Now apply per-user overrides via the UserSwarm spec.
	sw.Spec.Config.DefaultProvider = "anthropic"
	sw.Spec.Config.DefaultModel = "claude-sonnet-4-6"

	out2, err := BuildConfigTOML(sw, cfg)
	if err != nil {
		t.Fatalf("BuildConfigTOML (with overrides) returned error: %v", err)
	}

	if !strings.Contains(out2, `provider = "anthropic"`) {
		t.Errorf("expected overridden agent section to contain provider = \"anthropic\"\nfull output:\n%s", out2)
	}
	if !strings.Contains(out2, `model = "claude-sonnet-4-6"`) {
		t.Errorf("expected overridden agent section to contain model = \"claude-sonnet-4-6\"\nfull output:\n%s", out2)
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestBuildConfigTOML_NoAgents
// ---------------------------------------------------------------------------

func TestBuildConfigTOML_NoAgents(t *testing.T) {
	cfg := DefaultConfig()
	// Agents is nil by default — no agents section expected.

	sw := minimalSwarm("test-user", 42617)

	out, err := BuildConfigTOML(sw, cfg)
	if err != nil {
		t.Fatalf("BuildConfigTOML returned error: %v", err)
	}

	if strings.Contains(out, "[agents") {
		t.Errorf("expected no [agents] section when Agents is empty, got:\n%s", out)
	}

	// Also verify empty map produces no agents section.
	cfg.Agents = map[string]ZeroClawAgent{}
	out2, err := BuildConfigTOML(sw, cfg)
	if err != nil {
		t.Fatalf("BuildConfigTOML (empty map) returned error: %v", err)
	}
	if strings.Contains(out2, "[agents") {
		t.Errorf("expected no [agents] section when Agents is empty map, got:\n%s", out2)
	}
}

// ---------------------------------------------------------------------------
// Test 4: TestMergeManaged_FullSectionReplacement
// ---------------------------------------------------------------------------

func TestMergeManaged_FullSectionReplacement(t *testing.T) {
	live := map[string]any{
		"agents": map[string]any{
			"old_agent": map[string]any{"model": "old"},
		},
	}
	bootstrap := map[string]any{
		"agents": map[string]any{
			"wally": map[string]any{"model": "new"},
		},
	}

	mergeManaged(live, bootstrap)

	agents, ok := live["agents"].(map[string]any)
	if !ok {
		t.Fatalf("live[\"agents\"] is not map[string]any after merge")
	}
	if _, exists := agents["wally"]; !exists {
		t.Errorf("expected \"wally\" in agents after merge, got: %v", agents)
	}
	if _, exists := agents["old_agent"]; exists {
		t.Errorf("expected \"old_agent\" to be removed after full-section replacement, got: %v", agents)
	}
}

// ---------------------------------------------------------------------------
// Test 5: TestMergeManaged_AgentsRemovedWhenBootstrapEmpty
// ---------------------------------------------------------------------------

func TestMergeManaged_AgentsRemovedWhenBootstrapEmpty(t *testing.T) {
	live := map[string]any{
		"agents": map[string]any{
			"wally": map[string]any{"model": "gpt-5-mini"},
		},
	}
	// Bootstrap has no "agents" key.
	bootstrap := map[string]any{
		"default_provider": "openai",
	}

	mergeManaged(live, bootstrap)

	if _, exists := live["agents"]; exists {
		t.Errorf("expected \"agents\" key to be deleted when bootstrap has no agents section, live=%v", live)
	}
}

// ---------------------------------------------------------------------------
// Test 6: TestEnsureAgentSkills_WritesFiles
// ---------------------------------------------------------------------------

func TestEnsureAgentSkills_WritesFiles(t *testing.T) {
	tmpDir := t.TempDir()

	agentFiles := map[string]map[string]string{
		"wally": {
			"personality.md": "test content",
		},
	}

	if err := EnsureAgentSkills(tmpDir, agentFiles); err != nil {
		t.Fatalf("EnsureAgentSkills returned error: %v", err)
	}

	wantPath := filepath.Join(tmpDir, "agents", "wally", "personality.md")
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected file at %s to exist, got error: %v", wantPath, err)
	}
	if string(data) != "test content" {
		t.Errorf("file content = %q, want %q", string(data), "test content")
	}
}

// ---------------------------------------------------------------------------
// Test 7: TestBuildAgentSkillFiles_Wally
// ---------------------------------------------------------------------------

func TestBuildAgentSkillFiles_Wally(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents = map[string]ZeroClawAgent{
		"wally": {},
	}

	result := BuildAgentSkillFiles(cfg)

	wallyFiles, ok := result["wally"]
	if !ok {
		t.Fatal("expected \"wally\" key in BuildAgentSkillFiles result")
	}

	requiredFiles := []string{"personality.md", "guidelines.md", "domain.md", "tools.md"}
	for _, f := range requiredFiles {
		if _, exists := wallyFiles[f]; !exists {
			t.Errorf("expected wally to have %q file, files present: %v", f, mapKeys(wallyFiles))
		}
	}

	personality := wallyFiles["personality.md"]
	if !strings.Contains(personality, "Wally") {
		t.Errorf("personality.md should contain \"Wally\", got:\n%s", personality)
	}
}

// ---------------------------------------------------------------------------
// Test 8: TestBuildSoulMarkdown_ContainsManagerIdentity
// ---------------------------------------------------------------------------

func TestBuildSoulMarkdown_ContainsManagerIdentity(t *testing.T) {
	sw := minimalSwarm("test-user", 42617)

	out := BuildSoulMarkdown(sw)

	checks := []string{"Manager", "delegate", "test-user"}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("BuildSoulMarkdown output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
