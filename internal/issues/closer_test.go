// internal/issues/closer_test.go
package issues

import (
	"strings"
	"testing"
)

func TestBuildClosureComment(t *testing.T) {
	comment := BuildClosureComment(123, "Added retry logic", []string{"src/api.go", "src/api_test.go"})

	if comment == "" {
		t.Error("expected non-empty comment")
	}
	// Check key elements
	tests := []string{"123", "retry logic", "src/api.go", "Claude Plan Orchestrator"}
	for _, want := range tests {
		if !strings.Contains(comment, want) {
			t.Errorf("comment missing %q", want)
		}
	}
}

func TestBuildClosureComment_NoFiles(t *testing.T) {
	comment := BuildClosureComment(456, "Fixed bug", nil)

	if comment == "" {
		t.Error("expected non-empty comment")
	}
	// Should still have PR number and summary
	if !strings.Contains(comment, "456") {
		t.Error("comment missing PR number")
	}
	if !strings.Contains(comment, "Fixed bug") {
		t.Error("comment missing summary")
	}
	if !strings.Contains(comment, "Claude Plan Orchestrator") {
		t.Error("comment missing attribution")
	}
}

func TestBuildClosureComment_EmptyFiles(t *testing.T) {
	comment := BuildClosureComment(789, "Refactored module", []string{})

	if comment == "" {
		t.Error("expected non-empty comment")
	}
	// Should not contain "Changed files" section when files are empty
	if strings.Contains(comment, "Changed files") {
		t.Error("comment should not have Changed files section for empty list")
	}
}
