package prbot

import (
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestBuildPRBody(t *testing.T) {
	task := &domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		FilePath: "docs/plans/technical-module/epic-05-validators.md",
	}

	body := BuildPRBody(task, "Added validation functions", 15, "2m30s")

	if !containsString(body, "Validators") {
		t.Error("Body should contain task title")
	}
	if !containsString(body, "15 tests passed") {
		t.Error("Body should contain test count")
	}
	if !containsString(body, "ERP Orchestrator") {
		t.Error("Body should contain attribution")
	}
}

func TestAnalyzeDiff_Security(t *testing.T) {
	diff := `
diff --git a/auth/login.go b/auth/login.go
+func validatePassword(password string) bool {
+    return bcrypt.CompareHashAndPassword(hash, []byte(password))
+}
`
	category := AnalyzeDiff(diff)
	if category != CategorySecurity {
		t.Errorf("Category = %s, want security", category)
	}
}

func TestAnalyzeDiff_Architecture(t *testing.T) {
	diff := `
diff --git a/go.mod b/go.mod
+require github.com/newdep/pkg v1.0.0
`
	category := AnalyzeDiff(diff)
	if category != CategoryArchitecture {
		t.Errorf("Category = %s, want architecture", category)
	}
}

func TestAnalyzeDiff_Migrations(t *testing.T) {
	diff := `
diff --git a/migrations/001_create_users.sql b/migrations/001_create_users.sql
+CREATE TABLE users (
+    id SERIAL PRIMARY KEY
+);
`
	category := AnalyzeDiff(diff)
	if category != CategoryMigrations {
		t.Errorf("Category = %s, want migrations", category)
	}
}

func TestAnalyzeDiff_Routine(t *testing.T) {
	diff := `
diff --git a/utils/format.go b/utils/format.go
+func FormatDate(t time.Time) string {
+    return t.Format("2006-01-02")
+}
`
	category := AnalyzeDiff(diff)
	if category != CategoryRoutine {
		t.Errorf("Category = %s, want routine", category)
	}
}

func TestShouldAutoMerge(t *testing.T) {
	tests := []struct {
		category    Category
		needsReview bool
		want        bool
	}{
		{CategoryRoutine, false, true},
		{CategoryRoutine, true, false},
		{CategorySecurity, false, false},
		{CategoryArchitecture, false, false},
		{CategoryMigrations, false, false},
	}

	for _, tt := range tests {
		got := ShouldAutoMerge(tt.category, tt.needsReview)
		if got != tt.want {
			t.Errorf("ShouldAutoMerge(%s, %v) = %v, want %v",
				tt.category, tt.needsReview, got, tt.want)
		}
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
