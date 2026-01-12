package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// AgentStatus represents the status of an agent
type AgentStatus string

const (
	AgentQueued    AgentStatus = "queued"
	AgentRunning   AgentStatus = "running"
	AgentCompleted AgentStatus = "completed"
	AgentFailed    AgentStatus = "failed"
	AgentStuck     AgentStatus = "stuck"
)

// Agent represents a Claude Code agent working on a task
type Agent struct {
	TaskID       domain.TaskID
	WorktreePath string
	Status       AgentStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Prompt       string
	Output       []string
	Error        error

	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
}

// AgentManager manages concurrent agent execution
type AgentManager struct {
	maxConcurrent int
	agents        map[string]*Agent
	mu            sync.RWMutex
}

// NewAgentManager creates a new AgentManager
func NewAgentManager(maxConcurrent int) *AgentManager {
	return &AgentManager{
		maxConcurrent: maxConcurrent,
		agents:        make(map[string]*Agent),
	}
}

// Add adds an agent to the manager
func (m *AgentManager) Add(agent *Agent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[agent.TaskID.String()] = agent
}

// Get retrieves an agent by task ID
func (m *AgentManager) Get(taskID string) *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[taskID]
}

// Remove removes an agent from the manager
func (m *AgentManager) Remove(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, taskID)
}

// RunningCount returns the number of running agents
func (m *AgentManager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, a := range m.agents {
		if a.Status == AgentRunning {
			count++
		}
	}
	return count
}

// QueuedCount returns the number of queued agents
func (m *AgentManager) QueuedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, a := range m.agents {
		if a.Status == AgentQueued {
			count++
		}
	}
	return count
}

// CanStart returns true if another agent can be started
func (m *AgentManager) CanStart() bool {
	return m.RunningCount() < m.maxConcurrent
}

// Start starts an agent with Claude Code
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.Status != AgentQueued {
		return fmt.Errorf("agent not in queued state: %s", a.Status)
	}

	if a.Prompt == "" {
		return fmt.Errorf("agent has no prompt")
	}

	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Build claude command with prompt as argument
	a.cmd = exec.CommandContext(ctx, "claude",
		"--print",                        // Non-interactive mode
		"--dangerously-skip-permissions", // Skip permission prompts
		"--output-format", "stream-json", // Stream output as JSON for realtime updates
		"-p", a.Prompt,                   // Pass prompt as argument
	)
	a.cmd.Dir = a.WorktreePath

	// Capture output
	stdout, err := a.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := a.cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the process
	if err := a.cmd.Start(); err != nil {
		return fmt.Errorf("starting claude: %w", err)
	}

	now := time.Now()
	a.StartedAt = &now
	a.Status = AgentRunning

	// Stream output in background
	go a.streamOutput(stdout, stderr)

	return nil
}

func (a *Agent) streamOutput(stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	readLines := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			a.mu.Lock()
			a.Output = append(a.Output, scanner.Text())
			a.mu.Unlock()
		}
	}

	go readLines(stdout)
	go readLines(stderr)
	wg.Wait()

	// Wait for process to finish
	err := a.cmd.Wait()

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	a.FinishedAt = &now

	if err != nil {
		a.Status = AgentFailed
		a.Error = err
	} else {
		a.Status = AgentCompleted
	}
}

// Stop gracefully stops the agent
func (a *Agent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
}

// GetOutput returns a copy of the output lines
func (a *Agent) GetOutput() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]string, len(a.Output))
	copy(result, a.Output)
	return result
}

// Duration returns how long the agent has been running
func (a *Agent) Duration() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.StartedAt == nil {
		return 0
	}
	if a.FinishedAt != nil {
		return a.FinishedAt.Sub(*a.StartedAt)
	}
	return time.Since(*a.StartedAt)
}
