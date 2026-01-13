package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildpool"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/config"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/observer"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/skills"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
	"github.com/hochfrequenz/claude-plan-orchestrator/tui"
	"github.com/hochfrequenz/claude-plan-orchestrator/web/api"
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

	// build-pool command group
	buildPoolCmd := &cobra.Command{
		Use:   "build-pool",
		Short: "Manage the build worker pool",
	}

	buildPoolStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the build pool coordinator",
		RunE:  runBuildPoolStart,
	}

	buildPoolStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show build pool status",
		RunE:  runBuildPoolStatus,
	}

	buildPoolStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the build pool coordinator",
		RunE:  runBuildPoolStop,
	}

	buildPoolCmd.AddCommand(buildPoolStartCmd, buildPoolStatusCmd, buildPoolStopCmd)
	rootCmd.AddCommand(buildPoolCmd)
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
	// Ensure required skills are installed
	if installed, err := skills.EnsureInstalled(); err != nil {
		fmt.Printf("Warning: failed to install skills: %v\n", err)
	} else if installed {
		fmt.Println("Installed autonomous-plan-execution skill")
	}

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

	// Create agent manager with persistence
	agentMgr := executor.NewAgentManager(cfg.General.MaxParallelAgents)
	agentStoreAdp := &agentStoreAdapter{store: store}
	agentMgr.SetStore(agentStoreAdp)

	// Recover any agents that were running before
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recoveredAgents, err := agentMgr.RecoverAgents(ctx)
	if err != nil {
		fmt.Printf("Warning: failed to recover agents: %v\n", err)
	}

	// Start build pool coordinator if enabled
	var buildPoolCoord *buildpool.Coordinator
	var buildPoolGitDaemon *buildpool.GitDaemon
	if cfg.BuildPool.Enabled {
		// Create registry
		registry := buildpool.NewRegistry()

		// Set up embedded worker if enabled
		var embeddedFunc buildpool.EmbeddedWorkerFunc
		if cfg.BuildPool.LocalFallback.Enabled {
			embedded := buildpool.NewEmbeddedWorker(buildpool.EmbeddedConfig{
				RepoDir:     cfg.General.ProjectRoot,
				WorktreeDir: cfg.BuildPool.LocalFallback.WorktreeDir,
				MaxJobs:     cfg.BuildPool.LocalFallback.MaxJobs,
				UseNixShell: true,
			})
			embeddedFunc = embedded.Run
		}

		// Create dispatcher with embedded worker
		dispatcher := buildpool.NewDispatcher(registry, embeddedFunc)

		// Create coordinator
		buildPoolCoord = buildpool.NewCoordinator(buildpool.CoordinatorConfig{
			WebSocketPort:     cfg.BuildPool.WebSocketPort,
			HeartbeatInterval: time.Duration(cfg.BuildPool.Timeouts.HeartbeatIntervalSecs) * time.Second,
			HeartbeatTimeout:  time.Duration(cfg.BuildPool.Timeouts.HeartbeatTimeoutSecs) * time.Second,
			Debug:             cfg.BuildPool.Debug,
		}, registry, dispatcher)

		// Start git daemon
		buildPoolGitDaemon = buildpool.NewGitDaemon(buildpool.GitDaemonConfig{
			Port:       cfg.BuildPool.GitDaemonPort,
			BaseDir:    cfg.General.ProjectRoot,
			ListenAddr: cfg.BuildPool.GitDaemonListenAddr,
		})
		if err := buildPoolGitDaemon.Start(ctx); err != nil {
			fmt.Printf("Warning: failed to start git daemon: %v\n", err)
		} else {
			// Run coordinator in goroutine
			go func() {
				if err := buildPoolCoord.Start(ctx); err != nil && ctx.Err() == nil {
					fmt.Printf("Build pool coordinator error: %v\n", err)
				}
			}()
		}
	}

	// Convert recovered agents to AgentViews for TUI
	var recoveredViews []*tui.AgentView
	var stillRunning, completed int
	for _, agent := range recoveredAgents {
		recoveredViews = append(recoveredViews, &tui.AgentView{
			TaskID:       agent.TaskID.String(),
			Title:        agent.TaskID.String(), // Will be updated with actual title
			Status:       agent.Status,
			Duration:     agent.Duration(),
			WorktreePath: agent.WorktreePath,
			Output:       agent.GetOutput(),
		})
		if agent.Status == executor.AgentRunning {
			stillRunning++
		} else {
			completed++
		}
	}

	// Print recovery summary to console
	if len(recoveredAgents) > 0 {
		if stillRunning > 0 && completed > 0 {
			fmt.Printf("Recovered %d agent(s): %d still running, %d completed\n", len(recoveredAgents), stillRunning, completed)
		} else if stillRunning > 0 {
			fmt.Printf("Recovered %d running agent(s)\n", stillRunning)
		} else {
			fmt.Printf("Recovered %d completed agent(s) from previous session\n", completed)
		}
	}

	// Create plan change channel for receiving file watcher notifications
	planChangeChan := make(chan tui.PlanSyncMsg, 10)

	// Create plan watcher that sends changes to the channel
	planWatcher, err := observer.NewPlanWatcher(func(worktreePath string, changedFiles []string) {
		// Non-blocking send to avoid deadlock if channel is full
		select {
		case planChangeChan <- tui.PlanSyncMsg{
			WorktreePath: worktreePath,
			ChangedFiles: changedFiles,
		}:
		default:
			// Channel full, skip this notification
		}
	})
	if err != nil {
		fmt.Printf("Warning: failed to create plan watcher: %v\n", err)
	}

	// Start the watcher
	if planWatcher != nil {
		planWatcher.Start(ctx)
		defer planWatcher.Stop()

		// Add worktrees from recovered agents
		for _, agent := range recoveredAgents {
			if agent.WorktreePath != "" {
				planWatcher.AddWorktree(agent.WorktreePath)
			}
		}
	}

	// Build pool URL for TUI to fetch worker status
	var buildPoolURL string
	if cfg.BuildPool.Enabled {
		buildPoolURL = fmt.Sprintf("http://localhost:%d", cfg.BuildPool.WebSocketPort)
	}

	model := tui.NewModel(tui.ModelConfig{
		MaxActive:       cfg.General.MaxParallelAgents,
		AllTasks:        allTasks,
		Queued:          queued,
		ProjectRoot:     cfg.General.ProjectRoot,
		WorktreeDir:     cfg.General.WorktreeDir,
		PlansDir:        cfg.General.ProjectRoot + "/docs/plans",
		BuildPoolURL:    buildPoolURL,
		AgentManager:    agentMgr,
		RecoveredAgents: recoveredViews,
		PlanWatcher:     planWatcher,
		PlanChangeChan:  planChangeChan,
	})

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	finalModel, err := p.Run()

	// Stop build pool coordinator if it was started
	if buildPoolCoord != nil {
		buildPoolCoord.Stop()
	}
	if buildPoolGitDaemon != nil {
		buildPoolGitDaemon.Stop()
	}

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

