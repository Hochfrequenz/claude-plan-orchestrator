package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestBuildPrompt(t *testing.T) {
	task := &domain.Task{
		ID:          domain.TaskID{Module: "technical", EpicNum: 5},
		Title:       "Validators",
		Description: "Implement input validation",
	}

	epicContent := "# Epic 05: Validators\n\nImplement validators."
	completedDeps := []string{"technical/E04"}

	prompt := BuildPrompt(task, epicContent, "", completedDeps)

	if !containsString(prompt, "Validators") {
		t.Error("Prompt should contain task title")
	}
	if !containsString(prompt, "technical/E04") {
		t.Error("Prompt should contain completed dependencies")
	}
}

func TestAgent_StatusTransitions(t *testing.T) {
	agent := &Agent{
		TaskID: domain.TaskID{Module: "tech", EpicNum: 0},
		Status: AgentQueued,
	}

	if agent.Status != AgentQueued {
		t.Errorf("Initial status = %s, want queued", agent.Status)
	}

	agent.Status = AgentRunning
	agent.StartedAt = timePtr(time.Now())

	if agent.Status != AgentRunning {
		t.Errorf("Status = %s, want running", agent.Status)
	}
}

func TestAgentManager_MaxConcurrency(t *testing.T) {
	mgr := NewAgentManager(2)

	// Add 3 agents
	for i := 0; i < 3; i++ {
		mgr.Add(&Agent{
			TaskID: domain.TaskID{Module: "tech", EpicNum: i},
			Status: AgentQueued,
		})
	}

	// Should only allow 2 to run
	running := mgr.RunningCount()
	queued := mgr.QueuedCount()

	if running > 2 {
		t.Errorf("Running = %d, should not exceed max 2", running)
	}
	if queued+running != 3 {
		t.Errorf("Total agents = %d, want 3", queued+running)
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// Integration test - requires claude CLI
func TestAgent_Run_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would test actual Claude Code invocation
	// Skip for unit tests
	t.Skip("Integration test requires Claude Code CLI")
}

var _ = context.Background // silence unused import

func TestAgent_generateMCPConfig(t *testing.T) {
	t.Run("loads project .mcp.json", func(t *testing.T) {
		dir := t.TempDir()

		// Create a .mcp.json in the temp dir
		mcpConfig := map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"test-runner": map[string]interface{}{
					"command": "/usr/local/bin/test-runner",
					"args":    []string{"--verbose"},
				},
				"code-analyzer": map[string]interface{}{
					"command": "npx",
					"args":    []string{"code-analyzer"},
				},
			},
		}
		configData, _ := json.Marshal(mcpConfig)
		os.WriteFile(filepath.Join(dir, ".mcp.json"), configData, 0644)

		agent := &Agent{
			WorktreePath: dir,
		}

		result := agent.generateMCPConfig()
		if result == "" {
			t.Fatal("Expected non-empty MCP config")
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			t.Fatalf("Failed to parse config JSON: %v", err)
		}

		servers, ok := parsed["mcpServers"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected mcpServers in config")
		}

		if _, ok := servers["test-runner"]; !ok {
			t.Error("Expected test-runner in config")
		}
		if _, ok := servers["code-analyzer"]; !ok {
			t.Error("Expected code-analyzer in config")
		}
	})

	t.Run("returns empty when no mcp.json and no build-mcp", func(t *testing.T) {
		dir := t.TempDir()

		agent := &Agent{
			WorktreePath: dir,
		}

		// Note: This test assumes build-mcp is not in PATH during test
		// If build-mcp is in PATH, the test may pass with build-pool only
		result := agent.generateMCPConfig()

		// Either empty or contains only build-pool
		if result != "" {
			var parsed map[string]interface{}
			json.Unmarshal([]byte(result), &parsed)
			servers := parsed["mcpServers"].(map[string]interface{})
			// Should only have build-pool if anything
			for name := range servers {
				if name != "build-pool" {
					t.Errorf("Unexpected server %s in config", name)
				}
			}
		}
	})
}
