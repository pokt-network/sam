package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/cors"

	"github.com/pokt-network/sam/internal/autotopup"
	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/handler"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/pocket"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("starting SAM", "version", version)

	configPath := os.Getenv("CONFIG_FILE")
	if configPath == "" {
		configPath = "config.yaml"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// AUTH_TOKEN env var takes precedence over config.yaml auth settings.
	if authToken := os.Getenv("AUTH_TOKEN"); authToken != "" {
		cfg.Config.Auth.Enabled = true
		cfg.Config.Auth.Token = authToken
	}

	if cfg.Config.Auth.Enabled {
		if cfg.Config.Auth.Token == "" {
			logger.Error("auth is enabled but no token is configured (set auth.token in config.yaml or AUTH_TOKEN env var)")
			os.Exit(1)
		}
		logger.Info("API authentication enabled for write endpoints")
	}

	networks := make([]string, 0, len(cfg.Config.Networks))
	for k := range cfg.Config.Networks {
		networks = append(networks, k)
	}
	logger.Info("configuration loaded", "networks", networks)

	if path, err := exec.LookPath("pocketd"); err != nil {
		logger.Warn("pocketd not found in PATH; transactions will fail")
	} else {
		logger.Info("pocketd found", "path", path)
	}

	client := pocket.NewClient(logger)
	executor, err := pocket.NewExecutor(cfg, client, logger)
	if err != nil {
		logger.Error("failed to initialize executor", "error", err)
		os.Exit(1)
	}

	appCache := cache.New[[]models.Application](1 * time.Minute)
	bankCache := cache.New[models.BankAccount](1 * time.Minute)

	topUpStore, err := autotopup.NewStore(filepath.Join(dataDir, "autotopup.json"))
	if err != nil {
		logger.Error("failed to initialize auto-top-up store", "error", err)
		os.Exit(1)
	}

	worker := autotopup.NewWorker(topUpStore, cfg, client, executor, appCache, bankCache, logger)

	srv := &handler.Server{
		Config:     cfg,
		ConfigPath: configPath,
		Client:     client,
		Executor:   executor,
		AppCache:   appCache,
		BankCache:  bankCache,
		AutoTopUp:  topUpStore,
		Worker:     worker,
		Logger:     logger,
	}

	r := mux.NewRouter()
	r.Use(handler.SecurityHeaders())
	r.Use(handler.RequestLogger(logger))
	srv.SetupRoutes(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "9999"
	}
	if _, err := strconv.Atoi(port); err != nil {
		logger.Error("invalid PORT value", "port", port)
		os.Exit(1)
	}

	allowedOrigins := []string{
		"http://localhost:" + port,
		"http://127.0.0.1:" + port,
	}
	if originsEnv := os.Getenv("ALLOWED_ORIGINS"); originsEnv != "" {
		allowedOrigins = nil
		for _, o := range strings.Split(originsEnv, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}
	logger.Info("CORS configuration", "allowed_origins", allowedOrigins)

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	})

	httpServer := &http.Server{
		Handler:      corsHandler.Handler(r),
		Addr:         ":" + port,
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
	}

	// Start auto-top-up worker.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	go worker.Run(workerCtx)

	// Graceful shutdown.
	done := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)

		// Stop worker before HTTP server.
		workerCancel()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
		close(done)
	}()

	logger.Info("starting SAM server",
		"port", port,
		"api", "http://localhost:"+port+"/api",
		"health", "http://localhost:"+port+"/health",
	)

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	<-done
	logger.Info("server stopped")
}
