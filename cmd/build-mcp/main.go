// cmd/build-mcp/main.go
// MCP server that forwards build requests to the coordinator via HTTP
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

var coordinatorURL = "http://localhost:8081"

func main() {
	// Check for coordinator URL override
	if url := os.Getenv("BUILD_POOL_URL"); url != "" {
		coordinatorURL = url
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			return
		}

		var request map[string]interface{}
		if err := json.Unmarshal(line, &request); err != nil {
			continue
		}

		response := handleRequest(request)

		respBytes, _ := json.Marshal(response)
		fmt.Println(string(respBytes))
	}
}

func handleRequest(req map[string]interface{}) map[string]interface{} {
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
				"tools": listTools(),
			},
		}

	case "tools/call":
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})

		result, err := callTool(name, args)
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
					{"type": "text", "text": result},
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

func listTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "build",
			"description": "Build the Rust project with cargo (offloaded to build pool)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release":  map[string]interface{}{"type": "boolean", "description": "Build in release mode"},
					"package":  map[string]interface{}{"type": "string", "description": "Specific package to build"},
					"features": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				},
			},
		},
		{
			"name":        "test",
			"description": "Run tests (offloaded to build pool)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filter":    map[string]interface{}{"type": "string", "description": "Test name filter"},
					"package":   map[string]interface{}{"type": "string", "description": "Specific package to test"},
					"nocapture": map[string]interface{}{"type": "boolean", "description": "Show stdout/stderr"},
				},
			},
		},
		{
			"name":        "clippy",
			"description": "Run clippy lints (offloaded to build pool)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fix": map[string]interface{}{"type": "boolean", "description": "Apply suggested fixes"},
				},
			},
		},
		{
			"name":        "worker_status",
			"description": "Get status of connected build workers",
			"inputSchema": map[string]interface{}{
				"type": "object",
			},
		},
	}
}

func buildCommand(tool string, args map[string]interface{}) string {
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

	// Handle package arg
	if pkg, ok := args["package"].(string); ok && pkg != "" {
		// Insert -p before any -- args
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

func callTool(name string, args map[string]interface{}) (string, error) {
	switch name {
	case "worker_status":
		return getWorkerStatus()
	case "build", "test", "clippy":
		command := buildCommand(name, args)
		return submitJob(command)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func getWorkerStatus() (string, error) {
	resp, err := http.Get(coordinatorURL + "/status")
	if err != nil {
		return "", fmt.Errorf("failed to connect to build pool: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Pretty-print the JSON
	var data interface{}
	json.Unmarshal(body, &data)
	pretty, _ := json.MarshalIndent(data, "", "  ")
	return string(pretty), nil
}

func submitJob(command string) (string, error) {
	// Get repo info from git
	repo, commit := getGitInfo()

	reqBody := map[string]interface{}{
		"command": command,
		"repo":    repo,
		"commit":  commit,
		"timeout": 300, // 5 minute default
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(coordinatorURL+"/job", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to connect to build pool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("build pool error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		JobID    string `json:"job_id"`
		ExitCode int    `json:"exit_code"`
		Output   string `json:"output"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	if result.Error != "" {
		return fmt.Sprintf("Error: %s\n\nOutput:\n%s", result.Error, result.Output), nil
	}

	if result.ExitCode != 0 {
		return fmt.Sprintf("Command failed (exit code %d):\n%s", result.ExitCode, result.Output), nil
	}

	return result.Output, nil
}

func getGitInfo() (string, string) {
	var repo, commit string

	// Get current commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if out, err := cmd.Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}

	// Get remote URL
	cmd = exec.Command("git", "remote", "get-url", "origin")
	if out, err := cmd.Output(); err == nil {
		repo = strings.TrimSpace(string(out))
	}

	return repo, commit
}

// httpClient with reasonable timeout
var httpClient = &http.Client{
	Timeout: 10 * time.Minute,
}
