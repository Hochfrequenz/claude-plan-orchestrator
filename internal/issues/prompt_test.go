// internal/issues/prompt_test.go
package issues

import (
	"strings"
	"testing"
)

func TestBuildAnalysisPrompt(t *testing.T) {
	prompt := BuildAnalysisPrompt(42, "owner/repo", "/path/to/plans")

	if !strings.Contains(prompt, "42") {
		t.Error("prompt should contain issue number")
	}
	if !strings.Contains(prompt, "owner/repo") {
		t.Error("prompt should contain repo")
	}
	if !strings.Contains(prompt, "problem_statement") {
		t.Error("prompt should mention checklist items")
	}
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should request JSON output")
	}
}
