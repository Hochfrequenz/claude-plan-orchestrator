package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// StatusChangeCallback is called when an agent's status changes
type StatusChangeCallback func(agent *Agent, newStatus AgentStatus, errMsg string)

// Agent represents a Claude Code agent working on a task
type Agent struct {
	ID           string // Unique identifier for this agent run
	TaskID       domain.TaskID
	WorktreePath string
	LogPath      string // Path to the output log file
	PID          int    // Process ID of the running claude process
	Status       AgentStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Prompt       string
	Output       []string
	Error        error

	// Token usage from Claude session
	TokensInput  int
	TokensOutput int
	CostUSD      float64

	OnStatusChange StatusChangeCallback // Called when status changes

	cmd     *exec.Cmd
	cancel  context.CancelFunc
	logFile *os.File
	mu      sync.Mutex
}

// AgentStore defines the interface for persisting agent runs
type AgentStore interface {
	SaveAgentRun(run *AgentRunRecord) error
	UpdateAgentRunStatus(id string, status string, errorMessage string) error
	UpdateAgentRunUsage(id string, tokensInput, tokensOutput int, costUSD float64) error
	ListActiveAgentRuns() ([]*AgentRunRecord, error)
	DeleteAgentRun(id string) error
}

// AgentRunRecord represents a persisted agent run (matches taskstore.AgentRun)
type AgentRunRecord struct {
	ID           string
	TaskID       string
	WorktreePath string
	LogPath      string
	PID          int
	Status       string
	StartedAt    time.Time
	FinishedAt   *time.Time
	ErrorMessage string
	TokensInput  int
	TokensOutput int
	CostUSD      float64
}

// AgentManager manages concurrent agent execution
type AgentManager struct {
	maxConcurrent int
	agents        map[string]*Agent
	store         AgentStore
	mu            sync.RWMutex
}

// NewAgentManager creates a new AgentManager
func NewAgentManager(maxConcurrent int) *AgentManager {
	return &AgentManager{
		maxConcurrent: maxConcurrent,
		agents:        make(map[string]*Agent),
	}
}

// SetStore sets the persistence store for the agent manager
func (m *AgentManager) SetStore(store AgentStore) {
	m.store = store
}

// Add adds an agent to the manager and persists it
func (m *AgentManager) Add(agent *Agent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[agent.TaskID.String()] = agent

	// Persist to database if store is set
	if m.store != nil && agent.ID != "" {
		startedAt := time.Now()
		if agent.StartedAt != nil {
			startedAt = *agent.StartedAt
		}
		m.store.SaveAgentRun(&AgentRunRecord{
			ID:           agent.ID,
			TaskID:       agent.TaskID.String(),
			WorktreePath: agent.WorktreePath,
			LogPath:      agent.LogPath,
			PID:          agent.PID,
			Status:       string(agent.Status),
			StartedAt:    startedAt,
		})
	}
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
	agent := m.agents[taskID]
	delete(m.agents, taskID)

	// Remove from database if store is set
	if m.store != nil && agent != nil && agent.ID != "" {
		m.store.DeleteAgentRun(agent.ID)
	}
}

// UpdateAgentStatus updates an agent's status and persists to database
func (m *AgentManager) UpdateAgentStatus(taskID string, status AgentStatus, errMsg string) {
	m.mu.Lock()
	agent := m.agents[taskID]
	m.mu.Unlock()

	if agent == nil {
		return
	}

	agent.mu.Lock()
	agent.Status = status
	if errMsg != "" {
		agent.Error = fmt.Errorf("%s", errMsg)
	}
	agentID := agent.ID
	agent.mu.Unlock()

	// Update in database if store is set
	if m.store != nil && agentID != "" {
		m.store.UpdateAgentRunStatus(agentID, string(status), errMsg)
	}
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

	// Generate unique ID if not set
	if a.ID == "" {
		a.ID = fmt.Sprintf("%s-%d", a.TaskID.String(), time.Now().UnixNano())
	}

	// Create log file in worktree
	a.LogPath = filepath.Join(a.WorktreePath, ".claude-agent.log")
	logFile, err := os.Create(a.LogPath)
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}
	a.logFile = logFile

	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Build claude command with prompt as argument
	a.cmd = exec.CommandContext(ctx, "claude",
		"--print",                        // Non-interactive mode
		"--verbose",                      // Required for stream-json output
		"--dangerously-skip-permissions", // Skip permission prompts
		"--output-format", "stream-json", // Stream output as JSON for realtime updates
		"-p", a.Prompt,                   // Pass prompt as argument
	)
	a.cmd.Dir = a.WorktreePath

	// Capture output
	stdout, err := a.cmd.StdoutPipe()
	if err != nil {
		a.logFile.Close()
		return err
	}
	stderr, err := a.cmd.StderrPipe()
	if err != nil {
		a.logFile.Close()
		return err
	}

	// Start the process
	if err := a.cmd.Start(); err != nil {
		a.logFile.Close()
		return fmt.Errorf("starting claude: %w", err)
	}

	// Capture PID
	a.PID = a.cmd.Process.Pid

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
		// Increase buffer size for long JSON lines
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			// Try to parse token usage from result messages
			a.parseUsageFromLine(line)
			a.mu.Lock()
			a.Output = append(a.Output, line)
			// Write to log file
			if a.logFile != nil {
				a.logFile.WriteString(line + "\n")
				a.logFile.Sync() // Flush to disk for tail -f
			}
			a.mu.Unlock()
		}
	}

	go readLines(stdout)
	go readLines(stderr)
	wg.Wait()

	// Wait for process to finish
	err := a.cmd.Wait()

	a.mu.Lock()
	now := time.Now()
	a.FinishedAt = &now

	var newStatus AgentStatus
	var errMsg string
	if err != nil {
		a.Status = AgentFailed
		a.Error = err
		newStatus = AgentFailed
		errMsg = err.Error()
	} else {
		a.Status = AgentCompleted
		newStatus = AgentCompleted
	}

	// Close log file
	if a.logFile != nil {
		a.logFile.Close()
		a.logFile = nil
	}

	callback := a.OnStatusChange
	a.mu.Unlock()

	// Call status change callback outside of lock
	if callback != nil {
		callback(a, newStatus, errMsg)
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

// claudeResultMessage represents the final result message from Claude Code
type claudeResultMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Usage     struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// parseUsageFromLine tries to parse token usage from a stream-json line
func (a *Agent) parseUsageFromLine(line string) {
	var msg claudeResultMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}

	// Only process result messages
	if msg.Type == "result" {
		a.mu.Lock()
		a.TokensInput = msg.Usage.InputTokens
		a.TokensOutput = msg.Usage.OutputTokens
		a.CostUSD = msg.CostUSD
		a.mu.Unlock()
	}
}

