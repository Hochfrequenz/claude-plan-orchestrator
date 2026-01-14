// internal/buildpool/mcp_server.go
package buildpool

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// MCPServerConfig configures the MCP server
type MCPServerConfig struct {
	WorktreePath string
	GitDaemonURL string // Git daemon URL for remote workers (e.g., "git://host:9418/")
}

// MCPServer implements the MCP protocol for build tools
type MCPServer struct {
	config      MCPServerConfig
	dispatcher  *Dispatcher
	registry    *Registry
	coordinator *Coordinator
	repoURL     string // Remote URL (for remote workers)
	localRepo   string // Local worktree path (for embedded worker)
	commit      string
}

// MCPTool describes an available tool
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// verbositySchema defines the verbosity parameter for MCP tool schemas
var verbositySchema = map[string]interface{}{
	"type":        "string",
	"description": "Output verbosity level: minimal (errors only), normal (default), full (all output)",
	"enum":        []string{"minimal", "normal", "full"},
	"default":     "normal",
}

// NewMCPServer creates a new MCP server
func NewMCPServer(config MCPServerConfig, dispatcher *Dispatcher, registry *Registry) *MCPServer {
	s := &MCPServer{
		config:     config,
		dispatcher: dispatcher,
		registry:   registry,
	}

	// Get repo URL and commit from worktree
	s.loadRepoInfo()

	// Set local repo path on dispatcher for embedded worker
	// This allows embedded worker to use local path instead of remote URL
	if dispatcher != nil && s.localRepo != "" {
		dispatcher.SetLocalRepoPath(s.localRepo)
	}

	return s
}

// SetCoordinator sets the coordinator for log retrieval
func (s *MCPServer) SetCoordinator(c *Coordinator) {
	s.coordinator = c
}

func randomJobSuffix() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *MCPServer) loadRepoInfo() {
	// Get current commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = s.config.WorktreePath
	if out, err := cmd.Output(); err == nil {
		s.commit = strings.TrimSpace(string(out))
	}

	// Get remote URL (for remote workers if ever connected)
	cmd = exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = s.config.WorktreePath
	if out, err := cmd.Output(); err == nil {
		s.repoURL = strings.TrimSpace(string(out))
	}

	// Store local repo path (for embedded worker - avoids fetch for unpushed commits)
	s.localRepo = s.config.WorktreePath
}

// ListTools returns available tools
func (s *MCPServer) ListTools() []MCPTool {
	return []MCPTool{
		{
			Name:        "build",
			Description: "Build the Rust project with cargo",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release":   map[string]interface{}{"type": "boolean", "description": "Build in release mode"},
					"features":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"package":   map[string]interface{}{"type": "string", "description": "Specific package to build"},
					"verbosity": verbositySchema,
				},
			},
		},
		{
			Name:        "clippy",
			Description: "Run clippy lints",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fix":       map[string]interface{}{"type": "boolean", "description": "Apply suggested fixes"},
					"features":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"verbosity": verbositySchema,
				},
			},
		},
		{
			Name:        "test",
			Description: "Run tests",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filter":    map[string]interface{}{"type": "string", "description": "Test name filter"},
					"package":   map[string]interface{}{"type": "string", "description": "Specific package to test"},
					"features":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"nocapture": map[string]interface{}{"type": "boolean", "description": "Show stdout/stderr"},
					"verbosity": verbositySchema,
				},
			},
		},
		{
			Name:        "run_command",
			Description: "Run an arbitrary shell command",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":      map[string]interface{}{"type": "string", "description": "Command to run"},
					"timeout_secs": map[string]interface{}{"type": "integer", "description": "Timeout in seconds"},
					"verbosity":    verbositySchema,
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "worker_status",
			Description: "Get status of connected workers",
			InputSchema: map[string]interface{}{
				"type": "object",
			},
		},
		{
			Name:        "get_job_logs",
			Description: "Retrieve complete logs for a completed job from retention buffer",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job_id": map[string]interface{}{
						"type":        "string",
						"description": "The job ID to retrieve logs for",
					},
					"stream": map[string]interface{}{
						"type":        "string",
						"description": "Which stream to retrieve: stdout, stderr, or both (default: both)",
						"enum":        []string{"stdout", "stderr", "both"},
						"default":     "both",
					},
				},
				"required": []string{"job_id"},
			},
		},
	}
}

