package prbot

import (
	"regexp"
	"strings"
)

// Category represents the type of changes in a PR
type Category string

const (
	CategorySecurity     Category = "security"
	CategoryArchitecture Category = "architecture"
	CategoryMigrations   Category = "migrations"
	CategoryRoutine      Category = "routine"
)

var (
	securityPatterns = []string{
		`(?i)auth`,
		`(?i)password`,
		`(?i)credential`,
		`(?i)secret`,
		`(?i)token`,
		`(?i)encrypt`,
		`(?i)decrypt`,
		`(?i)permission`,
		`(?i)bcrypt`,
		`(?i)jwt`,
		`(?i)oauth`,
		`(?i)session`,
	}

	architecturePatterns = []string{
		`go\.mod`,
		`go\.sum`,
		`package\.json`,
		`(?i)api/`,
		`(?i)interface\s+\w+`,
		`(?i)public\s+(func|type)`,
	}

	migrationPatterns = []string{
		`migrations/`,
		`(?i)CREATE\s+TABLE`,
		`(?i)ALTER\s+TABLE`,
		`(?i)DROP\s+TABLE`,
		`(?i)\.sql$`,
	}
)

// AnalyzeDiff categorizes a diff by its content
func AnalyzeDiff(diff string) Category {
	// Check in order of priority
	if matchesAny(diff, securityPatterns) {
		return CategorySecurity
	}
	if matchesAny(diff, migrationPatterns) {
		return CategoryMigrations
	}
	if matchesAny(diff, architecturePatterns) {
		return CategoryArchitecture
	}
	return CategoryRoutine
}

func matchesAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// ShouldAutoMerge returns true if the PR should be auto-merged
func ShouldAutoMerge(category Category, needsReview bool) bool {
	if needsReview {
		return false
	}
	return category == CategoryRoutine
}

// GetLabels returns labels to apply based on category
func GetLabels(category Category) []string {
	switch category {
	case CategorySecurity:
		return []string{"needs-human-review", "security"}
	case CategoryArchitecture:
		return []string{"needs-human-review", "architecture"}
	case CategoryMigrations:
		return []string{"needs-human-review", "database"}
	default:
		return []string{"auto-merge"}
	}
}

// ExtractChangeSummary attempts to summarize changes from diff
func ExtractChangeSummary(diff string) string {
	var files []string
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				file := strings.TrimPrefix(parts[3], "b/")
				files = append(files, file)
			}
		}
	}

	if len(files) == 0 {
		return "Changes made"
	}
	if len(files) == 1 {
		return "Modified " + files[0]
	}
	return "Modified " + strings.Join(files[:minInt(3, len(files))], ", ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