// agentStoreAdapter wraps taskstore.Store to implement executor.AgentStore
type agentStoreAdapter struct {
	store *taskstore.Store
}

func (a *agentStoreAdapter) SaveAgentRun(run *executor.AgentRunRecord) error {
	return a.store.SaveAgentRun(&taskstore.AgentRun{
		ID:           run.ID,
		TaskID:       run.TaskID,
		WorktreePath: run.WorktreePath,
		LogPath:      run.LogPath,
		PID:          run.PID,
		Status:       run.Status,
		StartedAt:    run.StartedAt,
		FinishedAt:   run.FinishedAt,
		ErrorMessage: run.ErrorMessage,
		SessionID:    run.SessionID,
	})
}

func (a *agentStoreAdapter) UpdateAgentRunStatus(id string, status string, errorMessage string) error {
	return a.store.UpdateAgentRunStatus(id, status, errorMessage)
}

func (a *agentStoreAdapter) ListActiveAgentRuns() ([]*executor.AgentRunRecord, error) {
	runs, err := a.store.ListActiveAgentRuns()
	if err != nil {
		return nil, err
	}
	result := make([]*executor.AgentRunRecord, len(runs))
	for i, run := range runs {
		result[i] = &executor.AgentRunRecord{
			ID:           run.ID,
			TaskID:       run.TaskID,
			WorktreePath: run.WorktreePath,
			LogPath:      run.LogPath,
			PID:          run.PID,
			Status:       run.Status,
			StartedAt:    run.StartedAt,
			FinishedAt:   run.FinishedAt,
			ErrorMessage: run.ErrorMessage,
			SessionID:    run.SessionID,
		}
	}
	return result, nil
}

