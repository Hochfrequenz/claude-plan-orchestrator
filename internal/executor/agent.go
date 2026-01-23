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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	isync "github.com/hochfrequenz/claude-plan-orchestrator/internal/sync"
)

// orchestratorNamespace is a fixed UUID namespace for generating deterministic session IDs
// This ensures the same task always gets the same session ID for resume capability
var orchestratorNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// ExecutorType defines the AI coding agent to use
type ExecutorType string

const (
	ExecutorClaudeCode ExecutorType = "claude-code"
	ExecutorOpenCode   ExecutorType = "opencode"
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
	ID            string // Unique identifier for this agent run
	TaskID        domain.TaskID
	WorktreePath  string
	LogPath       string // Path to the output log file
	EpicFilePath  string // Path to the epic markdown file in the main repo (for sync)
	PID           int    // Process ID of the running claude process
	Status        AgentStatus
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Prompt        string
	Output        []string
	Error         error
	SessionID     string       // Claude Code session ID for resume capability
	BuildPoolURL  string       // URL for build pool coordinator (if configured)
	ExecutorType  ExecutorType // Which AI coding agent to use (claude-code or opencode)
	OpenCodeModel string       // Model to use for OpenCode (e.g., "zai-coding-plan/glm-4.7")

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
	ListRecentAgentRuns(limit int) ([]*AgentRunRecord, error)
	DeleteAgentRun(id string) error
	UpdateTaskStatus(id string, status domain.TaskStatus) error
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
	SessionID    string // Claude Code session ID for resume capability
	TokensInput  int
	TokensOutput int
	CostUSD      float64
}

// dbOp represents a database operation to be executed by the write queue
type dbOp struct {
	opType       string
	agentRunID   string
	record       *AgentRunRecord
	status       string
	errorMessage string
	tokensInput  int
	tokensOutput int
	costUSD      float64
	taskID       string
	taskStatus   domain.TaskStatus
}

// AgentManager manages concurrent agent execution
type AgentManager struct {
	maxConcurrent int
	agents        map[string]*Agent
	store         AgentStore
	syncer        *isync.Syncer
	buildPoolURL  string
	executorType  ExecutorType // Default executor for new agents
	openCodeModel string       // Model to use for OpenCode (e.g., "zai-coding-plan/glm-4.7")
	mu            sync.RWMutex

	// Database write queue for serializing DB operations
	dbWriteChan chan dbOp
	dbWriteDone chan struct{}
}

// NewAgentManager creates a new AgentManager
func NewAgentManager(maxConcurrent int) *AgentManager {
	m := &AgentManager{
		maxConcurrent: maxConcurrent,
		agents:        make(map[string]*Agent),
		dbWriteChan:   make(chan dbOp, 100), // Buffer for async writes
		dbWriteDone:   make(chan struct{}),
	}
	// Start the database write goroutine
	go m.dbWriter()
	return m
}

// dbWriter processes database operations sequentially to avoid lock contention
func (m *AgentManager) dbWriter() {
	for op := range m.dbWriteChan {
		if m.store == nil {
			continue
		}
		switch op.opType {
		case "save":
			m.store.SaveAgentRun(op.record)
		case "delete":
			m.store.DeleteAgentRun(op.agentRunID)
		case "updateStatus":
			m.store.UpdateAgentRunStatus(op.agentRunID, op.status, op.errorMessage)
		case "updateUsage":
			m.store.UpdateAgentRunUsage(op.agentRunID, op.tokensInput, op.tokensOutput, op.costUSD)
		case "updateTaskStatus":
			if err := m.store.UpdateTaskStatus(op.taskID, op.taskStatus); err != nil {
				fmt.Printf("Warning: failed to update task status in DB for %s: %v\n", op.taskID, err)
			}
		}
	}
	close(m.dbWriteDone)
}

