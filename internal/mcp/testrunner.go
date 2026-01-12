package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TestRunner wraps an MCP client for the erp-test-runner server
type TestRunner struct {
	client *Client
}

// TestRunResult represents the result of a test run
type TestRunResult struct {
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// TestResults represents the results of get_test_results
type TestResults struct {
	RunID          string       `json:"run_id"`
	Status         string       `json:"status"`
	ExitCode       *int         `json:"exit_code,omitempty"`
	Error          string       `json:"error,omitempty"`
	Filter         string       `json:"filter,omitempty"`
	ElapsedSeconds float64      `json:"elapsed_seconds"`
	Summary        *TestSummary `json:"summary,omitempty"`
	FailureCount   int          `json:"failure_count,omitempty"`
	Hint           string       `json:"hint,omitempty"`
	Output         string       `json:"output,omitempty"`
	OutputLineCount int         `json:"output_line_count"`
}

// TestSummary contains test result summary
type TestSummary struct {
	Total   int  `json:"total"`
	Passed  int  `json:"passed"`
	Failed  int  `json:"failed"`
	Ignored int  `json:"ignored"`
	Skipped int  `json:"skipped"`
	Success bool `json:"success"`
}

// MCPConfig contains the configuration for an MCP server from .mcp.json
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPServerConfig is the configuration for a single MCP server
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// LoadMCPConfig loads the MCP configuration from a project's .mcp.json file
func LoadMCPConfig(projectRoot string) (*MCPConfig, error) {
	configPath := filepath.Join(projectRoot, ".mcp.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read .mcp.json: %w", err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse .mcp.json: %w", err)
	}

	return &config, nil
}

// NewTestRunner creates a new test runner from the project's MCP config
func NewTestRunner(projectRoot string) (*TestRunner, error) {
	config, err := LoadMCPConfig(projectRoot)
	if err != nil {
		return nil, err
	}

	serverConfig, ok := config.MCPServers["erp-test-runner"]
	if !ok {
		return nil, fmt.Errorf("erp-test-runner not found in .mcp.json")
	}

	// Convert env map to slice
	var envSlice []string
	for k, v := range serverConfig.Env {
		// Expand home directory in values
		if len(v) > 0 && v[0] == '~' {
			home, _ := os.UserHomeDir()
			v = home + v[1:]
		}
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}

	// Inherit current environment
	envSlice = append(os.Environ(), envSlice...)

	client, err := NewClient(serverConfig.Command, serverConfig.Args, envSlice)
	if err != nil {
		return nil, err
	}

	return &TestRunner{client: client}, nil
}

// SyncTests builds and syncs test binaries to the remote server
func (t *TestRunner) SyncTests(buildFirst, release bool) (string, error) {
	args := map[string]interface{}{
		"build_first": buildFirst,
		"release":     release,
	}

	result, err := t.client.CallTool("sync_tests", args)
	if err != nil {
		return "", err
	}

	if result.IsError {
		return "", fmt.Errorf("sync_tests failed: %s", t.extractText(result))
	}

	return t.extractText(result), nil
}

// RunTests starts a test run and returns the run ID
func (t *TestRunner) RunTests(filter string, syncFirst, includeIgnored bool) (*TestRunResult, error) {
	args := map[string]interface{}{
		"sync_first":      syncFirst,
		"include_ignored": includeIgnored,
	}
	if filter != "" {
		args["filter"] = filter
	}

	result, err := t.client.CallTool("run_tests", args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("run_tests failed: %s", t.extractText(result))
	}

	// Parse the JSON result
	text := t.extractText(result)
	var runResult TestRunResult
	if err := json.Unmarshal([]byte(text), &runResult); err != nil {
		// Return text as message if not JSON
		return &TestRunResult{Message: text}, nil
	}

	return &runResult, nil
}

// GetTestResults gets the results of a test run
func (t *TestRunner) GetTestResults(runID string, blocking bool, tail int) (*TestResults, error) {
	args := map[string]interface{}{
		"run_id":   runID,
		"blocking": blocking,
	}
	if tail > 0 {
		args["tail"] = tail
	}

	result, err := t.client.CallTool("get_test_results", args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("get_test_results failed: %s", t.extractText(result))
	}

	text := t.extractText(result)
	var results TestResults
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		return nil, fmt.Errorf("failed to parse test results: %w", err)
	}

	return &results, nil
}

// Close closes the test runner connection
func (t *TestRunner) Close() error {
	return t.client.Close()
}

// extractText extracts text from tool result content
func (t *TestRunner) extractText(result *ToolResult) string {
	for _, content := range result.Content {
		if content.Type == "text" {
			return content.Text
		}
	}
	return ""
}
