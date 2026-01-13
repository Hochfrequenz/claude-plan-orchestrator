// internal/buildpool/mcp_server_test.go
package buildpool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPServer_ToolsList(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tools := server.ListTools()

	expectedTools := []string{"build", "clippy", "test", "run_command", "worker_status", "get_job_logs"}

	if len(tools) != len(expectedTools) {
		t.Errorf("got %d tools, want %d", len(tools), len(expectedTools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestMCPServer_BuildCommandArgs(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{
			name:     "default build",
			args:     nil,
			expected: "cargo build",
		},
		{
			name:     "release build",
			args:     map[string]interface{}{"release": true},
			expected: "cargo build --release",
		},
		{
			name:     "with features",
			args:     map[string]interface{}{"features": []interface{}{"foo", "bar"}},
			expected: "cargo build --features foo,bar",
		},
		{
			name:     "specific package",
			args:     map[string]interface{}{"package": "mylib"},
			expected: "cargo build -p mylib",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := server.buildCommand("build", tt.args)
			if cmd != tt.expected {
				t.Errorf("got %q, want %q", cmd, tt.expected)
			}
		})
	}
}

func TestMCPServer_ClippyCommandArgs(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{
			name:     "default clippy",
			args:     nil,
			expected: "cargo clippy --all-targets --all-features -- -D warnings",
		},
		{
			name:     "clippy with fix",
			args:     map[string]interface{}{"fix": true},
			expected: "cargo clippy --fix --all-targets --all-features",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := server.buildCommand("clippy", tt.args)
			if cmd != tt.expected {
				t.Errorf("got %q, want %q", cmd, tt.expected)
			}
		})
	}
}

func TestMCPServer_TestCommandArgs(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{
			name:     "default test",
			args:     nil,
			expected: "cargo test",
		},
		{
			name:     "test with filter",
			args:     map[string]interface{}{"filter": "my_test"},
			expected: "cargo test my_test",
		},
		{
			name:     "test with nocapture",
			args:     map[string]interface{}{"nocapture": true},
			expected: "cargo test -- --nocapture",
		},
		{
			name:     "test with package",
			args:     map[string]interface{}{"package": "mylib"},
			expected: "cargo test -p mylib",
		},
		{
			name:     "test with features",
			args:     map[string]interface{}{"features": []interface{}{"feature1"}},
			expected: "cargo test --features feature1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := server.buildCommand("test", tt.args)
			if cmd != tt.expected {
				t.Errorf("got %q, want %q", cmd, tt.expected)
			}
		})
	}
}

func TestMCPServer_HandleRequest_Initialize(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
	}

	resp := server.handleRequest(req)

	if resp["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", resp["jsonrpc"])
	}

	if resp["id"] != float64(1) {
		t.Errorf("expected id 1, got %v", resp["id"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result to be a map")
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocol version 2024-11-05, got %v", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected serverInfo to be a map")
	}

	if serverInfo["name"] != "build-pool" {
		t.Errorf("expected server name build-pool, got %v", serverInfo["name"])
	}
}

func TestMCPServer_HandleRequest_ToolsList(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(2),
		"method":  "tools/list",
	}

	resp := server.handleRequest(req)

	if resp["id"] != float64(2) {
		t.Errorf("expected id 2, got %v", resp["id"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result to be a map")
	}

	tools, ok := result["tools"].([]MCPTool)
	if !ok {
		t.Fatalf("expected tools to be []MCPTool")
	}

	if len(tools) != 6 {
		t.Errorf("expected 6 tools, got %d", len(tools))
	}
}

func TestMCPServer_HandleRequest_UnknownMethod(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(3),
		"method":  "unknown/method",
	}

	resp := server.handleRequest(req)

	errResp, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error response")
	}

	if errResp["code"] != -32601 {
		t.Errorf("expected error code -32601, got %v", errResp["code"])
	}
}

func TestMCPServer_WorkerStatus(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	result, err := server.workerStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	// Verify output is valid JSON
	var status map[string]interface{}
	if err := json.Unmarshal([]byte(result.Output), &status); err != nil {
		t.Errorf("output should be valid JSON: %v", err)
	}

	if _, ok := status["workers"]; !ok {
		t.Error("expected workers field in status")
	}
}

