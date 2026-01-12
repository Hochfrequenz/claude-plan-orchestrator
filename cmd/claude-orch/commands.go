package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/config"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
	"github.com/hochfrequenz/claude-plan-orchestrator/tui"
	"github.com/hochfrequenz/claude-plan-orchestrator/web/api"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	startCount  int
	startModule string
	listStatus  string
	listModule  string
	servePort   int
)

func init() {
	// start command
	startCmd := &cobra.Command{
		Use:   "start [TASK...]",
		Short: "Start tasks",
		RunE:  runStart,
	}
	startCmd.Flags().IntVar(&startCount, "count", 3, "number of tasks to start")
	startCmd.Flags().StringVar(&startModule, "module", "", "filter by module")
	rootCmd.AddCommand(startCmd)

	// status command
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show current status",
		RunE:  runStatus,
	}
	rootCmd.AddCommand(statusCmd)

	// list command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE:  runList,
	}
	listCmd.Flags().StringVar(&listStatus, "status", "", "filter by status")
	listCmd.Flags().StringVar(&listModule, "module", "", "filter by module")
	rootCmd.AddCommand(listCmd)

	// sync command
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync tasks from markdown files",
		RunE:  runSync,
	}
	rootCmd.AddCommand(syncCmd)

	// logs command
	logsCmd := &cobra.Command{
		Use:   "logs TASK",
		Short: "View logs for a task",
		Args:  cobra.ExactArgs(1),
		RunE:  runLogs,
	}
	rootCmd.AddCommand(logsCmd)

	// tui command
	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch TUI dashboard",
		RunE:  runTUI,
	}
	rootCmd.AddCommand(tuiCmd)

	// serve command
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start web UI server",
		RunE:  runServe,
	}
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func loadConfig() (*config.Config, error) {
	path := configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	return config.Load(path)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// If specific tasks provided, start those
	if len(args) > 0 {
		for _, taskID := range args {
			fmt.Printf("Starting task: %s\n", taskID)
			// TODO: Actually start the task
		}
		return nil
	}

	// Otherwise, get ready tasks from scheduler
	tasks, err := store.ListTasks(taskstore.ListOptions{Module: startModule})
	if err != nil {
		return err
	}

	completed, err := store.GetCompletedTaskIDs()
	if err != nil {
		return err
	}

	sched := scheduler.New(tasks, completed)
	ready := sched.GetReadyTasks(startCount)

	if len(ready) == 0 {
		fmt.Println("No tasks ready to start")
		return nil
	}

	fmt.Printf("Starting %d tasks:\n", len(ready))
	for _, task := range ready {
		fmt.Printf("  - %s: %s\n", task.ID.String(), task.Title)
	}

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	tasks, err := store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		return err
	}

	var notStarted, inProgress, complete int
	for _, t := range tasks {
		switch t.Status {
		case domain.StatusNotStarted:
			notStarted++
		case domain.StatusInProgress:
			inProgress++
		case domain.StatusComplete:
			complete++
		}
	}

	fmt.Printf("Tasks: %d total | %d not started | %d in progress | %d complete\n",
		len(tasks), notStarted, inProgress, complete)

	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	opts := taskstore.ListOptions{
		Module: listModule,
		Status: domain.TaskStatus(listStatus),
	}

	tasks, err := store.ListTasks(opts)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tPRIORITY")
	for _, t := range tasks {
		priority := string(t.Priority)
		if priority == "" {
			priority = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			t.ID.String(), t.Title, t.Status, priority)
	}
	w.Flush()

	return nil
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if cfg.General.ProjectRoot == "" {
		return fmt.Errorf("project_root not configured")
	}

	plansDir := cfg.General.ProjectRoot + "/docs/plans"
	tasks, err := parser.ParsePlansDir(plansDir)
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	for _, task := range tasks {
		if err := store.UpsertTask(task); err != nil {
			return fmt.Errorf("upserting %s: %w", task.ID.String(), err)
		}
	}

	fmt.Printf("Synced %d tasks from %s\n", len(tasks), plansDir)
	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	fmt.Printf("Logs for task: %s\n", taskID)
	fmt.Println("(not implemented)")
	return nil
}

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Open database
	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Load all tasks
	allTasks, err := store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to load tasks: %w", err)
	}

	// Load queued tasks (not started)
	queued, _ := store.ListTasks(taskstore.ListOptions{Status: domain.StatusNotStarted})

	model := tui.NewModel(tui.ModelConfig{
		MaxActive:   cfg.General.MaxParallelAgents,
		AllTasks:    allTasks,
		Queued:      queued,
		ProjectRoot: cfg.General.ProjectRoot,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	// Save config if it was changed in the TUI
	if m, ok := finalModel.(tui.Model); ok && m.ConfigChanged() {
		cfg.General.MaxParallelAgents = m.GetMaxActive()
		cfgPath := configPath
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}
		if err := cfg.Save(cfgPath); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Printf("Saved max_parallel_agents = %d to config\n", m.GetMaxActive())
	}

	return nil
}

// storeAdapter wraps taskstore.Store to implement api.Store
type storeAdapter struct {
	store *taskstore.Store
}

func (a *storeAdapter) ListTasks(opts interface{}) ([]*domain.Task, error) {
	return a.store.ListTasks(taskstore.ListOptions{})
}

func (a *storeAdapter) GetTask(id string) (*domain.Task, error) {
	return a.store.GetTask(id)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}

	port := servePort
	if port == 0 {
		port = cfg.Web.Port
	}

	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, port)
	adapter := &storeAdapter{store: store}
	server := api.NewServer(adapter, nil, addr)

	fmt.Printf("Starting web UI at http://%s\n", addr)
	return server.Start()
}
