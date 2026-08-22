package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/config"
	"github.com/11DingKing/embodied-reading-studio/internal/httpapi"
	appmiddleware "github.com/11DingKing/embodied-reading-studio/internal/middleware"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
	"github.com/11DingKing/embodied-reading-studio/internal/worker"
)

func main() {
	appConfig, err := config.Load()
	if err != nil {
		slog.Error("configuration failed", "error", err)
		os.Exit(1)
	}
	logger := newLogger(appConfig.LogLevel)
	if err := ensureDatabaseDirectory(appConfig.DatabasePath); err != nil {
		logger.Error("database directory failed", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := sqlite.Open(ctx, appConfig.DatabasePath)
	if err != nil {
		logger.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ids := idgen.UUID{}
	appClock := clock.System{}
	dependencies := service.Dependencies{Store: store, Clock: appClock, IDs: ids}
	auth := service.AuthService{Dependencies: dependencies, SessionTTL: appConfig.SessionTTL, TouchInterval: appConfig.SessionTouch}
	if _, err := auth.BootstrapCoordinator(ctx, appConfig.TenantID, appConfig.TenantName, appConfig.BootstrapEmail, appConfig.BootstrapPassword); err != nil {
		logger.Error("bootstrap coordinator failed", "error", err)
		os.Exit(1)
	}
	catalogs := service.CatalogService{Dependencies: dependencies}
	programs := service.ProgramService{Dependencies: dependencies}
	readings := service.ReadingService{Dependencies: dependencies, SessionLeaseTTL: appConfig.WorkerLeaseTTL}
	reviews := service.ReviewService{Dependencies: dependencies, LeaseTTL: appConfig.WorkerLeaseTTL}
	seminars := service.SeminarService{Dependencies: dependencies}
	archives := service.ArchiveService{
		Dependencies: dependencies,
		LeaseTTL:     appConfig.WorkerLeaseTTL,
		MaxAttempts:  5,
		Backoff: func(attempt int) time.Duration {
			if attempt < 1 {
				attempt = 1
			}
			if attempt > 8 {
				attempt = 8
			}
			return time.Duration(1<<(attempt-1)) * time.Second
		},
	}
	middleware := appmiddleware.HTTP{Auth: auth, IDs: ids, Logger: logger, OnAuthError: httpapi.WriteAuthenticationError}
	api := httpapi.API{
		Auth: auth, Catalog: catalogs, Programs: programs, Readings: readings, Reviews: reviews,
		Seminars: seminars, Archives: archives, Store: store, Middleware: middleware, Logger: logger,
	}

	runtime := &worker.Runtime{
		Interval: appConfig.WorkerPollInterval,
		Logger:   logger,
		Tasks: []worker.Task{
			worker.Outbox{Store: store, Seminars: seminars, Archives: archives, Owner: ids.New("worker"), LeaseTTL: appConfig.WorkerLeaseTTL, MaxAttempts: 5, Clock: appClock, Logger: logger},
			worker.AssignmentExpiry{Readings: readings, Limit: appConfig.WorkerBatchSize, Logger: logger},
		},
	}
	runtime.Start(ctx)

	server := &http.Server{
		Addr:              appConfig.ListenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errChannel := make(chan error, 1)
	go func() {
		logger.Info("server listening", "address", appConfig.ListenAddr)
		errChannel <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errChannel:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			cancel()
		}
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), appConfig.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("http shutdown failed", "error", err)
	}
	if err := runtime.Stop(shutdownContext); err != nil {
		logger.Error("worker shutdown failed", "error", err)
	}
}

func ensureDatabaseDirectory(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil
	}
	directory := filepath.Dir(strings.TrimPrefix(path, "file:"))
	return os.MkdirAll(directory, 0o750)
}

func newLogger(level string) *slog.Logger {
	parsed := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		parsed = slog.LevelDebug
	case "warn":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed}))
}
