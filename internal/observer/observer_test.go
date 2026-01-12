package observer

import (
	"testing"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
)

func TestObserver_DetectStuck(t *testing.T) {
	obs := New(5 * time.Minute)

	agent := &executor.Agent{
		TaskID: domain.TaskID{Module: "tech", EpicNum: 0},
		Status: executor.AgentRunning,
	}

	// Set started time to 10 minutes ago
	started := time.Now().Add(-10 * time.Minute)
	agent.StartedAt = &started

	if !obs.IsStuck(agent) {
		t.Error("Agent running for 10 minutes should be detected as stuck")
	}
}

func TestObserver_NotStuck(t *testing.T) {
	obs := New(5 * time.Minute)

	agent := &executor.Agent{
		TaskID: domain.TaskID{Module: "tech", EpicNum: 0},
		Status: executor.AgentRunning,
	}

	// Set started time to 2 minutes ago
	started := time.Now().Add(-2 * time.Minute)
	agent.StartedAt = &started

	if obs.IsStuck(agent) {
		t.Error("Agent running for 2 minutes should not be stuck")
	}
}

func TestObserver_Metrics(t *testing.T) {
	obs := New(5 * time.Minute)

	obs.RecordCompletion("tech/E00", 5*time.Minute, 1000, 500)
	obs.RecordCompletion("tech/E01", 10*time.Minute, 2000, 1000)

	metrics := obs.GetMetrics()

	if metrics.TotalCompleted != 2 {
		t.Errorf("TotalCompleted = %d, want 2", metrics.TotalCompleted)
	}
	if metrics.TotalTokensInput != 3000 {
		t.Errorf("TotalTokensInput = %d, want 3000", metrics.TotalTokensInput)
	}
	if metrics.AvgDuration != 7*time.Minute+30*time.Second {
		t.Errorf("AvgDuration = %v, want 7m30s", metrics.AvgDuration)
	}
}
