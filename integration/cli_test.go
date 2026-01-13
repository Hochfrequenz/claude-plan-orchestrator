//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binaryPath returns the path to the built CLI binary
func binaryPath(t *testing.T) string {
	t.Helper()
	// Look for the binary in common locations
	paths := []string{
		"../claude-orch",
		"./claude-orch",
		filepath.Join(os.Getenv("GOPATH"), "bin", "claude-orch"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}

	// Try to build it
	t.Log("Binary not found, building...")
	cmd := exec.Command("go", "build", "-o", "../claude-orch", "../cmd/claude-orch")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, out)
	}

	abs, _ := filepath.Abs("../claude-orch")
	return abs
}

// createTestConfig creates a temporary config file for testing
func createTestConfig(t *testing.T, projectRoot, dbPath string) string {
	t.Helper()
	configPath := TempConfigPath(t)

	config := `[general]
project_root = "` + projectRoot + `"
worktree_dir = "/tmp/worktrees"
max_parallel_agents = 3
database_path = "` + dbPath + `"

[claude]
model = "claude-opus-4-5-20251101"
max_tokens = 16000

[notifications]
desktop = false

[web]
port = 8080
host = "127.0.0.1"
`

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	return configPath
}

// TestCLI_Sync tests the sync command
func TestCLI_Sync(t *testing.T) {
	binary := binaryPath(t)
	plansDir := CopyFixturesToTemp(t)
	projectRoot := filepath.Dir(plansDir) // Parent of plans dir
	dbPath := TempDBPath(t)

	// Create proper directory structure (plans should be under docs/plans)
	docsPlansDir := filepath.Join(projectRoot, "docs", "plans")
	if err := os.MkdirAll(filepath.Dir(docsPlansDir), 0755); err != nil {
		t.Fatalf("Failed to create docs dir: %v", err)
	}
	if err := os.Rename(plansDir, docsPlansDir); err != nil {
		t.Fatalf("Failed to move plans dir: %v", err)
	}

	configPath := createTestConfig(t, projectRoot, dbPath)

	// Run sync command
	cmd := exec.Command(binary, "sync", "--config", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sync command failed: %v\n%s", err, out)
	}

	output := string(out)

	// Verify output indicates tasks were synced
	if !strings.Contains(output, "Synced") {
		t.Errorf("Expected 'Synced' in output, got: %s", output)
	}

	if !strings.Contains(output, "5 tasks") {
		t.Errorf("Expected '5 tasks' in output, got: %s", output)
	}
}

// TestCLI_Status tests the status command
func TestCLI_Status(t *testing.T) {
	binary := binaryPath(t)
	plansDir := CopyFixturesToTemp(t)
	projectRoot := filepath.Dir(plansDir)
	dbPath := TempDBPath(t)

	// Create proper directory structure
	docsPlansDir := filepath.Join(projectRoot, "docs", "plans")
	os.MkdirAll(filepath.Dir(docsPlansDir), 0755)
	os.Rename(plansDir, docsPlansDir)

	configPath := createTestConfig(t, projectRoot, dbPath)

	// First sync to populate database
	syncCmd := exec.Command(binary, "sync", "--config", configPath)
	if out, err := syncCmd.CombinedOutput(); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out)
	}

	// Run status command
	cmd := exec.Command(binary, "status", "--config", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status command failed: %v\n%s", err, out)
	}

	output := string(out)

	// Verify output contains expected information
	// We have 5 tasks: 2 complete, 1 in progress, 2 not started
	if !strings.Contains(output, "5 total") {
		t.Errorf("Expected '5 total' in output, got: %s", output)
	}

	if !strings.Contains(output, "2 complete") {
		t.Errorf("Expected '2 complete' in output, got: %s", output)
	}
}

// TestCLI_List tests the list command
func TestCLI_List(t *testing.T) {
	binary := binaryPath(t)
	plansDir := CopyFixturesToTemp(t)
	projectRoot := filepath.Dir(plansDir)
	dbPath := TempDBPath(t)

	// Create proper directory structure
	docsPlansDir := filepath.Join(projectRoot, "docs", "plans")
	os.MkdirAll(filepath.Dir(docsPlansDir), 0755)
	os.Rename(plansDir, docsPlansDir)

	configPath := createTestConfig(t, projectRoot, dbPath)

	// First sync
	syncCmd := exec.Command(binary, "sync", "--config", configPath)
	if out, err := syncCmd.CombinedOutput(); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out)
	}

	// Test list all
	cmd := exec.Command(binary, "list", "--config", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list command failed: %v\n%s", err, out)
	}

	output := string(out)

	// Verify output contains expected tasks
	expectedTasks := []string{"test/E00", "test/E01", "test/E02", "billing/E00", "billing/E01"}
	for _, taskID := range expectedTasks {
		if !strings.Contains(output, taskID) {
			t.Errorf("Expected task %s in output, got: %s", taskID, output)
		}
	}

	// Verify header
	if !strings.Contains(output, "ID") || !strings.Contains(output, "TITLE") {
		t.Errorf("Expected table header in output, got: %s", output)
	}
}

