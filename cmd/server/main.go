package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"pinn/internal/config"
	"pinn/internal/docker"
	"pinn/internal/server"
	"pinn/internal/service"
	"pinn/internal/storage"
	"syscall"
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

	taskService := service.NewTaskService(manager, storage, cfg)
	healthService := service.NewHealthService(manager)

	// blocking Run() call
	if err := server.New(taskService, healthService).Run(ctx, cfg.ServerPort); err != nil {
		return fmt.Errorf("server stopped with error: %w", err)
	}

	return nil
}
