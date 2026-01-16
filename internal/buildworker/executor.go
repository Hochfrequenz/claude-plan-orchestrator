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
	"log"
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
	Debug       bool
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

	if e.config.Debug {
		log.Printf("[executor] starting job %s: repo=%s commit=%s command=%q",
			job.ID, job.Repo, job.Commit, job.Command)
	}

	var wtPath string
	var err error

	// Only create worktree if a repo is specified
	if job.Repo != "" {
		if e.config.Debug {
			log.Printf("[executor] creating worktree for job %s from %s@%s", job.ID, job.Repo, job.Commit)
		}
		wtPath, err = e.createWorktree(job.ID, job.Repo, job.Commit)
		if err != nil {
			return nil, fmt.Errorf("creating worktree: %w", err)
		}
		if e.config.Debug {
			log.Printf("[executor] worktree created at %s", wtPath)
		}
		defer e.removeWorktree(job.Repo, wtPath)
	} else {
		// No repo - create a temp directory for the job
		tempBase := e.config.WorktreeDir
		if tempBase == "" {
			tempBase = os.TempDir()
		}
		wtPath, err = os.MkdirTemp(tempBase, fmt.Sprintf("job-%s-", job.ID))
		if err != nil {
			return nil, fmt.Errorf("creating temp dir: %w", err)
		}
		if e.config.Debug {
			log.Printf("[executor] temp dir created at %s (no repo)", wtPath)
		}
		defer os.RemoveAll(wtPath)
	}

	// Build command
	var cmd *exec.Cmd
	if e.config.UseNixShell {
		if e.config.Debug {
			log.Printf("[executor] running with nix develop: nix develop --command sh -c %q", job.Command)
		}
		cmd = exec.CommandContext(ctx, "nix", "develop", "--command", "sh", "-c", job.Command)
	} else {
		if e.config.Debug {
			log.Printf("[executor] running directly: sh -c %q", job.Command)
		}
		cmd = exec.CommandContext(ctx, "sh", "-c", job.Command)
	}
	cmd.Dir = wtPath

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range job.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture output - track stdout and stderr separately for verbosity filtering
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var stdoutBuf, stderrBuf strings.Builder

	if e.config.Debug {
		log.Printf("[executor] starting command in %s", wtPath)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %w", err)
	}
	if e.config.Debug {
		log.Printf("[executor] command started with PID %d", cmd.Process.Pid)
	}

	// Stream output - capture to separate buffers
	done := make(chan struct{})
	go func() {
		e.streamOutput(stdout, "stdout", &stdoutBuf, onOutput)
		done <- struct{}{}
	}()
	go func() {
		e.streamOutput(stderr, "stderr", &stderrBuf, onOutput)
		done <- struct{}{}
	}()

	<-done
	<-done

	if e.config.Debug {
		log.Printf("[executor] waiting for command to complete...")
	}
	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			if e.config.Debug {
				log.Printf("[executor] command exited with code %d", exitCode)
			}
		} else {
			return nil, fmt.Errorf("command failed: %w", err)
		}
	} else if e.config.Debug {
		log.Printf("[executor] command completed successfully")
	}

	duration := time.Since(start)
	if e.config.Debug {
		log.Printf("[executor] job %s finished in %.2fs with exit code %d", job.ID, duration.Seconds(), exitCode)
	}

	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	return &buildprotocol.JobResult{
		JobID:        job.ID,
		ExitCode:     exitCode,
		Output:       stdoutStr + stderrStr, // Combined for backwards compat
		Stdout:       stdoutStr,              // Separate for verbosity filtering
		Stderr:       stderrStr,              // Separate for verbosity filtering
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

	// For local repos, auto-commit changes and use HEAD
	if !strings.HasPrefix(repo, "git://") && !strings.HasPrefix(repo, "https://") {
		if e.config.Debug {
			log.Printf("[executor] local repo detected, checking for uncommitted changes")
		}

		// Auto-commit any uncommitted changes so worktree can access them
		if err := e.autoCommitChanges(repo); err != nil {
			if e.config.Debug {
				log.Printf("[executor] auto-commit skipped or failed: %v", err)
			}
			// Continue anyway - worktree creation might still work
		}

		// Use HEAD instead of passed commit (which may be stale)
		if e.config.Debug {
			log.Printf("[executor] creating worktree from HEAD at %s", repo)
		}
		cmd := exec.Command("git", "worktree", "add", "--detach", wtPath, "HEAD")
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git worktree add: %s: %w", out, err)
		}
		return wtPath, nil
	}

	// For remote repos, fetch into git cache directory
	// First ensure git cache dir exists and is a git repo
	if e.config.Debug {
		log.Printf("[executor] remote repo detected, initializing git cache")
	}
	if err := e.ensureGitCacheDir(); err != nil {
		return "", fmt.Errorf("git cache init: %w", err)
	}
	if e.config.Debug {
		log.Printf("[executor] git cache dir: %s", e.config.GitCacheDir)
	}

	if e.config.Debug {
		log.Printf("[executor] fetching: git fetch %s %s", repo, commit)
	}
	cmd := exec.Command("git", "fetch", repo, commit)
	cmd.Dir = e.config.GitCacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git fetch: %s: %w", out, err)
	}
	if e.config.Debug {
		log.Printf("[executor] fetch completed successfully")
	}

	if e.config.Debug {
		log.Printf("[executor] creating worktree at %s from FETCH_HEAD", wtPath)
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

// ensureGitCacheDir ensures the git cache directory exists and is a git repo
func (e *Executor) ensureGitCacheDir() error {
	// Try configured directory first, fall back to user cache dir
	dirs := []string{e.config.GitCacheDir}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".cache", "build-agent", "repos"))
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), "build-agent-repos"))

	var lastErr error
	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		// Try to create and initialize this directory
		if err := e.initGitCacheDir(dir); err != nil {
			lastErr = err
			continue
		}

		// Success - update config to use this directory
		e.config.GitCacheDir = dir
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no writable git cache directory found")
}

func (e *Executor) initGitCacheDir(dir string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Check if it's already a git repo
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil // Already a git repo
	}

	// Also check if it's a bare repo (no .git subdir, but has HEAD)
	headFile := filepath.Join(dir, "HEAD")
	if _, err := os.Stat(headFile); err == nil {
		return nil // Already a bare git repo
	}

	// Initialize as a bare repository
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init --bare: %s: %w", out, err)
	}

	return nil
}

// autoCommitChanges creates a WIP commit with any uncommitted changes
// This allows worktrees to access the current working state
func (e *Executor) autoCommitChanges(repoDir string) error {
	// Check if there are any changes (staged, unstaged, or untracked)
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}

	// No changes to commit
	if len(out) == 0 {
		if e.config.Debug {
			log.Printf("[executor] no uncommitted changes to auto-commit")
		}
		return nil
	}

	if e.config.Debug {
		log.Printf("[executor] found uncommitted changes, creating WIP commit")
	}

	// Add all changes (including untracked files)
	cmd = exec.Command("git", "add", "-A")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", out, err)
	}

	// Create WIP commit
	cmd = exec.Command("git", "commit", "-m", "[WIP] Auto-commit for build-pool embedded worker", "--no-verify")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Check if "nothing to commit" - this is okay
		if strings.Contains(string(out), "nothing to commit") {
			if e.config.Debug {
				log.Printf("[executor] nothing to commit after git add")
			}
			return nil
		}
		return fmt.Errorf("git commit: %s: %w", out, err)
	}

	if e.config.Debug {
		log.Printf("[executor] WIP commit created successfully")
	}
	return nil
}
