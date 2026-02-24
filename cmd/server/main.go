package main

import (
	"context"
	"fmt"
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

	"github.com/docker/docker/pkg/stdcopy"
)

func main() {
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
		slog.Error("Server stopped with error", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	if err := manager.Client.Close(); err != nil {
		slog.Error("Docker stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("correct shutdown")
}

func test_docker(ctx context.Context, manager *docker.Manager) {
	fmt.Println("Запуск контейнера...")
	id, err := manager.StartContainer(ctx, "nvidia/cuda:11.0.3-base-ubuntu20.04",
		docker.WithEnvs("TEST_VAR=hello_pinn"),
		docker.WithCmds([]string{"sh", "-c", "echo 'started'; sleep 2; echo 'completed'"}),
		docker.WithGPU(true),
	)
	if err != nil {
		log.Fatalf("Ошибка при запуске контейнера: %v", err)
	}

	fmt.Printf("Контейнер успешно запущен! ID: %s\n", id)

	logs, err := manager.GetContainerLogs(ctx, id, true)
	if err != nil {
		log.Fatalf("Ошибка при чтении логов: %v", err)
	}
	defer logs.Close()

	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, logs)
	if err != nil {
		log.Fatalf("Ошибка при перенаправлении логов в консоль: %v", err)
	}

	manager.RemoveContainer(ctx, id)
}
