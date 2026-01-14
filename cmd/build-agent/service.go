// cmd/build-agent/service.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

const (
	serviceName     = "build-agent"
	systemdUnitPath = "/etc/systemd/system/build-agent.service"
)

// systemd unit template
const systemdUnitTemplate = `[Unit]
Description=Build Agent Worker
Documentation=https://github.com/hochfrequenz/claude-plan-orchestrator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{.ExecStart}}
Restart=always
RestartSec=10

# Run as dedicated user if it exists, otherwise as root
{{if .User}}User={{.User}}{{end}}
{{if .Group}}Group={{.Group}}{{end}}

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ReadWritePaths={{.GitCacheDir}} {{.WorktreeDir}}

# Resource limits
LimitNOFILE=65535

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=build-agent

[Install]
WantedBy=multi-user.target
`

type unitConfig struct {
	ExecStart   string
	User        string
	Group       string
	GitCacheDir string
	WorktreeDir string
}

var (
	serviceUser        string
	serviceGroup       string
	serviceGitCacheDir string
	serviceWorktreeDir string
)

func newServiceCmd() *cobra.Command {
	serviceCmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the build-agent systemd service",
		Long:  "Install, start, stop, and manage the build-agent as a systemd service.",
	}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install build-agent as a systemd service",
		Long: `Creates a systemd unit file and enables the build-agent service.

The service will be configured to:
- Start automatically on boot
- Restart on failure with 10 second delay
- Use security hardening options
- Read config from the standard locations

Requires root privileges.`,
		RunE: runServiceInstall,
	}
	installCmd.Flags().StringVar(&serviceUser, "user", "", "User to run the service as")
	installCmd.Flags().StringVar(&serviceGroup, "group", "", "Group to run the service as")
	installCmd.Flags().StringVar(&serviceGitCacheDir, "git-cache-dir", "/var/cache/build-agent/repos", "Git cache directory")
	installCmd.Flags().StringVar(&serviceWorktreeDir, "worktree-dir", "/tmp/build-agent/jobs", "Worktree directory")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the build-agent systemd service",
		Long:  "Stops the service, disables it, and removes the systemd unit file.",
		RunE:  runServiceUninstall,
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the build-agent service",
		RunE:  runServiceStart,
	}

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the build-agent service",
		RunE:  runServiceStop,
	}

	restartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the build-agent service",
		RunE:  runServiceRestart,
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show build-agent service status",
		RunE:  runServiceStatus,
	}

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Show build-agent service logs",
		Long:  "Display logs from the build-agent service via journalctl.",
		RunE:  runServiceLogs,
	}
	var logsFollow bool
	var logsLines int
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 50, "Number of lines to show")

	serviceCmd.AddCommand(installCmd, uninstallCmd, startCmd, stopCmd, restartCmd, statusCmd, logsCmd)
	return serviceCmd
}

func runServiceInstall(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	if !isRoot() {
		return fmt.Errorf("root privileges required to install service. Try: sudo %s service install", os.Args[0])
	}

	// Find the build-agent binary
	execPath, err := findBuildAgentBinary()
	if err != nil {
		return err
	}

	// Build ExecStart command
	execStart := execPath
	// Check if a config file exists
	for _, cfgPath := range defaultConfigPaths {
		if _, err := os.Stat(cfgPath); err == nil {
			execStart = fmt.Sprintf("%s --config %s", execPath, cfgPath)
			break
		}
	}

	// Create directories
	dirs := []string{serviceGitCacheDir, serviceWorktreeDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
		fmt.Printf("Created directory: %s\n", dir)

		// Set ownership if user specified
		if serviceUser != "" {
			if err := runCmd("chown", "-R", serviceUser+":"+serviceGroup, dir); err != nil {
				fmt.Printf("Warning: could not set ownership on %s: %v\n", dir, err)
			}
		}
	}

	// Generate unit file
	cfg := unitConfig{
		ExecStart:   execStart,
		User:        serviceUser,
		Group:       serviceGroup,
		GitCacheDir: serviceGitCacheDir,
		WorktreeDir: serviceWorktreeDir,
	}

	tmpl, err := template.New("unit").Parse(systemdUnitTemplate)
	if err != nil {
		return fmt.Errorf("parsing unit template: %w", err)
	}

	var unitContent strings.Builder
	if err := tmpl.Execute(&unitContent, cfg); err != nil {
		return fmt.Errorf("executing unit template: %w", err)
	}

	// Write unit file
	if err := os.WriteFile(systemdUnitPath, []byte(unitContent.String()), 0644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}
	fmt.Printf("Created systemd unit: %s\n", systemdUnitPath)

	// Reload systemd
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}

	// Enable the service
	if err := runCmd("systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("enabling service: %w", err)
	}

	fmt.Printf("\nService installed and enabled successfully!\n")
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Ensure config file exists at %s\n", defaultConfigPaths[0])
	fmt.Printf("  2. Start the service: build-agent service start\n")
	fmt.Printf("  3. Check status: build-agent service status\n")
	fmt.Printf("  4. View logs: build-agent service logs -f\n")

	return nil
}