// StopDBWriter stops the database writer goroutine and waits for it to finish
func (m *AgentManager) StopDBWriter() {
	if m.dbWriteChan != nil {
		close(m.dbWriteChan)
		<-m.dbWriteDone
	}
}

// queueDBOp queues a database operation for async execution
func (m *AgentManager) queueDBOp(op dbOp) {
	select {
	case m.dbWriteChan <- op:
	default:
		// Channel full, execute synchronously as fallback
		if m.store == nil {
			return
		}
		switch op.opType {
		case "save":
			m.store.SaveAgentRun(op.record)
		case "delete":
			m.store.DeleteAgentRun(op.agentRunID)
		case "updateStatus":
			m.store.UpdateAgentRunStatus(op.agentRunID, op.status, op.errorMessage)
		case "updateUsage":
			m.store.UpdateAgentRunUsage(op.agentRunID, op.tokensInput, op.tokensOutput, op.costUSD)
		case "updateTaskStatus":
			if err := m.store.UpdateTaskStatus(op.taskID, op.taskStatus); err != nil {
				fmt.Printf("Warning: failed to update task status in DB for %s: %v\n", op.taskID, err)
			}
		}
	}
}

// SetStore sets the persistence store for the agent manager
func (m *AgentManager) SetStore(store AgentStore) {
	m.store = store
}

// SetSyncer sets the syncer for updating epic and README status
func (m *AgentManager) SetSyncer(syncer *isync.Syncer) {
	m.syncer = syncer
}

// SetBuildPoolURL sets the URL for the build pool coordinator
func (m *AgentManager) SetBuildPoolURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildPoolURL = url
}

// GetBuildPoolURL returns the URL for the build pool coordinator
func (m *AgentManager) GetBuildPoolURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buildPoolURL
}

// SetExecutorType sets the default executor type for new agents
func (m *AgentManager) SetExecutorType(executorType ExecutorType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executorType = executorType
}

// GetExecutorType returns the default executor type
func (m *AgentManager) GetExecutorType() ExecutorType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.executorType == "" {
		return ExecutorClaudeCode
	}
	return m.executorType
}

// SetOpenCodeModel sets the model to use for OpenCode
func (m *AgentManager) SetOpenCodeModel(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openCodeModel = model
}

// GetOpenCodeModel returns the model to use for OpenCode
func (m *AgentManager) GetOpenCodeModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.openCodeModel
}

// SetMaxConcurrent updates the maximum number of concurrent agents
func (m *AgentManager) SetMaxConcurrent(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxConcurrent = max
}

// GetMaxConcurrent returns the maximum number of concurrent agents
func (m *AgentManager) GetMaxConcurrent() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxConcurrent
}

