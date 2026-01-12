package parser

import (
	"regexp"
	"testing"
)

func TestReadmeRegexPatterns(t *testing.T) {
	// These are the regex patterns from ParseReadmeStatuses in parser.go
	// Pattern 1: xxx-module/epic-NN-...
	epicStandardRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]*)-module/epic-(\d+)-[^)]+\.md\)`)
	// Pattern 2: YYYY-MM-DD-xxx-epic-N-... or YYYY-MM-DD-xxx-module-epic-N-...
	epicDatePrefixRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?\d{4}-\d{2}-\d{2}-([a-z][a-z0-9-]*(?:-module)?)-epic-(\d+)-[^)]+\.md\)`)
	// Pattern 3: xxx-module/YYYY-MM-DD-...-epic-N-...
	epicNestedDateRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]*)-module/\d{4}-\d{2}-\d{2}-[a-z][a-z0-9-]*-epic-(\d+)-[^)]+\.md\)`)
	// Pattern 4: xxx/epic-NN-... (non-module directories)
	epicNonModuleRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]+)/epic-(\d+)-[^)]+\.md\)`)

	tests := []struct {
		name        string
		line        string
		wantMatch   bool
		wantModule  string
		wantEpicNum string
		regex       *regexp.Regexp
		regexName   string
	}{
		// Standard xxx-module/epic-NN-yyy.md pattern
		{
			name:        "technical-module standard",
			line:        "| [E00](docs/plans/technical-module/epic-00-scaffolding.md) | Project Scaffolding | 🟢 |",
			wantMatch:   true,
			wantModule:  "technical",
			wantEpicNum: "00",
			regex:       epicStandardRegex,
			regexName:   "epicStandardRegex",
		},
		{
			name:        "customer-module standard",
			line:        "| [E05](docs/plans/customer-module/epic-05-http-api.md) | HTTP API Wiring | 🔴 |",
			wantMatch:   true,
			wantModule:  "customer",
			wantEpicNum: "05",
			regex:       epicStandardRegex,
			regexName:   "epicStandardRegex",
		},
		// Date prefix YYYY-MM-DD-xxx-epic-N-yyy.md pattern
		{
			name:        "subledger date prefix",
			line:        "| [E1](docs/plans/2026-01-05-subledger-epic-1-core-foundation.md) | Core Foundation | 🟢 |",
			wantMatch:   true,
			wantModule:  "subledger",
			wantEpicNum: "1",
			regex:       epicDatePrefixRegex,
			regexName:   "epicDatePrefixRegex",
		},
		{
			name:        "task-module date prefix",
			line:        "| [E00](docs/plans/2026-01-07-task-module-epic-0-scaffolding.md) | Module Scaffolding | 🟢 |",
			wantMatch:   true,
			wantModule:  "task-module",
			wantEpicNum: "0",
			regex:       epicDatePrefixRegex,
			regexName:   "epicDatePrefixRegex",
		},
		// Nested path: xxx-module/YYYY-MM-DD-xxx-module-epic-N-yyy.md
		{
			name:        "task-module nested with date",
			line:        "| [E03](docs/plans/task-module/2026-01-07-task-module-epic-3-task-types-sla.md) | Task Types | 🟢 |",
			wantMatch:   true,
			wantModule:  "task",
			wantEpicNum: "3",
			regex:       epicNestedDateRegex,
			regexName:   "epicNestedDateRegex",
		},
		// Testing strategy module (non-standard directory name)
		{
			name:        "testing-strategy non-module",
			line:        "| [E1](docs/plans/testing-strategy/epic-01-core-infrastructure.md) | Core Infrastructure | 🟢 |",
			wantMatch:   true,
			wantModule:  "testing-strategy",
			wantEpicNum: "01",
			regex:       epicNonModuleRegex,
			regexName:   "epicNonModuleRegex",
		},
		// Workflow-module should match standard
		{
			name:        "workflow-module standard",
			line:        "| [E00](docs/plans/workflow-module/epic-00-scaffolding.md) | Module Scaffolding | 🟢 |",
			wantMatch:   true,
			wantModule:  "workflow",
			wantEpicNum: "00",
			regex:       epicStandardRegex,
			regexName:   "epicStandardRegex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.regex.FindStringSubmatch(tt.line)

			if tt.wantMatch {
				if matches == nil {
					t.Errorf("Expected %s to match but got none for: %s", tt.regexName, tt.line)
					// Try all patterns and show what they find
					t.Logf("epicStandardRegex matches: %v", epicStandardRegex.FindStringSubmatch(tt.line))
					t.Logf("epicDatePrefixRegex matches: %v", epicDatePrefixRegex.FindStringSubmatch(tt.line))
					t.Logf("epicNestedDateRegex matches: %v", epicNestedDateRegex.FindStringSubmatch(tt.line))
					t.Logf("epicNonModuleRegex matches: %v", epicNonModuleRegex.FindStringSubmatch(tt.line))
					return
				}
				if len(matches) < 4 {
					t.Errorf("Expected 4 groups but got %d: %v", len(matches), matches)
					return
				}
				// Group 2 is the module, Group 3 is the epic number
				if matches[2] != tt.wantModule {
					t.Errorf("Module = %q, want %q", matches[2], tt.wantModule)
				}
				if matches[3] != tt.wantEpicNum {
					t.Errorf("Epic num = %q, want %q", matches[3], tt.wantEpicNum)
				}
			} else {
				if matches != nil {
					t.Errorf("Expected no match but got: %v", matches)
				}
			}
		})
	}
}