func (a *agentStoreAdapter) DeleteAgentRun(id string) error {
	return a.store.DeleteAgentRun(id)
}

func (a *agentStoreAdapter) UpdateAgentRunUsage(id string, tokensInput, tokensOutput int, costUSD float64) error {
	return a.store.UpdateAgentRunUsage(id, tokensInput, tokensOutput, costUSD)
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

func runBuildPoolStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if !cfg.BuildPool.Enabled {
		fmt.Println("Build pool is not enabled in config. Set build_pool.enabled = true")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create registry
	registry := buildpool.NewRegistry()

	// Set up embedded worker if enabled
	var embeddedFunc buildpool.EmbeddedWorkerFunc
	if cfg.BuildPool.LocalFallback.Enabled {
		embedded := buildpool.NewEmbeddedWorker(buildpool.EmbeddedConfig{
			RepoDir:     cfg.General.ProjectRoot,
			WorktreeDir: cfg.BuildPool.LocalFallback.WorktreeDir,
			MaxJobs:     cfg.BuildPool.LocalFallback.MaxJobs,
			UseNixShell: true,
		})
		embeddedFunc = embedded.Run
	}

	// Create dispatcher with embedded worker
	dispatcher := buildpool.NewDispatcher(registry, embeddedFunc)

	// Create coordinator
	coord := buildpool.NewCoordinator(buildpool.CoordinatorConfig{
		WebSocketPort:     cfg.BuildPool.WebSocketPort,
		HeartbeatInterval: time.Duration(cfg.BuildPool.Timeouts.HeartbeatIntervalSecs) * time.Second,
		HeartbeatTimeout:  time.Duration(cfg.BuildPool.Timeouts.HeartbeatTimeoutSecs) * time.Second,
		Debug:             cfg.BuildPool.Debug,
	}, registry, dispatcher)

	// Start git daemon
	gitDaemon := buildpool.NewGitDaemon(buildpool.GitDaemonConfig{
		Port:       cfg.BuildPool.GitDaemonPort,
		BaseDir:    cfg.General.ProjectRoot,
		ListenAddr: cfg.BuildPool.GitDaemonListenAddr,
	})
	if err := gitDaemon.Start(ctx); err != nil {
		return fmt.Errorf("starting git daemon: %w", err)
	}
	defer gitDaemon.Stop()

	fmt.Printf("Build pool coordinator starting...\n")
	fmt.Printf("  WebSocket: :%d\n", cfg.BuildPool.WebSocketPort)
	fmt.Printf("  Git daemon: :%d\n", cfg.BuildPool.GitDaemonPort)

	// Run coordinator in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- coord.Start(ctx)
	}()

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		fmt.Printf("\nReceived %v, shutting down...\n", sig)
		cancel()
		coord.Stop()
		return nil
	case err := <-errCh:
		return err
	}
}

func runBuildPoolStatus(cmd *cobra.Command, args []string) error {
	// TODO: Connect to running coordinator and query status
	fmt.Println("Build pool status:")
	fmt.Println("  (status query not yet implemented)")
	return nil
}

func runBuildPoolStop(cmd *cobra.Command, args []string) error {
	// TODO: Signal running coordinator to stop
	fmt.Println("Stopping build pool...")
	fmt.Println("  (graceful stop not yet implemented)")
	return nil
}
