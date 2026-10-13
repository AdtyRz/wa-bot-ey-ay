package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"temanwa-ai/internal/ai/gemini"
	"temanwa-ai/internal/ai/prompts"
	"temanwa-ai/internal/ai/trends"
	"temanwa-ai/internal/commands"
	"temanwa-ai/internal/config"
	"temanwa-ai/internal/conversation"
	"temanwa-ai/internal/media"
	"temanwa-ai/internal/observability"
	"temanwa-ai/internal/queue"
	"temanwa-ai/internal/storage"
	"temanwa-ai/internal/storage/postgres"
	"temanwa-ai/internal/webhook"
	"temanwa-ai/internal/whatsapp"
)

func main() {
	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. Setup logger
	logger := observability.SetupLogger(cfg.LogLevel)
	logger.Info("Starting TemanWA AI Bot", "env", cfg.AppEnv, "budget_mode", cfg.AIBudgetMode, "model", cfg.GeminiModel)

	// 3. Setup context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 4. Initialize storage repository (Supabase Postgres or in-memory fallback)
	var repo storage.Repository
	if cfg.SupabaseDBURL != "" {
		pgRepo, err := postgres.NewPostgresRepo(ctx, cfg.SupabaseDBURL)
		if err != nil {
			logger.Error("Failed to connect to Supabase PostgreSQL, falling back to memory repository", "error", err)
			repo = storage.NewMemoryRepo()
		} else {
			logger.Info("Connected to Supabase PostgreSQL")
			repo = pgRepo
		}
	} else {
		logger.Warn("SUPABASE_DB_URL not configured, running with in-memory storage")
		repo = storage.NewMemoryRepo()
	}
	defer repo.Close()

	// 5. Initialize media manager
	mediaMgr, err := media.NewManager(cfg)
	if err != nil {
		logger.Error("Failed to initialize media manager", "error", err)
		os.Exit(1)
	}
	mediaMgr.StartTTLCleanup(ctx)

	// 6. Initialize GOWA client
	waClient := whatsapp.NewGOWAClient(cfg.GOWABaseURL, cfg.GOWAUsername, cfg.GOWAPassword, cfg.GOWADeviceID, 20*time.Second)

	// 7. Initialize Gemini AI Client
	aiClient := gemini.NewGeminiClient(cfg)

	// 8. Initialize Prompts manager
	promptMgr, err := prompts.NewManager("prompts/persona_id.md")
	if err != nil {
		logger.Warn("Could not load external persona_id.md, using embedded fallback", "error", err)
	}

	// 9. Initialize Commands handler & Trends service
	cmdHandler := commands.NewHandler(cfg, repo)
	trendService := trends.NewTrendService(cfg, repo)

	// 10. Initialize Conversation Orchestrator
	orchestrator := conversation.NewOrchestrator(
		cfg,
		repo,
		waClient,
		aiClient,
		mediaMgr,
		promptMgr,
		cmdHandler,
		trendService,
	)

	// 11. Bounded Worker Pool
	workerPool := queue.NewWorkerPool(cfg.AIMaxConcurrentRequests*2, 200)
	defer workerPool.Stop()

	// 12. Setup HTTP Server and Webhook Handler
	webhookHandler := webhook.NewHandler(cfg, orchestrator, workerPool)

	mux := http.NewServeMux()
	webhookPath := cfg.GOWAWebhookPath
	if webhookPath == "" {
		webhookPath = "/webhooks/gowa"
	}
	mux.Handle(webhookPath, webhookHandler)
	mux.HandleFunc("/health", webhook.HealthHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"temanwa-ai","message":"TemanWA AI Bot is running"}`))
	})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("HTTP server listening", "port", cfg.Port, "webhook_path", webhookPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("Shutting down server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server shutdown failed", "error", err)
	}

	logger.Info("TemanWA AI Bot stopped cleanly")
}
