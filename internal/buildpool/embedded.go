// internal/buildpool/embedded.go
package buildpool

import (
	"context"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildworker"
)

// EmbeddedConfig configures the embedded worker
type EmbeddedConfig struct {
	RepoDir     string
	WorktreeDir string
	MaxJobs     int
	UseNixShell bool
}

// EmbeddedWorker runs jobs locally as fallback
type EmbeddedWorker struct {
	executor *buildworker.Executor
	pool     *buildworker.Pool
}

// NewEmbeddedWorker creates a new embedded worker
func NewEmbeddedWorker(config EmbeddedConfig) *EmbeddedWorker {
	return &EmbeddedWorker{
		executor: buildworker.NewExecutor(buildworker.ExecutorConfig{
			GitCacheDir: config.RepoDir,
			WorktreeDir: config.WorktreeDir,
			UseNixShell: config.UseNixShell,
		}),
		pool: buildworker.NewPool(config.MaxJobs),
	}
}

// Run executes a job and returns the result
func (e *EmbeddedWorker) Run(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
	if !e.pool.Acquire() {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 1,
			Output:   "embedded worker: no slots available",
		}
	}
	defer e.pool.Release()

	timeout := time.Duration(job.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := e.executor.RunJob(ctx, buildworker.Job{
		ID:      job.JobID,
		Repo:    job.Repo,
		Commit:  job.Commit,
		Command: job.Command,
		Env:     job.Env,
		Timeout: timeout,
	}, nil) // No streaming for embedded worker

	if err != nil {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: -1,
			Output:   "embedded worker error: " + err.Error(),
		}
	}

	return result
}
