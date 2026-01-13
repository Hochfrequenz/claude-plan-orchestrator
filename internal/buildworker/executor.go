// Package buildworker provides job execution with git worktree isolation
// for the distributed build system.
package buildworker

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// Job represents a job to execute
type Job struct {
	ID      string
	Repo    string
	Commit  string
	Command string
	Env     map[string]string
	Timeout time.Duration
}

// OutputCallback is called for each line of output
type OutputCallback func(stream, data string)

// ExecutorConfig configures the job executor
type ExecutorConfig struct {
	GitCacheDir string
	WorktreeDir string
	UseNixShell bool
}

// Executor runs jobs in isolated worktrees
type Executor struct {
	config ExecutorConfig
}

// NewExecutor creates a new job executor
func NewExecutor(config ExecutorConfig) *Executor {
	return &Executor{config: config}
}

// RunJob executes a job and returns the result
func (e *Executor) RunJob(ctx context.Context, job Job, onOutput OutputCallback) (*buildprotocol.JobResult, error) {
	start := time.Now()

	// Create worktree for this job
	wtPath, err := e.createWorktree(job.ID, job.Repo, job.Commit)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}
	defer e.removeWorktree(job.Repo, wtPath)

	// Build command
	var cmd *exec.Cmd
	if e.config.UseNixShell {
		cmd = exec.CommandContext(ctx, "nix", "develop", "--command", "sh", "-c", job.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", job.Command)
	}
	cmd.Dir = wtPath

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range job.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture output
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var output strings.Builder

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %w", err)
	}

	// Stream output
	done := make(chan struct{})
	go func() {
		e.streamOutput(stdout, "stdout", &output, onOutput)
		done <- struct{}{}
	}()
	go func() {
		e.streamOutput(stderr, "stderr", &output, onOutput)
		done <- struct{}{}
	}()

	<-done
	<-done

	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("command failed: %w", err)
		}
	}

	duration := time.Since(start)

	return &buildprotocol.JobResult{
		JobID:        job.ID,
		ExitCode:     exitCode,
		Output:       output.String(),
		DurationSecs: duration.Seconds(),
	}, nil
}

func (e *Executor) createWorktree(jobID, repo, commit string) (string, error) {
	// Ensure worktree directory exists
	if err := os.MkdirAll(e.config.WorktreeDir, 0755); err != nil {
		return "", err
	}

	suffix := randomSuffix()
	wtPath := filepath.Join(e.config.WorktreeDir, fmt.Sprintf("job-%s-%s", jobID, suffix))

	// For local repos (testing), just create worktree directly
	if !strings.HasPrefix(repo, "git://") && !strings.HasPrefix(repo, "https://") {
		cmd := exec.Command("git", "worktree", "add", "--detach", wtPath, commit)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git worktree add: %s: %w", out, err)
		}
		return wtPath, nil
	}

	// For remote repos, fetch first
	cmd := exec.Command("git", "fetch", repo, commit)
	cmd.Dir = e.config.GitCacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git fetch: %s: %w", out, err)
	}

	cmd = exec.Command("git", "worktree", "add", "--detach", wtPath, "FETCH_HEAD")
	cmd.Dir = e.config.GitCacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", out, err)
	}

	return wtPath, nil
}

func (e *Executor) removeWorktree(repo, wtPath string) error {
	// Determine the git directory to use for worktree removal
	gitDir := repo
	if strings.HasPrefix(repo, "git://") || strings.HasPrefix(repo, "https://") {
		gitDir = e.config.GitCacheDir
	}

	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = gitDir
	cmd.Run() // Best effort
	return nil
}

func (e *Executor) streamOutput(r io.Reader, stream string, output *strings.Builder, callback OutputCallback) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		output.WriteString(line)
		if callback != nil {
			callback(stream, line)
		}
	}
}

func randomSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
