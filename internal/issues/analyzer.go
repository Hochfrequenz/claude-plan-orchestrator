// internal/issues/analyzer.go
package issues

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/config"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
)

type ChecklistItem struct {
	Pass  bool   `json:"pass"`
	Notes string `json:"notes"`
}

type AnalysisResult struct {
	IssueNumber           int                      `json:"issue_number"`
	Ready                 bool                     `json:"ready"`
	Checklist             map[string]ChecklistItem `json:"checklist"`
	Group                 string                   `json:"group"`
	PlanFiles             []string                 `json:"plan_files"`
	Dependencies          []string                 `json:"dependencies"`
	CommentPosted         bool                     `json:"comment_posted"`
	LabelsUpdated         bool                     `json:"labels_updated"`
	RefinementSuggestions []string                 `json:"refinement_suggestions,omitempty"`
}

func ParseAnalysisResult(data []byte) (*AnalysisResult, error) {
	var result AnalysisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *AnalysisResult) AllChecksPassed() bool {
	for _, item := range r.Checklist {
		if !item.Pass {
			return false
		}
	}
	return true
}

type Analyzer struct {
	store    *taskstore.Store
	fetcher  *Fetcher
	config   *config.GitHubIssuesConfig
	plansDir string
}

func NewAnalyzer(store *taskstore.Store, cfg *config.GitHubIssuesConfig, plansDir string) *Analyzer {
	return &Analyzer{
		store:    store,
		fetcher:  NewFetcher(cfg),
		config:   cfg,
		plansDir: plansDir,
	}
}

// AnalyzeCandidates fetches and analyzes all candidate issues.
func (a *Analyzer) AnalyzeCandidates(ctx context.Context, maxParallel int) error {
	issues, err := a.fetcher.FetchCandidateIssues()
	if err != nil {
		return fmt.Errorf("fetch candidates: %w", err)
	}

	// Filter out already-analyzed issues
	var toAnalyze []*domain.GitHubIssue
	for _, issue := range issues {
		existing, err := a.store.GetGitHubIssue(issue.IssueNumber)
		if err != nil || existing == nil || existing.Status == domain.IssuePending {
			toAnalyze = append(toAnalyze, issue)
		}
	}

	// Analyze with concurrency limit
	sem := make(chan struct{}, maxParallel)
	errCh := make(chan error, len(toAnalyze))

	for _, issue := range toAnalyze {
		sem <- struct{}{}
		go func(iss *domain.GitHubIssue) {
			defer func() { <-sem }()
			if err := a.analyzeIssue(ctx, iss); err != nil {
				errCh <- fmt.Errorf("issue #%d: %w", iss.IssueNumber, err)
			}
		}(issue)
	}

	// Wait for all to complete
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
	close(errCh)

	// Collect errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("analysis errors: %v", errs)
	}
	return nil
}

// AnalyzeOne analyzes a single issue (for manual triggering).
func (a *Analyzer) AnalyzeOne(ctx context.Context, issue *domain.GitHubIssue) error {
	return a.analyzeIssue(ctx, issue)
}

func (a *Analyzer) analyzeIssue(ctx context.Context, issue *domain.GitHubIssue) error {
	// Save as pending
	issue.Status = domain.IssuePending
	if err := a.store.UpsertGitHubIssue(issue); err != nil {
		return err
	}

	// Build prompt
	prompt := BuildAnalysisPrompt(issue.IssueNumber, a.config.Repo, a.plansDir)

	// Spawn Claude Code agent
	cmd := exec.CommandContext(ctx, "claude",
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "text",
		"-p", prompt)
	cmd.Dir = filepath.Dir(a.plansDir) // project root

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude agent: %w", err)
	}

	// Parse result from output (agent should output JSON)
	result, err := extractJSONFromOutput(output)
	if err != nil {
		return fmt.Errorf("parse result: %w", err)
	}

	// Update issue status based on result
	now := time.Now()
	issue.AnalyzedAt = &now
	if result.Ready {
		issue.Status = domain.IssueReady
		issue.GroupName = result.Group
		if len(result.PlanFiles) > 0 {
			issue.PlanPath = result.PlanFiles[0]
		}
	} else {
		issue.Status = domain.IssueNeedsRefinement
	}

	if err := a.store.UpsertGitHubIssue(issue); err != nil {
		return err
	}

	// If ready, parse and upsert the generated task
	if result.Ready && len(result.PlanFiles) > 0 {
		for _, planPath := range result.PlanFiles {
			task, err := parser.ParseEpicFile(planPath)
			if err != nil {
				return fmt.Errorf("parse plan: %w", err)
			}
			if err := a.store.UpsertTask(task); err != nil {
				return fmt.Errorf("upsert task: %w", err)
			}
		}
	}

	return nil
}

func extractJSONFromOutput(output []byte) (*AnalysisResult, error) {
	// Try to find JSON in the output (may be wrapped in markdown code blocks)
	str := string(output)

	// Look for JSON object
	start := -1
	depth := 0
	for i, c := range str {
		if c == '{' {
			if start == -1 {
				start = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && start != -1 {
				return ParseAnalysisResult([]byte(str[start : i+1]))
			}
		}
	}
	return nil, fmt.Errorf("no JSON object found in output")
}