// GetUsage returns token usage (input, output, cost)
func (a *Agent) GetUsage() (int, int, float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.TokensInput, a.TokensOutput, a.CostUSD
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

// IsProcessRunning checks if the agent's process is still running
func (a *Agent) IsProcessRunning() bool {
	if a.PID == 0 {
		return false
	}
	// On Unix, sending signal 0 checks if process exists
	process, err := os.FindProcess(a.PID)
	if err != nil {
		return false
	}
	// Try to send signal 0 - this doesn't actually send a signal,
	// but returns an error if the process doesn't exist
	err = process.Signal(os.Signal(nil))
	return err == nil
}

// LoadOutputFromLog reads the last N lines from the log file
func (a *Agent) LoadOutputFromLog(maxLines int) error {
	if a.LogPath == "" {
		return nil
	}

	file, err := os.Open(a.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Log file doesn't exist yet
		}
		return err
	}
	defer file.Close()

	// Read all lines (we could optimize with ring buffer for large files)
	var lines []string
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Keep only last maxLines
	if len(lines) > maxLines {
		a.Output = lines[len(lines)-maxLines:]
	} else {
		a.Output = lines
	}

	return scanner.Err()
}

// TailLogFile starts tailing the log file and updating Output
func (a *Agent) TailLogFile(ctx context.Context) error {
	if a.LogPath == "" {
		return fmt.Errorf("no log path set")
	}

	go func() {
		file, err := os.Open(a.LogPath)
		if err != nil {
			return
		}
		defer file.Close()

		// Seek to end
		file.Seek(0, io.SeekEnd)

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				if scanner.Scan() {
					line := scanner.Text()
					a.mu.Lock()
					a.Output = append(a.Output, line)
					a.mu.Unlock()
				} else {
					// No new data, wait a bit
					time.Sleep(100 * time.Millisecond)
					// Refresh scanner to pick up new data
					scanner = bufio.NewScanner(file)
					scanner.Buffer(buf, 1024*1024)
				}
			}
		}
	}()

	return nil
}

// GetAll returns all agents in the manager
func (m *AgentManager) GetAll() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	agents := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	return agents
}

// RecoverAgents loads active agents from the database and checks their status
func (m *AgentManager) RecoverAgents(ctx context.Context) ([]*Agent, error) {
	if m.store == nil {
		return nil, nil
	}

	runs, err := m.store.ListActiveAgentRuns()
	if err != nil {
		return nil, err
	}

	var recovered []*Agent
	for _, run := range runs {
		// Parse task ID
		taskID, err := domain.ParseTaskID(run.TaskID)
		if err != nil {
			// Invalid task ID, mark as failed and skip
			m.store.UpdateAgentRunStatus(run.ID, "failed", "invalid task ID")
			continue
		}

		agent := &Agent{
			ID:           run.ID,
			TaskID:       taskID,
			WorktreePath: run.WorktreePath,
			LogPath:      run.LogPath,
			PID:          run.PID,
			StartedAt:    &run.StartedAt,
		}

		// Check if process is still running
		if agent.IsProcessRunning() {
			agent.Status = AgentRunning
			// Load recent output from log file
			agent.LoadOutputFromLog(100)
			// Start tailing the log file
			agent.TailLogFile(ctx)
		} else {
			// Process is no longer running - check if it completed or failed
			// We'll mark it as completed since we can't know for sure
			agent.Status = AgentCompleted
			now := time.Now()
			agent.FinishedAt = &now
			// Load all output from log file
			agent.LoadOutputFromLog(100)
			// Update database
			m.store.UpdateAgentRunStatus(run.ID, "completed", "")
		}

		// Add to manager
		m.mu.Lock()
		m.agents[run.TaskID] = agent
		m.mu.Unlock()

		recovered = append(recovered, agent)
	}

	return recovered, nil
}

// CreateStatusCallback returns a callback that updates the manager's store
func (m *AgentManager) CreateStatusCallback() StatusChangeCallback {
	return func(agent *Agent, newStatus AgentStatus, errMsg string) {
		if m.store != nil && agent.ID != "" {
			m.store.UpdateAgentRunStatus(agent.ID, string(newStatus), errMsg)
			// Save token usage when agent completes
			if newStatus == AgentCompleted || newStatus == AgentFailed {
				tokensIn, tokensOut, cost := agent.GetUsage()
				if tokensIn > 0 || tokensOut > 0 {
					m.store.UpdateAgentRunUsage(agent.ID, tokensIn, tokensOut, cost)
				}
			}
		}
	}
}
