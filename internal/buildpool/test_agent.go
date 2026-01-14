// internal/buildpool/test_agent.go
package buildpool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TestAgentConfig configures the test agent
type TestAgentConfig struct {
	BuildPoolURL string // URL of the build pool coordinator
	ProjectRoot  string // Path to the project root
	MCPBinary    string // Path to the build-mcp binary (optional, will search PATH)
	Verbose      bool   // Show verbose output
}

// TestAgentResult contains the results of the test
type TestAgentResult struct {
	Success bool
	Output  string
	Error   string
}

// TestAgentOutputCallback is called for each line of output from the test agent
type TestAgentOutputCallback func(line string)

// TestPrompt is the prompt given to the Claude agent to test the MCP tools
const TestPrompt = `You are testing the build pool MCP tools. Your job is to call each tool and verify it works.

Please run the following tests IN ORDER and report the results:

1. **worker_status**: Call the worker_status tool to see available workers
   - Report: number of workers, whether local fallback is active

2. **run_command (success)**: Call run_command with command "echo 'Hello from build pool test'"
   - Report: exit code (should be 0), output should contain "Hello from build pool test"

3. **run_command (failure)**: Call run_command with command "exit 42"
   - Report: exit code (should be 42), verify error is reported properly

4. **run_command (stderr)**: Call run_command with command "echo 'stderr test' >&2 && exit 1"
   - Report: exit code (should be 1), verify stderr is captured

5. **run_command (bad command)**: Call run_command with command "/nonexistent_command_12345"
   - Report: exit code (should be non-zero), verify error message is shown

After running all tests, provide a summary:
- PASS/FAIL status for each test
- Any issues or observations
- Overall verdict: PASS if all tests work, FAIL if any critical issues

Be concise. Just run the tests and report results.`

// RunTestAgent runs a Claude agent to test the build pool MCP tools
func RunTestAgent(ctx context.Context, config TestAgentConfig, onOutput TestAgentOutputCallback) (*TestAgentResult, error) {
	// Find or create MCP config
	mcpConfig, cleanup, err := createTestMCPConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating MCP config: %w", err)
	}
	defer cleanup()

	// Build claude command
	args := []string{
		"--print",
		"--dangerously-skip-permissions",
		"--mcp-config", mcpConfig,
	}

	if config.Verbose {
		args = append(args, "--verbose", "--output-format", "stream-json")
	}

	args = append(args, "-p", TestPrompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = config.ProjectRoot

	// Capture output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	// Collect output
	var outputLines []string
	outputCh := make(chan string, 100)
	doneCh := make(chan struct{})

	// Stream stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			outputCh <- line
		}
	}()

	// Stream stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			outputCh <- "[stderr] " + line
		}
	}()

	// Collect and forward output
	go func() {
		for line := range outputCh {
			outputLines = append(outputLines, line)
			if onOutput != nil {
				// For stream-json, extract text content
				if config.Verbose {
					text := extractTextFromStreamJSON(line)
					if text != "" {
						onOutput(text)
					}
				} else {
					onOutput(line)
				}
			}
		}
		close(doneCh)
	}()

	// Wait for completion
	err = cmd.Wait()
	close(outputCh)
	<-doneCh

	result := &TestAgentResult{
		Output: strings.Join(outputLines, "\n"),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}

	return result, nil
}