// TestCLI_ListWithModuleFilter tests the list command with module filter
func TestCLI_ListWithModuleFilter(t *testing.T) {
	binary := binaryPath(t)
	plansDir := CopyFixturesToTemp(t)
	projectRoot := filepath.Dir(plansDir)
	dbPath := TempDBPath(t)

	// Create proper directory structure
	docsPlansDir := filepath.Join(projectRoot, "docs", "plans")
	os.MkdirAll(filepath.Dir(docsPlansDir), 0755)
	os.Rename(plansDir, docsPlansDir)

	configPath := createTestConfig(t, projectRoot, dbPath)

	// First sync
	syncCmd := exec.Command(binary, "sync", "--config", configPath)
	if out, err := syncCmd.CombinedOutput(); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out)
	}

	// Test list with module filter
	cmd := exec.Command(binary, "list", "--module", "billing", "--config", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list command failed: %v\n%s", err, out)
	}

	output := string(out)

	// Should contain billing tasks
	if !strings.Contains(output, "billing/E00") {
		t.Errorf("Expected billing/E00 in output, got: %s", output)
	}

	// Should NOT contain test tasks
	if strings.Contains(output, "test/E00") {
		t.Errorf("Did not expect test/E00 in output, got: %s", output)
	}
}

// TestCLI_ListWithStatusFilter tests the list command with status filter
func TestCLI_ListWithStatusFilter(t *testing.T) {
	binary := binaryPath(t)
	plansDir := CopyFixturesToTemp(t)
	projectRoot := filepath.Dir(plansDir)
	dbPath := TempDBPath(t)

	// Create proper directory structure
	docsPlansDir := filepath.Join(projectRoot, "docs", "plans")
	os.MkdirAll(filepath.Dir(docsPlansDir), 0755)
	os.Rename(plansDir, docsPlansDir)

	configPath := createTestConfig(t, projectRoot, dbPath)

	// First sync
	syncCmd := exec.Command(binary, "sync", "--config", configPath)
	if out, err := syncCmd.CombinedOutput(); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out)
	}

	// Test list with status filter (complete tasks)
	cmd := exec.Command(binary, "list", "--status", "complete", "--config", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list command failed: %v\n%s", err, out)
	}

	output := string(out)

	// Should contain complete tasks (test/E00 and billing/E00)
	if !strings.Contains(output, "test/E00") {
		t.Errorf("Expected test/E00 (complete) in output, got: %s", output)
	}

	if !strings.Contains(output, "billing/E00") {
		t.Errorf("Expected billing/E00 (complete) in output, got: %s", output)
	}

	// Should NOT contain not_started tasks
	if strings.Contains(output, "test/E02") {
		t.Errorf("Did not expect test/E02 (not_started) in output, got: %s", output)
	}
}

// TestCLI_InvalidCommand tests error handling for invalid commands
func TestCLI_InvalidCommand(t *testing.T) {
	binary := binaryPath(t)

	cmd := exec.Command(binary, "invalidcommand")
	out, err := cmd.CombinedOutput()

	// Should return error
	if err == nil {
		t.Error("Expected error for invalid command")
	}

	output := string(out)

	// Should suggest valid commands or show help
	if !strings.Contains(output, "unknown command") && !strings.Contains(output, "Usage") {
		t.Errorf("Expected error message or usage info, got: %s", output)
	}
}

// TestCLI_SyncMissingProjectRoot tests error when project_root is not configured
func TestCLI_SyncMissingProjectRoot(t *testing.T) {
	binary := binaryPath(t)
	dbPath := TempDBPath(t)
	configPath := TempConfigPath(t)

	// Create config without project_root
	config := `[general]
project_root = ""
database_path = "` + dbPath + `"
`
	os.MkdirAll(filepath.Dir(configPath), 0755)
	os.WriteFile(configPath, []byte(config), 0644)

	cmd := exec.Command(binary, "sync", "--config", configPath)
	out, err := cmd.CombinedOutput()

	// Should return error
	if err == nil {
		t.Error("Expected error when project_root is not configured")
	}

	output := string(out)
	if !strings.Contains(output, "project_root") {
		t.Errorf("Expected error about project_root, got: %s", output)
	}
}
