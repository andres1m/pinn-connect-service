package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"pinn/internal/docker"
	"pinn/internal/handler"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	test_docker()
}

func run_routers() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/health", handler.Health)
	r.Post("/run", handler.Run)
	r.Get("/status", handler.Status)
	log.Fatal(http.ListenAndServe(":8080", r))
}

func test_docker() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	manager, err := docker.NewManager()
	if err != nil {
		log.Fatalf("Ошибка инициализации Docker клиента: %v", err)

	}

	opts := &docker.RunOptions{
		Image: "alpine",
		Env:   []string{"TEST_VAR=hello_pinn"},
		Cmd:   []string{"sh", "-c", "echo 'started'; sleep 2; echo 'finished'"},
	}

	fmt.Println("Запуск контейнера...")
	id, err := manager.StartContainer(ctx, opts)
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