// extractTextFromStreamJSON extracts text content from stream-json output
func extractTextFromStreamJSON(line string) string {
	// Try to parse as JSON
	var msg struct {
		Type    string `json:"type"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}

	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return "" // Not JSON, skip
	}

	// Handle assistant message content
	if msg.Type == "content_block_delta" && msg.Content.Type == "text_delta" {
		return msg.Content.Text
	}

	// Handle result message
	if msg.Type == "result" {
		for _, c := range msg.Message.Content {
			if c.Type == "text" {
				return c.Text
			}
		}
	}

	return ""
}

// createTestMCPConfig creates a temporary MCP config for the test agent
func createTestMCPConfig(config TestAgentConfig) (string, func(), error) {
	// Find build-mcp binary
	mcpBinary := config.MCPBinary
	if mcpBinary == "" {
		// Look in common locations
		candidates := []string{
			filepath.Join(config.ProjectRoot, "build-mcp"),
			filepath.Join(filepath.Dir(os.Args[0]), "build-mcp"),
		}

		// Also check PATH
		if path, err := exec.LookPath("build-mcp"); err == nil {
			candidates = append([]string{path}, candidates...)
		}

		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				mcpBinary = c
				break
			}
		}

		if mcpBinary == "" {
			return "", nil, fmt.Errorf("build-mcp binary not found")
		}
	}

	// Create temp config file
	tmpFile, err := os.CreateTemp("", "mcp-test-config-*.json")
	if err != nil {
		return "", nil, err
	}

	// Build config
	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"build-pool": map[string]interface{}{
				"command": mcpBinary,
				"args":    []string{},
				"env": map[string]string{
					"BUILD_POOL_URL": config.BuildPoolURL,
					"PROJECT_ROOT":   config.ProjectRoot,
				},
			},
		},
	}

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(mcpConfig); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", nil, err
	}
	tmpFile.Close()

	cleanup := func() {
		os.Remove(tmpFile.Name())
	}

	return tmpFile.Name(), cleanup, nil
}

// RunTestAgentWithEmbeddedCoordinator starts a temporary coordinator with embedded worker,
// runs the agent test, then shuts down the coordinator. Use this when no external coordinator
// is available.
func RunTestAgentWithEmbeddedCoordinator(ctx context.Context, projectRoot string, verbose bool, onOutput TestAgentOutputCallback) (*TestAgentResult, error) {
	// Create worktree directory for embedded worker
	worktreeDir, err := os.MkdirTemp("", "agent-test-worktrees-")
	if err != nil {
		return nil, fmt.Errorf("creating temp worktree dir: %w", err)
	}
	defer os.RemoveAll(worktreeDir)

	// Set up the full stack
	registry := NewRegistry()

	// Create real embedded worker
	embedded := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     projectRoot,
		WorktreeDir: worktreeDir,
		MaxJobs:     2,
		UseNixShell: false, // Don't require nix for tests
	})

	// Create dispatcher with embedded worker
	dispatcher := NewDispatcher(registry, embedded.Run)
	dispatcher.SetLocalRepoPath(projectRoot)

	// Create coordinator
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Create HTTP handler with coordinator endpoints
	mux := createCoordinatorMux(coord)

	// Start test server (automatically handles port allocation and cleanup)
	server := httptest.NewServer(mux)
	defer server.Close()

	buildPoolURL := server.URL

	// Now run the agent test against this coordinator
	config := TestAgentConfig{
		BuildPoolURL: buildPoolURL,
		ProjectRoot:  projectRoot,
		Verbose:      verbose,
	}

	return RunTestAgent(ctx, config, onOutput)
}

// createCoordinatorMux creates an HTTP handler with all coordinator endpoints
func createCoordinatorMux(coord *Coordinator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", coord.HandleWebSocket)
	mux.HandleFunc("/status", coord.HandleStatus)
	mux.HandleFunc("/job", coord.HandleJobSubmit)
	mux.HandleFunc("/logs/", coord.HandleGetLogs)
	return mux
}

// QuickTest runs a quick test without a full agent - just HTTP calls
func QuickTest(ctx context.Context, buildPoolURL string) (*TestAgentResult, error) {
	// This is a simpler test that doesn't require Claude
	// It just makes HTTP calls to verify the coordinator is responding

	client := &httpClient{timeout: 30 * time.Second}
	var results []string

	// Test 1: Status endpoint
	resp, err := client.get(buildPoolURL + "/status")
	if err != nil {
		return &TestAgentResult{
			Success: false,
			Error:   fmt.Sprintf("status check failed: %v", err),
		}, nil
	}
	results = append(results, fmt.Sprintf("Status: OK (%d bytes)", len(resp)))

	// Test 2: Submit a simple job
	jobReq := map[string]interface{}{
		"command": "echo 'quick test'",
		"timeout": 30,
	}
	reqBody, _ := json.Marshal(jobReq)
	resp, err = client.post(buildPoolURL+"/job", "application/json", string(reqBody))
	if err != nil {
		return &TestAgentResult{
			Success: false,
			Error:   fmt.Sprintf("job submission failed: %v", err),
		}, nil
	}

	var jobResp struct {
		ExitCode int    `json:"exit_code"`
		Output   string `json:"output"`
	}
	if err := json.Unmarshal([]byte(resp), &jobResp); err != nil {
		return &TestAgentResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse job response: %v", err),
		}, nil
	}

	if jobResp.ExitCode != 0 {
		results = append(results, fmt.Sprintf("Job: FAILED (exit=%d, output=%q)", jobResp.ExitCode, jobResp.Output))
	} else {
		results = append(results, fmt.Sprintf("Job: OK (exit=%d)", jobResp.ExitCode))
	}

	return &TestAgentResult{
		Success: jobResp.ExitCode == 0,
		Output:  strings.Join(results, "\n"),
	}, nil
}

// Simple HTTP client wrapper
type httpClient struct {
	timeout time.Duration
}

func (c *httpClient) get(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	req, err := exec.CommandContext(ctx, "curl", "-s", url).Output()
	if err != nil {
		return "", err
	}
	return string(req), nil
}

func (c *httpClient) post(url, contentType, body string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	req, err := exec.CommandContext(ctx, "curl", "-s", "-X", "POST",
		"-H", "Content-Type: "+contentType,
		"-d", body, url).Output()
	if err != nil {
		return "", err
	}
	return string(req), nil
}
