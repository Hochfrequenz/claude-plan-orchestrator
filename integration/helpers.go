//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// FixturesDir returns the path to the fixtures directory
func FixturesDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	return filepath.Join(filepath.Dir(filename), "fixtures")
}

// SamplePlansDir returns the path to the sample-plans fixtures
func SamplePlansDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(FixturesDir(t), "sample-plans")
}

// TempDBPath creates a temporary database path for testing
func TempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

// TempConfigPath creates a temporary config file path for testing
func TempConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "config.toml")
}

// CopyFixturesToTemp copies the fixtures to a temp directory
// This is useful when tests need to modify files
func CopyFixturesToTemp(t *testing.T) string {
	t.Helper()
	src := SamplePlansDir(t)
	dst := filepath.Join(t.TempDir(), "plans")

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("Failed to copy fixtures: %v", err)
	}

	return dst
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(targetPath, data, 0644)
	})
}
