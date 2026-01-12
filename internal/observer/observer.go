package observer

import (
	"sync"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/executor"
)

// Observer monitors agent execution and collects metrics
type Observer struct {
	stuckThreshold time.Duration

	completions []completion
	mu          sync.RWMutex
}

type completion struct {
	TaskID       string
	Duration     time.Duration
	TokensInput  int
	TokensOutput int
	CompletedAt  time.Time
}

// Metrics holds aggregated metrics
type Metrics struct {
	TotalCompleted    int
	TotalFailed       int
	TotalTokensInput  int
	TotalTokensOutput int
	AvgDuration       time.Duration
}

// New creates a new Observer
func New(stuckThreshold time.Duration) *Observer {
	return &Observer{
		stuckThreshold: stuckThreshold,
	}
}

// IsStuck returns true if an agent appears to be stuck
func (o *Observer) IsStuck(agent *executor.Agent) bool {
	if agent.Status != executor.AgentRunning {
		return false
	}
	if agent.StartedAt == nil {
		return false
	}
	return time.Since(*agent.StartedAt) > o.stuckThreshold
}

// RecordCompletion records a task completion
func (o *Observer) RecordCompletion(taskID string, duration time.Duration, tokensIn, tokensOut int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.completions = append(o.completions, completion{
		TaskID:       taskID,
		Duration:     duration,
		TokensInput:  tokensIn,
		TokensOutput: tokensOut,
		CompletedAt:  time.Now(),
	})
}

// GetMetrics returns aggregated metrics
func (o *Observer) GetMetrics() Metrics {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var metrics Metrics
	var totalDuration time.Duration

	for _, c := range o.completions {
		metrics.TotalCompleted++
		metrics.TotalTokensInput += c.TokensInput
		metrics.TotalTokensOutput += c.TokensOutput
		totalDuration += c.Duration
	}

	if metrics.TotalCompleted > 0 {
		metrics.AvgDuration = totalDuration / time.Duration(metrics.TotalCompleted)
	}

	return metrics
}

// GetRecentCompletions returns completions from the last duration
func (o *Observer) GetRecentCompletions(since time.Duration) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	cutoff := time.Now().Add(-since)
	var result []string

	for _, c := range o.completions {
		if c.CompletedAt.After(cutoff) {
			result = append(result, c.TaskID)
		}
	}

	return result
}