func TestAllRegexPatternsAgainstRealLines(t *testing.T) {
	// Test what ParseReadmeStatuses would actually produce
	epicStandardRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]*)-module/epic-(\d+)-[^)]+\.md\)`)
	epicDatePrefixRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?\d{4}-\d{2}-\d{2}-([a-z][a-z0-9-]*(?:-module)?)-epic-(\d+)-[^)]+\.md\)`)
	epicNestedDateRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]*)-module/\d{4}-\d{2}-\d{2}-[a-z][a-z0-9-]*-epic-(\d+)-[^)]+\.md\)`)
	epicNonModuleRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]+)/epic-(\d+)-[^)]+\.md\)`)

	lines := []struct {
		line           string
		expectedModule string
		expectedEpic   string
	}{
		{"| [E00](docs/plans/technical-module/epic-00-scaffolding.md) | Project Scaffolding | 🟢 |", "technical", "00"},
		{"| [E01](docs/plans/technical-module/epic-01-supporting-entities.md) | Supporting Entities | 🟢 |", "technical", "01"},
		{"| [E1](docs/plans/2026-01-05-subledger-epic-1-core-foundation.md) | Core Foundation | 🟢 |", "subledger", "1"},
		{"| [E2](docs/plans/2026-01-05-subledger-epic-2-payment-processing.md) | Payment Processing | 🟢 |", "subledger", "2"},
		{"| [E00](docs/plans/2026-01-07-task-module-epic-0-scaffolding.md) | Module Scaffolding | 🟢 |", "task", "0"},
		{"| [E03](docs/plans/task-module/2026-01-07-task-module-epic-3-task-types-sla.md) | Task Types | 🟢 |", "task", "3"},
		{"| [E1](docs/plans/testing-strategy/epic-01-core-infrastructure.md) | Core Infrastructure | 🟢 |", "testing-strategy", "01"},
	}

	for _, tc := range lines {
		t.Run(tc.line[:50]+"...", func(t *testing.T) {
			var module, epicNum string
			var matched bool

			// Try patterns in order of specificity (same as ParseReadmeStatuses)
			if matches := epicStandardRegex.FindStringSubmatch(tc.line); matches != nil {
				module = matches[2]
				epicNum = matches[3]
				matched = true
				t.Logf("Matched epicStandardRegex: module=%s, epic=%s", module, epicNum)
			} else if matches := epicNestedDateRegex.FindStringSubmatch(tc.line); matches != nil {
				module = matches[2]
				epicNum = matches[3]
				matched = true
				t.Logf("Matched epicNestedDateRegex: module=%s, epic=%s", module, epicNum)
			} else if matches := epicDatePrefixRegex.FindStringSubmatch(tc.line); matches != nil {
				module = matches[2]
				epicNum = matches[3]
				// Strip -module suffix if present
				if len(module) > 7 && module[len(module)-7:] == "-module" {
					module = module[:len(module)-7]
				}
				matched = true
				t.Logf("Matched epicDatePrefixRegex: module=%s, epic=%s", module, epicNum)
			} else if matches := epicNonModuleRegex.FindStringSubmatch(tc.line); matches != nil {
				module = matches[2]
				epicNum = matches[3]
				matched = true
				t.Logf("Matched epicNonModuleRegex: module=%s, epic=%s", module, epicNum)
			}

			if !matched {
				t.Errorf("No regex matched line: %s", tc.line)
				return
			}

			if module != tc.expectedModule {
				t.Errorf("Module = %q, want %q", module, tc.expectedModule)
			}
			if epicNum != tc.expectedEpic {
				t.Errorf("Epic = %q, want %q", epicNum, tc.expectedEpic)
			}
		})
	}
}
