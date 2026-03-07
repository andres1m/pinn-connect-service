package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"pinn/internal/config"
	"pinn/internal/docker"
	"pinn/internal/repository"
	"pinn/internal/server"
	"pinn/internal/service"
	"pinn/internal/storage"
	"pinn/internal/workspace"
	"sync"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	if err := runMigrations(cfg.DBURL); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("error while creating new pgxpool: %w", err)
	}
	defer pool.Close()

	taskRepo := repository.NewTaskRepository(pool)
	modelRepo := repository.NewModelRepository(pool)

	workspace := workspace.NewLocalWorkspace(cfg)

	modelService := service.NewModelService(modelRepo)
	taskService := service.NewTaskService(manager, storage, cfg, taskRepo, workspace, modelService)
	healthService := service.NewHealthService(manager)

	var wg sync.WaitGroup
	taskService.StartWorker(ctx, &wg)
	taskService.StartScheduler(ctx)

	// blocking Run() call
	if err := server.New(taskService, modelService, healthService, cfg).Run(ctx, cfg.ServerPort); err != nil {
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
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("Migrations applied successfully")
	return nil
}
