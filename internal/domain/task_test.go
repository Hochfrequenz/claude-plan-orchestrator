package domain

import (
	"testing"
)

func TestTaskID_Parse(t *testing.T) {
	tests := []struct {
		input      string
		wantModule string
		wantPrefix string
		wantEpic   int
		wantErr    bool
	}{
		// Standard format: module/E##
		{"technical/E05", "technical", "", 5, false},
		{"billing/E00", "billing", "", 0, false},
		{"pricing/E123", "pricing", "", 123, false},
		// Prefix format: module/PREFIX##
		{"cli-impl/CLI02", "cli-impl", "CLI", 2, false},
		{"cli-impl/TUI05", "cli-impl", "TUI", 5, false},
		{"api-module/API00", "api-module", "API", 0, false},
		// Invalid formats
		{"invalid", "", "", 0, true},
		{"module/invalid", "", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tid, err := ParseTaskID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTaskID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil {
				if tid.Module != tt.wantModule {
					t.Errorf("Module = %q, want %q", tid.Module, tt.wantModule)
				}
				if tid.Prefix != tt.wantPrefix {
					t.Errorf("Prefix = %q, want %q", tid.Prefix, tt.wantPrefix)
				}
				if tid.EpicNum != tt.wantEpic {
					t.Errorf("EpicNum = %d, want %d", tid.EpicNum, tt.wantEpic)
				}
			}
		})
	}
}

func TestTaskID_String(t *testing.T) {
	// Standard format without prefix
	tid := TaskID{Module: "technical", EpicNum: 5}
	if got := tid.String(); got != "technical/E05" {
		t.Errorf("String() = %q, want %q", got, "technical/E05")
	}

	// With prefix
	tidWithPrefix := TaskID{Module: "cli-impl", Prefix: "CLI", EpicNum: 2}
	if got := tidWithPrefix.String(); got != "cli-impl/CLI02" {
		t.Errorf("String() = %q, want %q", got, "cli-impl/CLI02")
	}

	// Verify round-trip: parse -> string -> parse
	original := "module/TUI05"
	parsed, _ := ParseTaskID(original)
	if got := parsed.String(); got != original {
		t.Errorf("Round-trip failed: %q -> %q", original, got)
	}
}

func TestTask_IsReady(t *testing.T) {
	completed := map[string]bool{"technical/E04": true}

	task := Task{
		ID:        TaskID{Module: "technical", EpicNum: 5},
		DependsOn: []TaskID{{Module: "technical", EpicNum: 4}},
		Status:    StatusNotStarted,
	}

	if !task.IsReady(completed) {
		t.Error("Task should be ready when dependencies are complete")
	}

	task.DependsOn = append(task.DependsOn, TaskID{Module: "billing", EpicNum: 1})
	if task.IsReady(completed) {
		t.Error("Task should not be ready when dependencies are incomplete")
	}
}

func TestTask_HasGitHubIssue(t *testing.T) {
	issueNum := 42
	taskWithIssue := Task{GitHubIssue: &issueNum}
	taskWithoutIssue := Task{}

	if !taskWithIssue.HasGitHubIssue() {
		t.Error("expected HasGitHubIssue() = true for task with issue")
	}
	if taskWithoutIssue.HasGitHubIssue() {
		t.Error("expected HasGitHubIssue() = false for task without issue")
	}
}