func (s *MCPServer) buildCommand(tool string, args map[string]interface{}) string {
	if args == nil {
		args = make(map[string]interface{})
	}

	var parts []string

	switch tool {
	case "build":
		parts = []string{"cargo", "build"}
		if release, ok := args["release"].(bool); ok && release {
			parts = append(parts, "--release")
		}
	case "clippy":
		parts = []string{"cargo", "clippy", "--all-targets", "--all-features", "--", "-D", "warnings"}
		if fix, ok := args["fix"].(bool); ok && fix {
			parts = []string{"cargo", "clippy", "--fix", "--all-targets", "--all-features"}
		}
	case "test":
		parts = []string{"cargo", "test"}
		if filter, ok := args["filter"].(string); ok && filter != "" {
			parts = append(parts, filter)
		}
		if nocapture, ok := args["nocapture"].(bool); ok && nocapture {
			parts = append(parts, "--", "--nocapture")
		}
	}

	// Common args - features
	if features, ok := args["features"].([]interface{}); ok && len(features) > 0 {
		featureStrs := make([]string, 0, len(features))
		for _, f := range features {
			if s, ok := f.(string); ok {
				featureStrs = append(featureStrs, s)
			}
		}
		if len(featureStrs) == 0 {
			return strings.Join(parts, " ")
		}
		// Insert before any -- args
		insertPos := len(parts)
		for i, p := range parts {
			if p == "--" {
				insertPos = i
				break
			}
		}
		newParts := make([]string, 0, len(parts)+2)
		newParts = append(newParts, parts[:insertPos]...)
		newParts = append(newParts, "--features", strings.Join(featureStrs, ","))
		newParts = append(newParts, parts[insertPos:]...)
		parts = newParts
	}

	// Common args - package
	if pkg, ok := args["package"].(string); ok && pkg != "" {
		// Insert before any -- args
		insertPos := len(parts)
		for i, p := range parts {
			if p == "--" {
				insertPos = i
				break
			}
		}
		newParts := make([]string, 0, len(parts)+2)
		newParts = append(newParts, parts[:insertPos]...)
		newParts = append(newParts, "-p", pkg)
		newParts = append(newParts, parts[insertPos:]...)
		parts = newParts
	}

	return strings.Join(parts, " ")
}

