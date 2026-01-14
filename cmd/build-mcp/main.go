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
var gitDaemonURL = "" // Constructed from coordinator URL

func main() {
	// Check for coordinator URL override
	if url := os.Getenv("BUILD_POOL_URL"); url != "" {
		coordinatorURL = url
	}

	// Construct git daemon URL from coordinator URL
	// e.g., "http://host:8081" -> "git://host:9418/"
	gitDaemonURL = constructGitDaemonURL(coordinatorURL)

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

// verbositySchema defines the verbosity parameter for MCP tool schemas
var verbositySchema = map[string]interface{}{
	"type":        "string",
	"description": "Output verbosity level: minimal (errors only), normal (default), full (all output)",
	"enum":        []string{"minimal", "normal", "full"},
	"default":     "normal",
}

func listTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "build",
			"description": "Build the Rust project with cargo (offloaded to build pool)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release":   map[string]interface{}{"type": "boolean", "description": "Build in release mode"},
					"package":   map[string]interface{}{"type": "string", "description": "Specific package to build"},
					"features":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"verbosity": verbositySchema,
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
					"verbosity": verbositySchema,
				},
			},
		},
		{
			"name":        "clippy",
			"description": "Run clippy lints (offloaded to build pool)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fix":       map[string]interface{}{"type": "boolean", "description": "Apply suggested fixes"},
					"verbosity": verbositySchema,
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
		{
			"name":        "get_job_logs",
			"description": "Retrieve complete logs for a completed job from retention buffer",
			"inputSchema": map[string]interface{}{
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
		verbosity, _ := args["verbosity"].(string)
		return submitJob(command, verbosity)
	case "get_job_logs":
		return getJobLogs(args)
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

func submitJob(command, verbosity string) (string, error) {
	// Get repo info from git
	repo, commit := getGitInfo()

	reqBody := map[string]interface{}{
		"command": command,
		"repo":    repo,
		"commit":  commit,
		"timeout": 300, // 5 minute default
	}
	if verbosity != "" {
		reqBody["verbosity"] = verbosity
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

func getJobLogs(args map[string]interface{}) (string, error) {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return "", fmt.Errorf("job_id is required")
	}

	// Build URL with optional stream query param
	url := coordinatorURL + "/logs/" + jobID
	if stream, ok := args["stream"].(string); ok && stream != "" && stream != "both" {
		url += "?stream=" + stream
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to connect to build pool: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusNotFound {
		var result struct {
			Error string `json:"error"`
		}
		json.Unmarshal(body, &result)
		return fmt.Sprintf("Logs not found for job %s: %s", jobID, result.Error), nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("build pool error (%d): %s", resp.StatusCode, string(body))
	}

	// Pretty-print the JSON response
	var data interface{}
	json.Unmarshal(body, &data)
	pretty, _ := json.MarshalIndent(data, "", "  ")
	return string(pretty), nil
}

func getGitInfo() (string, string) {
	var repo, commit string

	// Get current commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if out, err := cmd.Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}

	// Use git daemon URL if available (for remote workers)
	// Fall back to remote URL if git daemon URL not constructed
	if gitDaemonURL != "" {
		repo = gitDaemonURL
	} else {
		// Get remote URL
		cmd = exec.Command("git", "remote", "get-url", "origin")
		if out, err := cmd.Output(); err == nil {
			repo = strings.TrimSpace(string(out))
		}
	}

	return repo, commit
}

// constructGitDaemonURL extracts hostname from coordinator URL and constructs git daemon URL
// e.g., "http://host:8081" -> "git://host:9418/"
// For localhost, substitutes with external host (Tailscale IP or hostname)
func constructGitDaemonURL(coordURL string) string {
	hostPart := strings.TrimPrefix(coordURL, "http://")
	hostPart = strings.TrimPrefix(hostPart, "https://")

	// Remove port if present
	if idx := strings.Index(hostPart, ":"); idx != -1 {
		hostPart = hostPart[:idx]
	}
	// Remove path if present
	if idx := strings.Index(hostPart, "/"); idx != -1 {
		hostPart = hostPart[:idx]
	}

	if hostPart == "" {
		return ""
	}

	// If localhost, get external host for remote worker accessibility
	if hostPart == "localhost" || hostPart == "127.0.0.1" {
		hostPart = getExternalHost()
	}

	return fmt.Sprintf("git://%s:9418/", hostPart)
}

// getExternalHost tries to get a network-accessible address for this machine
// Prefers Tailscale IP, falls back to hostname
func getExternalHost() string {
	// Try Tailscale IP first (most reliable for remote access)
	if out, err := exec.Command("tailscale", "ip", "-4").Output(); err == nil {
		if ip := strings.TrimSpace(string(out)); ip != "" {
			return ip
		}
	}

	// Try hostname
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}

	// Last resort - return localhost (will likely fail for remote workers)
	return "localhost"
}

// httpClient with reasonable timeout
var httpClient = &http.Client{
	Timeout: 10 * time.Minute,
}
