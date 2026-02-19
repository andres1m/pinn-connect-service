package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"pinn/internal/docker"
	"pinn/internal/server"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
)

func main() {
	manager, err := docker.NewManager()
	if err != nil {
		log.Fatalf("Error while initializing Docker client: %v", err)
	}

	test_docker(manager)

	log.Fatal(server.New(manager).Run(":8080"))
}

func test_docker(manager *docker.Manager) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("Запуск контейнера...")
	id, err := manager.StartContainer(ctx, "alpine",
		docker.WithEnvs([]string{"TEST_VAR=hello_pinn"}),
		docker.WithCmds([]string{"sh", "-c", "echo 'started'; sleep 2; echo 'completed'"}),
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
