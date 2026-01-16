package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	// Create initial commit
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# Test"), 0644)

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

func TestWorktreeManager_Create(t *testing.T) {
	repoDir := setupGitRepo(t)
	worktreeDir := t.TempDir()

	mgr := NewWorktreeManager(repoDir, worktreeDir)

	taskID := domain.TaskID{Module: "technical", EpicNum: 5}
	wtPath, err := mgr.Create(taskID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify worktree was created
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("Worktree directory not created")
	}

	// Verify branch was created
	cmd := exec.Command("git", "branch", "--list", "feat/technical-E05")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	if len(out) == 0 {
		t.Error("Branch feat/technical-E05 not created")
	}
}

func TestWorktreeManager_Remove(t *testing.T) {
	repoDir := setupGitRepo(t)
	worktreeDir := t.TempDir()

	mgr := NewWorktreeManager(repoDir, worktreeDir)

	taskID := domain.TaskID{Module: "technical", EpicNum: 5}
	wtPath, _ := mgr.Create(taskID)

	if err := mgr.Remove(wtPath); err != nil {
		t.Fatal(err)
	}

	// Verify worktree was removed
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("Worktree directory still exists")
	}
}

func TestWorktreeManager_BranchName(t *testing.T) {
	tests := []struct {
		taskID domain.TaskID
		want   string
	}{
		// Standard epics without prefix
		{domain.TaskID{Module: "technical", EpicNum: 5}, "feat/technical-E05"},
		{domain.TaskID{Module: "billing", EpicNum: 0}, "feat/billing-E00"},
		// Epics with subsystem prefix
		{domain.TaskID{Module: "cli-tui-impl", Prefix: "CLI", EpicNum: 2}, "feat/cli-tui-impl-CLI02"},
		{domain.TaskID{Module: "cli-tui-impl", Prefix: "TUI", EpicNum: 7}, "feat/cli-tui-impl-TUI07"},
		{domain.TaskID{Module: "api-module", Prefix: "API", EpicNum: 0}, "feat/api-module-API00"},
	}

	for _, tt := range tests {
		got := BranchName(tt.taskID)
		if got != tt.want {
			t.Errorf("BranchName(%v) = %q, want %q", tt.taskID, got, tt.want)
		}
	}
}
