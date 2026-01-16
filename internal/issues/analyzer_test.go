// internal/issues/analyzer_test.go
package issues

import (
	"testing"
)

func TestParseAnalysisResult(t *testing.T) {
	output := `{
		"issue_number": 42,
		"ready": true,
		"checklist": {
			"problem_statement": {"pass": true, "notes": ""},
			"acceptance_criteria": {"pass": true, "notes": ""},
			"bounded_scope": {"pass": true, "notes": ""},
			"no_blocking_questions": {"pass": true, "notes": ""},
			"files_identified": {"pass": true, "notes": "src/api/billing.go"}
		},
		"group": "billing",
		"plan_files": ["docs/plans/billing/issue-42/epic-00-add-retry.md"],
		"dependencies": ["billing/E03"],
		"comment_posted": true,
		"labels_updated": true
	}`

	result, err := ParseAnalysisResult([]byte(output))
	if err != nil {
		t.Fatalf("ParseAnalysisResult() error = %v", err)
	}

	if !result.Ready {
		t.Error("expected Ready = true")
	}
	if result.Group != "billing" {
		t.Errorf("Group = %v, want billing", result.Group)
	}
	if len(result.PlanFiles) != 1 {
		t.Errorf("PlanFiles count = %d, want 1", len(result.PlanFiles))
	}
}

func TestAnalysisResult_AllChecksPassed(t *testing.T) {
	result := &AnalysisResult{
		Checklist: map[string]ChecklistItem{
			"problem_statement":     {Pass: true},
			"acceptance_criteria":   {Pass: true},
			"bounded_scope":         {Pass: true},
			"no_blocking_questions": {Pass: true},
			"files_identified":      {Pass: true},
		},
	}
	if !result.AllChecksPassed() {
		t.Error("expected AllChecksPassed() = true")
	}

	result.Checklist["bounded_scope"] = ChecklistItem{Pass: false}
	if result.AllChecksPassed() {
		t.Error("expected AllChecksPassed() = false when one fails")
	}
}
