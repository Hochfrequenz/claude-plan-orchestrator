// Package updater provides self-update functionality for claude-orch
package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubRepo    = "hochfrequenz/claude-plan-orchestrator"
	githubAPIURL  = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
	downloadURL   = "https://github.com/" + githubRepo + "/releases/download"
	binaryName    = "claude-orch"
	checkTimeout  = 10 * time.Second
	downloadTimeout = 5 * time.Minute
)

// GitHubRelease represents the GitHub API response for a release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// CheckLatestVersion fetches the latest version tag from GitHub
func CheckLatestVersion() (string, error) {
	client := &http.Client{Timeout: checkTimeout}

	resp, err := client.Get(githubAPIURL)
	if err != nil {
		return "", fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	return release.TagName, nil
}

// NeedsUpdate compares version strings and returns true if latest is newer
// Versions are expected in format "vX.Y.Z" or "X.Y.Z"
func NeedsUpdate(current, latest string) bool {
	// Strip 'v' prefix if present
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	// "dev" version always needs update (unless latest is also dev)
	if current == "dev" {
		return latest != "dev"
	}

	// Parse version components
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	// Compare major.minor.patch
	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return false // Equal versions
}

// parseVersion extracts major, minor, patch from a version string
func parseVersion(v string) [3]int {
	var parts [3]int
	fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
	return parts
}

// SelfUpdate downloads and installs the specified version
func SelfUpdate(targetVersion string) error {
	// Determine platform
	platform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)

	// Build download URL
	// Format: claude-orch_0.3.20_linux_amd64.tar.gz
	versionNum := strings.TrimPrefix(targetVersion, "v")
	archiveName := fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, versionNum, platform)
	url := fmt.Sprintf("%s/%s/%s", downloadURL, targetVersion, archiveName)

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "claude-orch-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download archive
	archivePath := filepath.Join(tmpDir, archiveName)
	if err := downloadFile(url, archivePath); err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	// Extract archive
	newBinaryPath := filepath.Join(tmpDir, binaryName)
	if err := extractTarGz(archivePath, tmpDir, binaryName); err != nil {
		return fmt.Errorf("failed to extract update: %w", err)
	}

	// Get current executable path
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Replace binary
	if err := replaceBinary(currentExe, newBinaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	return nil
}

// downloadFile downloads a URL to a local file
func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: downloadTimeout}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// extractTarGz extracts a specific file from a tar.gz archive
func extractTarGz(archivePath, destDir, targetFile string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Look for the target binary (may be at root or in a subdirectory)
		baseName := filepath.Base(header.Name)
		if baseName == targetFile && header.Typeflag == tar.TypeReg {
			destPath := filepath.Join(destDir, targetFile)
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, tr); err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("binary %s not found in archive", targetFile)
}

// replaceBinary replaces the current binary with a new one
func replaceBinary(currentPath, newPath string) error {
	// Get permissions of current binary
	info, err := os.Stat(currentPath)
	if err != nil {
		return err
	}

	// Create backup path
	backupPath := currentPath + ".old"

	// Remove old backup if exists
	os.Remove(backupPath)

	// Rename current to backup
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Copy new binary to current location (can't rename across filesystems)
	if err := copyFile(newPath, currentPath, info.Mode()); err != nil {
		// Rollback: restore backup
		os.Rename(backupPath, currentPath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Remove backup
	os.Remove(backupPath)

	return nil
}

// copyFile copies a file preserving permissions
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
