package domain

import (
	"testing"
)

func TestTaskID_Parse(t *testing.T) {
	tests := []struct {
		input      string
		wantModule string
		wantEpic   int
		wantErr    bool
	}{
		{"technical/E05", "technical", 5, false},
		{"billing/E00", "billing", 0, false},
		{"pricing/E123", "pricing", 123, false},
		{"invalid", "", 0, true},
		{"module/invalid", "", 0, true},
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
				if tid.EpicNum != tt.wantEpic {
					t.Errorf("EpicNum = %d, want %d", tid.EpicNum, tt.wantEpic)
				}
			}
		})
	}
}

func TestTaskID_String(t *testing.T) {
	tid := TaskID{Module: "technical", EpicNum: 5}
	if got := tid.String(); got != "technical/E05" {
		t.Errorf("String() = %q, want %q", got, "technical/E05")
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