func TestMCPServer_CallTool_NoDispatcher(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	_, err := server.CallTool("build", nil)
	if err == nil {
		t.Error("expected error when no dispatcher configured")
	}

	if err.Error() != "no dispatcher configured" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMCPServer_CallTool_UnknownTool(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	_, err := server.CallTool("unknown_tool", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}

	if err.Error() != "unknown tool: unknown_tool" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMCPServer_CallTool_RunCommandMissingArg(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	// Test with nil args
	_, err := server.CallTool("run_command", nil)
	if err == nil {
		t.Error("expected error for run_command without command arg")
	}
	if err.Error() != "run_command requires 'command' argument" {
		t.Errorf("unexpected error: %v", err)
	}

	// Test with empty args
	_, err = server.CallTool("run_command", map[string]interface{}{})
	if err == nil {
		t.Error("expected error for run_command with empty args")
	}
	if err.Error() != "run_command requires 'command' argument" {
		t.Errorf("unexpected error: %v", err)
	}

	// Test with empty command string
	_, err = server.CallTool("run_command", map[string]interface{}{"command": ""})
	if err == nil {
		t.Error("expected error for run_command with empty command")
	}
	if err.Error() != "run_command requires 'command' argument" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMCPServer_BuildCommand_InvalidFeatures(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	// Test with non-string feature elements - should not panic
	args := map[string]interface{}{
		"features": []interface{}{123, "valid", nil, true},
	}
	cmd := server.buildCommand("build", args)
	// Should only include the valid string feature
	if cmd != "cargo build --features valid" {
		t.Errorf("got %q, want %q", cmd, "cargo build --features valid")
	}

	// Test with all invalid features - should return base command
	args = map[string]interface{}{
		"features": []interface{}{123, nil},
	}
	cmd = server.buildCommand("build", args)
	if cmd != "cargo build" {
		t.Errorf("got %q, want %q", cmd, "cargo build")
	}
}

func TestMCPServer_ToolSchema(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tools := server.ListTools()

	// Verify each tool has required fields
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool should have a name")
		}
		if tool.Description == "" {
			t.Errorf("tool %s should have a description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s should have an input schema", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("tool %s schema type should be object", tool.Name)
		}
	}
}

func TestMCPServer_WorkerStatus_ReturnsRealData(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)

	// Register a worker
	registry.Register(&ConnectedWorker{
		ID:      "worker-1",
		MaxJobs: 4,
		Slots:   3,
	})

	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)

	result, err := server.CallTool("worker_status", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !strings.Contains(result.Output, "worker-1") {
		t.Errorf("output missing worker-1: %s", result.Output)
	}
	if !strings.Contains(result.Output, `"max_jobs": 4`) {
		t.Errorf("output missing max_jobs: %s", result.Output)
	}
}

func TestMCPServer_GetJobLogsTool(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tools := server.ListTools()

	var found bool
	for _, tool := range tools {
		if tool.Name == "get_job_logs" {
			found = true
			props := tool.InputSchema["properties"].(map[string]interface{})
			if _, ok := props["job_id"]; !ok {
				t.Error("get_job_logs missing job_id property")
			}
			if _, ok := props["stream"]; !ok {
				t.Error("get_job_logs missing stream property")
			}
			required := tool.InputSchema["required"].([]string)
			if len(required) != 1 || required[0] != "job_id" {
				t.Errorf("get_job_logs required = %v, want [job_id]", required)
			}
			break
		}
	}

	if !found {
		t.Error("get_job_logs tool not found")
	}
}

func TestMCPServer_GetJobLogsExecution(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	// Simulate a completed job with retained logs
	coord.AccumulateOutput("test-job-logs", "stdout", "stdout content")
	coord.AccumulateOutput("test-job-logs", "stderr", "stderr content")
	coord.RetainLogs("test-job-logs")

	// Call get_job_logs
	result, err := server.CallTool("get_job_logs", map[string]interface{}{
		"job_id": "test-job-logs",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !strings.Contains(result.Output, "stdout content") {
		t.Errorf("output missing stdout: %s", result.Output)
	}
	if !strings.Contains(result.Output, "stderr content") {
		t.Errorf("output missing stderr: %s", result.Output)
	}
}

func TestMCPServer_GetJobLogsNotFound(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	result, err := server.CallTool("get_job_logs", map[string]interface{}{
		"job_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1 for not found", result.ExitCode)
	}
}

func TestMCPServer_ToolsHaveVerbosityParam(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tools := server.ListTools()

	toolsWithVerbosity := []string{"build", "clippy", "test", "run_command"}

	for _, tool := range tools {
		shouldHave := false
		for _, name := range toolsWithVerbosity {
			if tool.Name == name {
				shouldHave = true
				break
			}
		}

		if !shouldHave {
			continue
		}

		props, ok := tool.InputSchema["properties"].(map[string]interface{})
		if !ok {
			t.Errorf("tool %s: properties not found", tool.Name)
			continue
		}

		verbosity, ok := props["verbosity"].(map[string]interface{})
		if !ok {
			t.Errorf("tool %s: missing verbosity property", tool.Name)
			continue
		}

		enum, ok := verbosity["enum"].([]string)
		if !ok {
			t.Errorf("tool %s: verbosity missing enum", tool.Name)
			continue
		}

		expected := []string{"minimal", "normal", "full"}
		if len(enum) != len(expected) {
			t.Errorf("tool %s: verbosity enum = %v, want %v", tool.Name, enum, expected)
		}
	}
}