// CallTool executes a tool and returns the result
func (s *MCPServer) CallTool(name string, args map[string]interface{}) (*buildprotocol.JobResult, error) {
	var command string
	var timeout int

	switch name {
	case "build", "clippy", "test":
		command = s.buildCommand(name, args)
	case "run_command":
		cmd, ok := args["command"].(string)
		if !ok || cmd == "" {
			return nil, fmt.Errorf("run_command requires 'command' argument")
		}
		command = cmd
		if t, ok := args["timeout_secs"].(float64); ok {
			timeout = int(t)
		}
	case "worker_status":
		// Return worker status without dispatching a job
		return s.workerStatus()
	case "get_job_logs":
		// Retrieve logs from retention buffer
		return s.getJobLogs(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	// Extract verbosity from args, default to minimal
	verbosity := buildprotocol.VerbosityMinimal
	if v, ok := args["verbosity"].(string); ok && v != "" {
		verbosity = v
	}

	// Create job
	// Use git daemon URL for remote workers (faster, local network)
	// Fall back to remote URL if git daemon not configured
	// The embedded worker will substitute local path via dispatcher
	repoURL := s.repoURL
	if s.config.GitDaemonURL != "" {
		repoURL = s.config.GitDaemonURL
	}

	jobID := fmt.Sprintf("mcp-%s", randomJobSuffix())
	job := &buildprotocol.JobMessage{
		JobID:   jobID,
		Repo:    repoURL,
		Commit:  s.commit,
		Command: command,
		Timeout: timeout,
	}

	// Submit to dispatcher with verbosity
	if s.dispatcher == nil {
		return nil, fmt.Errorf("no dispatcher configured")
	}

	resultCh := s.dispatcher.SubmitWithVerbosity(job, verbosity)
	s.dispatcher.TryDispatch()

	// Wait for result
	result := <-resultCh

	// Parse output for test results
	if name == "test" {
		result.ParseTestOutput()
	}

	return result, nil
}

func (s *MCPServer) workerStatus() (*buildprotocol.JobResult, error) {
	workers := []map[string]interface{}{}

	if s.registry != nil {
		for _, w := range s.registry.All() {
			maxJobs, slots, connectedAt := w.GetStatus()
			workers = append(workers, map[string]interface{}{
				"id":              w.ID,
				"max_jobs":        maxJobs,
				"active_jobs":     maxJobs - slots,
				"connected_since": connectedAt.Format(time.RFC3339),
			})
		}
	}

	queuedJobs := 0
	if s.dispatcher != nil {
		queuedJobs = s.dispatcher.QueueLength()
	}

	status := map[string]interface{}{
		"workers":               workers,
		"queued_jobs":           queuedJobs,
		"local_fallback_active": s.registry == nil || s.registry.Count() == 0,
	}

	output, _ := json.MarshalIndent(status, "", "  ")

	return &buildprotocol.JobResult{
		JobID:    "status",
		ExitCode: 0,
		Output:   string(output),
	}, nil
}

func (s *MCPServer) getJobLogs(args map[string]interface{}) (*buildprotocol.JobResult, error) {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return &buildprotocol.JobResult{
			ExitCode: 1,
			Output:   "job_id is required",
		}, nil
	}

	if s.coordinator == nil {
		return &buildprotocol.JobResult{
			ExitCode: 1,
			Output:   "no coordinator configured for log retrieval",
		}, nil
	}

	stdout, stderr, found := s.coordinator.GetRetainedLogs(jobID)
	if !found {
		return &buildprotocol.JobResult{
			ExitCode: 1,
			Output:   fmt.Sprintf("logs not found for job %s", jobID),
		}, nil
	}

	// Filter by stream if specified
	stream, _ := args["stream"].(string)
	if stream == "" {
		stream = "both"
	}

	var output string
	switch stream {
	case "stdout":
		output = stdout
	case "stderr":
		output = stderr
	default: // "both"
		result := map[string]string{
			"stdout": stdout,
			"stderr": stderr,
		}
		outputBytes, _ := json.MarshalIndent(result, "", "  ")
		output = string(outputBytes)
	}

	return &buildprotocol.JobResult{
		JobID:    jobID,
		ExitCode: 0,
		Output:   output,
	}, nil
}

// Run starts the MCP server on stdin/stdout
func (s *MCPServer) Run() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		var request map[string]interface{}
		if err := json.Unmarshal(line, &request); err != nil {
			continue
		}

		response := s.handleRequest(request)

		respBytes, _ := json.Marshal(response)
		fmt.Println(string(respBytes))
	}
}

func (s *MCPServer) handleRequest(req map[string]interface{}) map[string]interface{} {
	method, _ := req["method"].(string)
	id, _ := req["id"].(float64)

	switch method {
	case "initialize":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "build-pool",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"tools": s.ListTools(),
			},
		}

	case "tools/call":
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})

		result, err := s.CallTool(name, args)
		if err != nil {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]interface{}{
					"code":    -32000,
					"message": err.Error(),
				},
			}
		}

		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": result.Output},
				},
			},
		}

	default:
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": "method not found",
			},
		}
	}
}
