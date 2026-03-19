package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"pinn-connect-service/internal/config"
	"pinn-connect-service/internal/db"
	"pinn-connect-service/internal/docker"
	"pinn-connect-service/internal/gc"
	"pinn-connect-service/internal/repository"
	"pinn-connect-service/internal/server"
	"pinn-connect-service/internal/service"
	"pinn-connect-service/internal/storage"
	"pinn-connect-service/internal/sysstats"
	"pinn-connect-service/internal/workspace"
	"sync"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/jackc/pgx/v5/pgxpool"
)

// @title           Pinn API
// @version         1.0
// @description     Pinn Server API for distributed task execution and model building.
// @host            localhost:8080
// @BasePath        /
func main() {
	slog.SetDefault(slog.Default())

	if err := run(); err != nil {
		slog.Error("application stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	manager, err := docker.NewManager(ctx)
	if err != nil {
		return fmt.Errorf("error while initializing Docker client: %w", err)
	}
	defer func() {
		slog.Info("closing Docker client...")
		if err := manager.Client.Close(); err != nil {
			slog.Error("failed to close Docker client", "error", err)
		}
	}()

	storage, err := storage.NewMinIOStorage(ctx, cfg)
	if err != nil {
		return fmt.Errorf("error while initializing minio storage: %w", err)
	}

	if err := runMigrations(cfg.DB.URL); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.DB.URL)
	if err != nil {
		return fmt.Errorf("error while creating new pgxpool: %w", err)
	}
	defer pool.Close()

	taskRepo := repository.NewTaskRepository(pool)
	modelRepo := repository.NewModelRepository(pool)

	workspace := workspace.NewLocalWorkspace(cfg)

	modelService := service.NewModelService(modelRepo, manager)
	taskService := service.NewTaskService(manager, storage, cfg, taskRepo, workspace, modelService)
	healthService := service.NewHealthService(manager, storage, &db.PostgresDatabasePinger{Pool: pool})

	gcCtx, gcCancel := context.WithTimeout(ctx, 20*time.Second)
	gc := gc.NewGarbageCollector(taskRepo, workspace, manager, storage, taskService)
	gc.Cleanup(gcCtx)
	gcCancel()

	var wg sync.WaitGroup
	taskService.StartWorker(ctx, &wg)
	taskService.StartScheduler(ctx, &wg)

	sysstats.StartCPULoadFetcher(ctx, cfg.SysstatsCPUInterval, &wg)

	// blocking Run() call
	if err := server.New(taskService, modelService, healthService, cfg).Run(ctx, cfg.Server.Port); err != nil {
		return fmt.Errorf("server stopped with error: %w", err)
	}

	wg.Wait()

	return nil
}

func runMigrations(dbURL string) error {
	m, err := migrate.New(
		"file://migrations",
		dbURL,
	)
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}

	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			slog.Error("failed to close migrate source", "error", sourceErr)
		}
		if dbErr != nil {
			slog.Error("failed to close migrate db connection", "error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("running migrations: %w", err)
	}

	slog.Info("Migrations applied successfully")
	return nil
}