func runServiceUninstall(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	if !isRoot() {
		return fmt.Errorf("root privileges required. Try: sudo %s service uninstall", os.Args[0])
	}

	// Stop the service if running
	_ = runCmd("systemctl", "stop", serviceName)

	// Disable the service
	_ = runCmd("systemctl", "disable", serviceName)

	// Remove the unit file
	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}

	// Reload systemd
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}

	fmt.Printf("Service uninstalled successfully.\n")
	fmt.Printf("Note: Config file at %s was not removed.\n", defaultConfigPaths[0])

	return nil
}

func runServiceStart(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	if !serviceInstalled() {
		return fmt.Errorf("service not installed. Run: build-agent service install")
	}

	if !isRoot() {
		// Try with sudo
		return runCmdInteractive("sudo", "systemctl", "start", serviceName)
	}

	if err := runCmd("systemctl", "start", serviceName); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}

	fmt.Printf("Service started. Check status with: build-agent service status\n")
	return nil
}

func runServiceStop(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	if !serviceInstalled() {
		return fmt.Errorf("service not installed")
	}

	if !isRoot() {
		return runCmdInteractive("sudo", "systemctl", "stop", serviceName)
	}

	if err := runCmd("systemctl", "stop", serviceName); err != nil {
		return fmt.Errorf("stopping service: %w", err)
	}

	fmt.Printf("Service stopped.\n")
	return nil
}

func runServiceRestart(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	if !serviceInstalled() {
		return fmt.Errorf("service not installed. Run: build-agent service install")
	}

	if !isRoot() {
		return runCmdInteractive("sudo", "systemctl", "restart", serviceName)
	}

	if err := runCmd("systemctl", "restart", serviceName); err != nil {
		return fmt.Errorf("restarting service: %w", err)
	}

	fmt.Printf("Service restarted. Check status with: build-agent service status\n")
	return nil
}

func runServiceStatus(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	if !serviceInstalled() {
		fmt.Printf("Service not installed.\n")
		fmt.Printf("Install with: build-agent service install\n")
		return nil
	}

	// Run systemctl status interactively to show full output
	return runCmdInteractive("systemctl", "status", serviceName, "--no-pager")
}

func runServiceLogs(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service management is only supported on Linux")
	}

	follow, _ := cmd.Flags().GetBool("follow")
	lines, _ := cmd.Flags().GetInt("lines")

	jArgs := []string{"-u", serviceName, "-n", fmt.Sprintf("%d", lines), "--no-pager"}
	if follow {
		jArgs = append(jArgs, "-f")
	}

	return runCmdInteractive("journalctl", jArgs...)
}

// Helper functions

func isRoot() bool {
	return os.Geteuid() == 0
}

func serviceInstalled() bool {
	_, err := os.Stat(systemdUnitPath)
	return err == nil
}

func findBuildAgentBinary() (string, error) {
	// First, try to find the current executable
	execPath, err := os.Executable()
	if err == nil {
		execPath, err = filepath.EvalSymlinks(execPath)
		if err == nil {
			return execPath, nil
		}
	}

	// Try to find in PATH
	path, err := exec.LookPath("build-agent")
	if err == nil {
		return filepath.Abs(path)
	}

	// Try common locations
	commonPaths := []string{
		"/usr/local/bin/build-agent",
		"/usr/bin/build-agent",
		"/opt/build-agent/build-agent",
	}
	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("could not find build-agent binary. Ensure it's installed in PATH or /usr/local/bin")
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmdInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
