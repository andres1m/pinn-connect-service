package sysstats

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"
)

func TestGetHostResources(t *testing.T) {
	// Устанавливаем известное значение CPU для проверки
	expectedCPU := 25.5
	cpuUsageBits.Store(math.Float64bits(expectedCPU))

	res, err := GetHostResources()
	if err != nil {
		t.Fatalf("GetHostResources() returned error: %v", err)
	}

	if res == nil {
		t.Fatal("GetHostResources() returned nil")
	}

	if res.CPUUtilization != expectedCPU {
		t.Errorf("expected CPU utilization %f, got %f", expectedCPU, res.CPUUtilization)
	}

	// Проверяем, что память возвращается (обычно > 0 на живой системе)
	if res.AvailableMemoryBytes == 0 {
		t.Log("Warning: AvailableMemoryBytes is 0, which is unusual for a running system")
	}
}

func TestStartCPULoadFetcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Используем очень короткий интервал для ускорения теста
	interval := 10 * time.Millisecond
	StartCPULoadFetcher(ctx, interval, &wg)

	// Даем время на выполнение хотя бы одной итерации
	time.Sleep(50 * time.Millisecond)

	// Проверяем, что значение в атомарной переменной считывается без паник
	// (Само значение зависит от системы, поэтому проверяем только работоспособность механизма)
	val := math.Float64frombits(cpuUsageBits.Load())
	t.Logf("CPU utilization captured in test: %f", val)

	// Завершаем контекст и проверяем корректную остановку
	cancel()

	// Ожидаем завершения WaitGroup с таймаутом
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Успешно завершено
	case <-time.After(500 * time.Millisecond):
		t.Error("StartCPULoadFetcher did not stop after context cancellation")
	}
}
