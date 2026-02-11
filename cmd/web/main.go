package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/cors"

	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/handler"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/pocket"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load("config.yaml")
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
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
	executor := pocket.NewExecutor(cfg, client, logger)

	srv := &handler.Server{
		Config:    cfg,
		Client:    client,
		Executor:  executor,
		AppCache:  cache.New[[]models.Application](1 * time.Minute),
		BankCache: cache.New[models.BankAccount](1 * time.Minute),
		Logger:    logger,
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

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:" + port,
			"http://127.0.0.1:" + port,
		},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	})

	httpServer := &http.Server{
		Handler:      corsHandler.Handler(r),
		Addr:         ":" + port,
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
	}

	// Graceful shutdown.
	done := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)

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