// Add adds an agent to the manager and persists it
func (m *AgentManager) Add(agent *Agent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[agent.TaskID.String()] = agent

	// Persist to database via write queue
	if agent.ID != "" {
		startedAt := time.Now()
		if agent.StartedAt != nil {
			startedAt = *agent.StartedAt
		}
		m.queueDBOp(dbOp{
			opType: "save",
			record: &AgentRunRecord{
				ID:           agent.ID,
				TaskID:       agent.TaskID.String(),
				WorktreePath: agent.WorktreePath,
				LogPath:      agent.LogPath,
				PID:          agent.PID,
				Status:       string(agent.Status),
				StartedAt:    startedAt,
				SessionID:    agent.SessionID,
			},
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

	// Remove from database via write queue
	if agent != nil && agent.ID != "" {
		m.queueDBOp(dbOp{
			opType:     "delete",
			agentRunID: agent.ID,
		})
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

	// Update in database via write queue
	if agentID != "" {
		m.queueDBOp(dbOp{
			opType:       "updateStatus",
			agentRunID:   agentID,
			status:       string(status),
			errorMessage: errMsg,
		})
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

// Start starts an agent with the configured executor (Claude Code or OpenCode)
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

	// Generate session ID for resume capability
	// Use UUID v5 (deterministic) so same task always gets same session ID
	if a.SessionID == "" {
		a.SessionID = uuid.NewSHA1(orchestratorNamespace, []byte(a.TaskID.String())).String()
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

	// Build command based on executor type
	a.cmd = a.buildCommand(ctx)

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
	execName := "claude"
	if a.ExecutorType == ExecutorOpenCode {
		execName = "opencode"
	}
	if err := a.cmd.Start(); err != nil {
		a.logFile.Close()
		return fmt.Errorf("starting %s: %w", execName, err)
	}

	// Capture PID
	a.PID = a.cmd.Process.Pid

	now := time.Now()
	a.StartedAt = &now
	a.Status = AgentRunning

	// Call status change callback for running status (triggers sync to in_progress)
	callback := a.OnStatusChange
	if callback != nil {
		go callback(a, AgentRunning, "")
	}

	// Stream output in background
	go a.streamOutput(stdout, stderr)

	return nil
}

// buildCommand creates the appropriate command based on executor type
func (a *Agent) buildCommand(ctx context.Context) *exec.Cmd {
	switch a.ExecutorType {
	case ExecutorOpenCode:
		return a.buildOpenCodeCommand(ctx)
	default:
		return a.buildClaudeCodeCommand(ctx)
	}
}

// buildClaudeCodeCommand builds the command for Claude Code
func (a *Agent) buildClaudeCodeCommand(ctx context.Context) *exec.Cmd {
	args := []string{
		"--print",                        // Non-interactive mode
		"--verbose",                      // Required for stream-json output
		"--dangerously-skip-permissions", // Skip permission prompts
		"--output-format", "stream-json", // Stream output as JSON for realtime updates
		"--session-id", a.SessionID,      // Named session for resume capability
	}

	// Add MCP config if available (from project's .mcp.json + orchestrator MCPs)
	if mcpConfig := a.generateMCPConfig(); mcpConfig != "" {
		args = append(args, "--mcp-config", mcpConfig)
	}

	// Add prompt
	args = append(args, "-p", a.Prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = a.WorktreePath
	return cmd
}

// buildOpenCodeCommand builds the command for OpenCode
func (a *Agent) buildOpenCodeCommand(ctx context.Context) *exec.Cmd {
	// Note: OpenCode manages its own session IDs (prefixed with "ses")
	// We don't pass -s here; use -c for resume instead
	// Note: --format json causes OpenCode to hang, so we use default format
	args := []string{
		"run", // Non-interactive mode
	}

	// Add model if specified (e.g., "zai-coding-plan/glm-4.7")
	if a.OpenCodeModel != "" {
		args = append(args, "-m", a.OpenCodeModel)
	}

	// Add prompt as final argument
	args = append(args, a.Prompt)

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = a.WorktreePath

	// Log the command being executed (without the full prompt for brevity)
	// Note: caller (Start) already holds the lock, so we don't acquire it here
	cmdLog := fmt.Sprintf("[orchestrator] Executing: opencode %s", strings.Join(args[:len(args)-1], " "))
	if a.OpenCodeModel == "" {
		cmdLog += " (WARNING: no model specified, will use opencode default which requires billing)"
	}
	a.Output = append(a.Output, cmdLog)
	if a.logFile != nil {
		a.logFile.WriteString(cmdLog + "\n")
		a.logFile.Sync()
	}

	// Set up environment for MCP config
	cmd.Env = os.Environ()
	if mcpConfigPath := a.generateOpenCodeMCPConfig(); mcpConfigPath != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG="+mcpConfigPath)
	}

	return cmd
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
		// Try to extract meaningful error from output (e.g., OpenCode API errors)
		if extractedErr := a.extractErrorFromOutput(); extractedErr != "" {
			a.Error = fmt.Errorf("%s: %s", err.Error(), extractedErr)
			errMsg = a.Error.Error()
		} else {
			a.Error = err
			errMsg = err.Error()
		}
		newStatus = AgentFailed
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

// extractErrorFromOutput scans output lines for error messages from executors
// (e.g., OpenCode API errors, Claude Code errors) and returns a human-readable message.
// Must be called with a.mu held (or after output is finalized).
func (a *Agent) extractErrorFromOutput() string {
	// Scan output in reverse (errors usually at the end)
	for i := len(a.Output) - 1; i >= 0 && i >= len(a.Output)-20; i-- {
		line := a.Output[i]
		if !strings.HasPrefix(line, "{") {
			continue
		}

		// Try to parse OpenCode error format: {"type":"error",...}
		var openCodeErr struct {
			Type  string `json:"type"`
			Error struct {
				Name string `json:"name"`
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &openCodeErr); err == nil && openCodeErr.Type == "error" {
			msg := openCodeErr.Error.Data.Message
			if msg == "" {
				msg = openCodeErr.Error.Name
			}
			// Extract the core message from nested JSON if present
			if strings.Contains(msg, "CreditsError") || strings.Contains(msg, "No payment method") {
				return "OpenCode billing error: No payment method configured"
			}
			if strings.Contains(msg, "Unauthorized") {
				return "OpenCode authentication error: " + msg
			}
			if msg != "" {
				return msg
			}
		}

		// Try Claude Code error format
		var claudeErr struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &claudeErr); err == nil && claudeErr.Type == "error" {
			if claudeErr.Error != "" {
				return claudeErr.Error
			}
		}
	}
	return ""
}

// CheckEpicStatus reads the epic file in the worktree and returns its current status
// This is useful before resuming to see if the task was already completed
func (a *Agent) CheckEpicStatus() (domain.TaskStatus, error) {
	epicPath, err := a.findEpicFile()
	if err != nil {
		return domain.StatusNotStarted, err
	}

	content, err := os.ReadFile(epicPath)
	if err != nil {
		return domain.StatusNotStarted, fmt.Errorf("reading epic file: %w", err)
	}

	// Parse frontmatter to get status
	fm, _, err := parseSimpleFrontmatter(content)
	if err != nil {
		return domain.StatusNotStarted, fmt.Errorf("parsing frontmatter: %w", err)
	}

	return toStatus(fm.Status), nil
}

// findEpicFile locates the epic markdown file in the worktree
func (a *Agent) findEpicFile() (string, error) {
	if a.WorktreePath == "" {
		return "", fmt.Errorf("no worktree path set")
	}

	// Look in docs/plans directories for epic files matching this task
	plansDir := filepath.Join(a.WorktreePath, "docs", "plans")
	epicPattern := regexp.MustCompile(fmt.Sprintf(`epic-0*%d-.*\.md$`, a.TaskID.EpicNum))

	var foundPath string
	filepath.Walk(plansDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		// Check if this is our epic file (matching module and epic number)
		if epicPattern.MatchString(base) {
			// Check if directory contains our module
			dir := filepath.Base(filepath.Dir(path))
			if strings.Contains(dir, a.TaskID.Module) || strings.HasPrefix(dir, a.TaskID.Module) {
				foundPath = path
				return filepath.SkipAll
			}
		}
		return nil
	})

	if foundPath == "" {
		return "", fmt.Errorf("epic file not found for task %s", a.TaskID.String())
	}
	return foundPath, nil
}

// simpleFrontmatter is a minimal struct to extract just the status field
type simpleFrontmatter struct {
	Status string `yaml:"status"`
}

// parseSimpleFrontmatter extracts just the status from YAML frontmatter
func parseSimpleFrontmatter(content []byte) (*simpleFrontmatter, []byte, error) {
	if !strings.HasPrefix(string(content), "---\n") {
		return &simpleFrontmatter{}, content, nil
	}

	rest := content[4:]
	endIdx := strings.Index(string(rest), "\n---")
	if endIdx == -1 {
		return &simpleFrontmatter{}, content, nil
	}

	fmData := rest[:endIdx]
	remaining := rest[endIdx+4:]

	// Simple line-by-line parsing for status field
	var fm simpleFrontmatter
	for _, line := range strings.Split(string(fmData), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") {
			fm.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
			break
		}
	}

	return &fm, remaining, nil
}

// toStatus converts a string to a TaskStatus
func toStatus(s string) domain.TaskStatus {
	switch s {
	case "in_progress", "inprogress", "in-progress", "running":
		return domain.StatusInProgress
	case "complete", "completed", "done":
		return domain.StatusComplete
	default:
		return domain.StatusNotStarted
	}
}

// Resume restarts the agent by resuming its session
// This continues from where the previous session left off
func (a *Agent) Resume(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Can only resume completed or failed agents
	if a.Status != AgentCompleted && a.Status != AgentFailed {
		return fmt.Errorf("can only resume completed or failed agents, current status: %s", a.Status)
	}

	// Check epic status - if already complete, no need to resume
	a.mu.Unlock() // Temporarily unlock for file I/O
	epicStatus, err := a.CheckEpicStatus()
	a.mu.Lock() // Re-acquire lock
	if err == nil && epicStatus == domain.StatusComplete {
		return fmt.Errorf("task already complete (epic status: complete)")
	}

	// Must have a session ID to resume
	if a.SessionID == "" {
		// Generate deterministic UUID based on task ID
		a.SessionID = uuid.NewSHA1(orchestratorNamespace, []byte(a.TaskID.String())).String()
	}

	// Clear previous output to avoid mixing formats
	// (session file uses [assistant] format, stream uses raw JSON)
	a.Output = nil

	// Re-open or create log file (append mode)
	logPath := filepath.Join(a.WorktreePath, ".claude-agent.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	a.logFile = logFile
	a.LogPath = logPath

	// Add resume marker to log
	logFile.WriteString(fmt.Sprintf("\n=== Session resumed at %s ===\n", time.Now().Format(time.RFC3339)))

	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Build resume command based on executor type
	a.cmd = a.buildResumeCommand(ctx)

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
	execName := "claude"
	if a.ExecutorType == ExecutorOpenCode {
		execName = "opencode"
	}
	if err := a.cmd.Start(); err != nil {
		a.logFile.Close()
		return fmt.Errorf("starting %s: %w", execName, err)
	}

	// Update state
	a.PID = a.cmd.Process.Pid
	now := time.Now()
	a.StartedAt = &now
	a.FinishedAt = nil
	a.Status = AgentRunning
	a.Error = nil

	// Call status change callback for running status (triggers sync to in_progress)
	callback := a.OnStatusChange
	if callback != nil {
		go callback(a, AgentRunning, "")
	}

	// Stream output in background
	go a.streamOutput(stdout, stderr)

	return nil
}

// buildResumeCommand creates the appropriate resume command based on executor type
func (a *Agent) buildResumeCommand(ctx context.Context) *exec.Cmd {
	switch a.ExecutorType {
	case ExecutorOpenCode:
		return a.buildOpenCodeResumeCommand(ctx)
	default:
		return a.buildClaudeCodeResumeCommand(ctx)
	}
}

// buildClaudeCodeResumeCommand builds the resume command for Claude Code
func (a *Agent) buildClaudeCodeResumeCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "claude",
		"--print",                        // Non-interactive mode
		"--verbose",                      // Required for stream-json output
		"--dangerously-skip-permissions", // Skip permission prompts
		"--output-format", "stream-json", // Stream output as JSON for realtime updates
		"--resume", a.SessionID,          // Resume the named session
	)
	cmd.Dir = a.WorktreePath
	return cmd
}

// buildOpenCodeResumeCommand builds the resume command for OpenCode
func (a *Agent) buildOpenCodeResumeCommand(ctx context.Context) *exec.Cmd {
	// Note: --format json causes OpenCode to hang, so we use default format
	args := []string{
		"run", // Non-interactive mode
		"-c",  // Continue last session
	}

	// Add model if specified (e.g., "zai-coding-plan/glm-4.7")
	if a.OpenCodeModel != "" {
		args = append(args, "-m", a.OpenCodeModel)
	}

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = a.WorktreePath

	// Set up environment for MCP config
	cmd.Env = os.Environ()
	if mcpConfigPath := a.generateOpenCodeMCPConfig(); mcpConfigPath != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG="+mcpConfigPath)
	}

	return cmd
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

// generateMCPConfig creates an MCP config by merging the project's .mcp.json with orchestrator MCPs.
// Returns the config as a JSON string, or empty string if no MCPs are configured.
func (a *Agent) generateMCPConfig() string {
	mcpServers := make(map[string]interface{})

	// 1. Load project's .mcp.json if it exists
	projectConfigPath := filepath.Join(a.WorktreePath, ".mcp.json")
	if data, err := os.ReadFile(projectConfigPath); err == nil {
		var projectConfig struct {
			MCPServers map[string]interface{} `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &projectConfig); err == nil {
			for name, config := range projectConfig.MCPServers {
				mcpServers[name] = config
			}
		}
	}

	// 2. Add build-mcp if available (orchestrator's own MCP) and build pool URL is configured
	buildMCPPath := findBuildMCP()
	if buildMCPPath != "" && a.BuildPoolURL != "" {
		mcpServers["build-pool"] = map[string]interface{}{
			"command": buildMCPPath,
			"args":    []string{},
			"env": map[string]string{
				"BUILD_POOL_URL": a.BuildPoolURL,
			},
		}
	} else if buildMCPPath != "" && a.BuildPoolURL == "" {
		// Log warning: build-mcp found but no URL configured
		fmt.Fprintf(os.Stderr, "Warning: build-mcp found at %s but BuildPoolURL not set for agent %s\n", buildMCPPath, a.TaskID.String())
	}

	// Return empty if no MCPs configured
	if len(mcpServers) == 0 {
		return ""
	}

	config := map[string]interface{}{
		"mcpServers": mcpServers,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return ""
	}

	return string(configJSON)
}

// findBuildMCP locates the build-mcp binary by checking:
// 1. BUILD_MCP_PATH environment variable
// 2. Same directory as the current executable
// 3. System PATH
func findBuildMCP() string {
	// 1. Check environment variable
	if path := os.Getenv("BUILD_MCP_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 2. Check same directory as current executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		buildMCPPath := filepath.Join(exeDir, "build-mcp")
		if _, err := os.Stat(buildMCPPath); err == nil {
			return buildMCPPath
		}
	}

	// 3. Check system PATH
	if path, err := exec.LookPath("build-mcp"); err == nil {
		return path
	}

	return ""
}

// generateOpenCodeMCPConfig creates an MCP config file in OpenCode's format.
// Returns the path to the generated config file, or empty string if no MCPs are configured.
func (a *Agent) generateOpenCodeMCPConfig() string {
	mcpServers := make(map[string]interface{})

	// 1. Load project's .mcp.json if it exists (convert from Claude Code format)
	projectConfigPath := filepath.Join(a.WorktreePath, ".mcp.json")
	if data, err := os.ReadFile(projectConfigPath); err == nil {
		var projectConfig struct {
			MCPServers map[string]interface{} `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &projectConfig); err == nil {
			for name, configRaw := range projectConfig.MCPServers {
				// Convert Claude Code format to OpenCode format
				if configMap, ok := configRaw.(map[string]interface{}); ok {
					openCodeServer := map[string]interface{}{
						"type":    "local",
						"enabled": true,
					}

					// Convert command (string or []string) to []string
					if cmd, ok := configMap["command"].(string); ok {
						openCodeServer["command"] = []string{cmd}
					} else if cmdList, ok := configMap["command"].([]interface{}); ok {
						var cmdStrings []string
						for _, c := range cmdList {
							if s, ok := c.(string); ok {
								cmdStrings = append(cmdStrings, s)
							}
						}
						openCodeServer["command"] = cmdStrings
					}

					// Append args to command
					if args, ok := configMap["args"].([]interface{}); ok {
						if cmdList, ok := openCodeServer["command"].([]string); ok {
							for _, arg := range args {
								if s, ok := arg.(string); ok {
									cmdList = append(cmdList, s)
								}
							}
							openCodeServer["command"] = cmdList
						}
					}

					// Copy env to environment
					if env, ok := configMap["env"].(map[string]interface{}); ok {
						envMap := make(map[string]string)
						for k, v := range env {
							if s, ok := v.(string); ok {
								envMap[k] = s
							}
						}
						openCodeServer["environment"] = envMap
					}

					mcpServers[name] = openCodeServer
				}
			}
		}
	}

	// 2. Add build-mcp if available
	buildMCPPath := findBuildMCP()
	if buildMCPPath != "" && a.BuildPoolURL != "" {
		mcpServers["build-pool"] = map[string]interface{}{
			"type":    "local",
			"command": []string{buildMCPPath},
			"enabled": true,
			"environment": map[string]string{
				"BUILD_POOL_URL": a.BuildPoolURL,
			},
		}
	}

	// Build OpenCode config structure with permissions
	// Allow external_directory to prevent permission prompts for worktree paths
	config := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"permission": map[string]string{
			"external_directory": "allow",
		},
	}

	// Add MCP servers if any are configured
	if len(mcpServers) > 0 {
		config["mcp"] = mcpServers
	}

	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return ""
	}

	// Write to temp file in worktree
	configPath := filepath.Join(a.WorktreePath, ".opencode-mcp.json")
	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		return ""
	}

	return configPath
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

// LoadOutput loads agent output, preferring Claude session file for resumed agents
// This ensures we capture output even if TUI was closed while agent ran
func (a *Agent) LoadOutput(maxLines int) error {
	// For agents with a session ID, try Claude's session file first
	// This captures output from resumed sessions even if TUI wasn't running
	if a.SessionID != "" {
		sessionPath := a.GetClaudeSessionFilePath()
		if sessionPath != "" {
			if _, err := os.Stat(sessionPath); err == nil {
				// Session file exists, load from it
				return a.LoadOutputFromClaudeSession(maxLines)
			}
		}
	}

	// Fall back to our log file
	return a.LoadOutputFromLog(maxLines)
}

// GetClaudeSessionFilePath returns the path to Claude Code's session JSONL file
// Claude Code stores sessions at ~/.claude/projects/<encoded-path>/<session-id>.jsonl
func (a *Agent) GetClaudeSessionFilePath() string {
	if a.SessionID == "" || a.WorktreePath == "" {
		return ""
	}

	// Encode the worktree path: replace / with - and prefix with -
	encodedPath := "-" + strings.ReplaceAll(strings.TrimPrefix(a.WorktreePath, "/"), "/", "-")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, ".claude", "projects", encodedPath, a.SessionID+".jsonl")
}

// claudeSessionMessage represents a message in Claude Code's session JSONL
type claudeSessionMessage struct {
	Type      string `json:"type,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Message   struct {
		Role    string `json:"role,omitempty"`
		Content []struct {
			Type string `json:"type,omitempty"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
	} `json:"message,omitempty"`
}

