package main

import (
	"context"
	"log"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	manager, err := docker.NewManager(ctx)
	if err != nil {
		log.Fatalf("Error while initializing Docker client: %v", err)
	}

	storage, err := storage.NewMinIOStorage(ctx, cfg)
	if err != nil {
		log.Fatalf("Error while initializing minio storage: %v", err)
	}

	taskService := service.NewTaskService(manager, storage, cfg)
	healthService := service.NewHealthService(manager)

	if err := server.New(taskService, healthService).Run(ctx, ":8080"); err != nil {
		log.Fatalf("Server stopped with error: %v", err)
	}

	<-ctx.Done()
	if err := manager.Client.Close(); err != nil {
		log.Fatalf("Docker stopped with error: %v", err)
	}

	slog.Info("correct shutdown")
}