// LoadOutputFromClaudeSession reads output from Claude Code's session file
// This captures output even if the TUI was closed while the agent ran
func (a *Agent) LoadOutputFromClaudeSession(maxMessages int) error {
	sessionPath := a.GetClaudeSessionFilePath()
	if sessionPath == "" {
		return nil // No session file path available
	}

	file, err := os.Open(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Session file doesn't exist
		}
		return err
	}
	defer file.Close()

	var messages []string
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024) // 2MB buffer for large JSON lines

	for scanner.Scan() {
		line := scanner.Text()
		var msg claudeSessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // Skip unparseable lines
		}

		// Extract assistant messages
		if msg.Message.Role == "assistant" {
			for _, content := range msg.Message.Content {
				if content.Type == "text" && content.Text != "" {
					// Format as a readable message
					messages = append(messages, fmt.Sprintf("[assistant] %s", content.Text))
				}
			}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Keep only last maxMessages
	if len(messages) > maxMessages {
		a.Output = messages[len(messages)-maxMessages:]
	} else {
		a.Output = messages
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
			m.queueDBOp(dbOp{
				opType:       "updateStatus",
				agentRunID:   run.ID,
				status:       "failed",
				errorMessage: "invalid task ID",
			})
			continue
		}

		agent := &Agent{
			ID:           run.ID,
			TaskID:       taskID,
			WorktreePath: run.WorktreePath,
			LogPath:      run.LogPath,
			PID:          run.PID,
			StartedAt:    &run.StartedAt,
			SessionID:    run.SessionID,
		}

		// Check if process is still running
		if agent.IsProcessRunning() {
			agent.Status = AgentRunning
			// Load recent output (prefers Claude session file for resumed agents)
			agent.LoadOutput(100)
			// Start tailing the log file
			agent.TailLogFile(ctx)
		} else {
			// Process is no longer running - check if it completed or failed
			// We'll mark it as completed since we can't know for sure
			agent.Status = AgentCompleted
			now := time.Now()
			agent.FinishedAt = &now
			// Load output (prefers Claude session file for resumed agents)
			agent.LoadOutput(100)
			// Update database via write queue
			m.queueDBOp(dbOp{
				opType:       "updateStatus",
				agentRunID:   run.ID,
				status:       "completed",
				errorMessage: "",
			})
		}

		// Add to manager
		m.mu.Lock()
		m.agents[run.TaskID] = agent
		m.mu.Unlock()

		recovered = append(recovered, agent)
	}

	return recovered, nil
}

// CreateStatusCallback returns a callback that updates the manager's store and syncs status
func (m *AgentManager) CreateStatusCallback() StatusChangeCallback {
	return func(agent *Agent, newStatus AgentStatus, errMsg string) {
		// Update agent_runs table in database via write queue
		if agent.ID != "" {
			m.queueDBOp(dbOp{
				opType:       "updateStatus",
				agentRunID:   agent.ID,
				status:       string(newStatus),
				errorMessage: errMsg,
			})
			// Save token usage when agent completes
			if newStatus == AgentCompleted || newStatus == AgentFailed {
				tokensIn, tokensOut, cost := agent.GetUsage()
				if tokensIn > 0 || tokensOut > 0 {
					m.queueDBOp(dbOp{
						opType:       "updateUsage",
						agentRunID:   agent.ID,
						tokensInput:  tokensIn,
						tokensOutput: tokensOut,
						costUSD:      cost,
					})
				}
			}
		}

		// Determine task status for sync
		var taskStatus domain.TaskStatus
		switch newStatus {
		case AgentCompleted:
			taskStatus = domain.StatusComplete
		case AgentRunning:
			taskStatus = domain.StatusInProgress
		default:
			// Don't sync for other statuses (failed, stuck, queued)
			return
		}

		// Update tasks table in database via write queue
		m.queueDBOp(dbOp{
			opType:     "updateTaskStatus",
			taskID:     agent.TaskID.String(),
			taskStatus: taskStatus,
		})

		// Sync epic and README status (atomic operation)
		if m.syncer != nil && agent.EpicFilePath != "" {
			// Atomically: pull, update files, commit, push
			if err := m.syncer.SyncTaskStatus(agent.TaskID, taskStatus, agent.EpicFilePath); err != nil {
				fmt.Printf("Warning: sync failed for %s: %v\n", agent.TaskID.String(), err)
			}
		}
	}
}
