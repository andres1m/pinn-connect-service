package sysstats

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type HostResources struct {
	AvailableMemoryBytes uint64
	CPUUtilization       float64
}

var cpuUsageBits atomic.Uint64

func GetHostResources() (*HostResources, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return nil, fmt.Errorf("getting virtual memory: %w", err)
	}

	currentCPU := math.Float64frombits(cpuUsageBits.Load())

	return &HostResources{
		AvailableMemoryBytes: v.Available,
		CPUUtilization:       currentCPU,
	}, nil
}

func StartCPULoadFetcher(ctx context.Context, interval time.Duration, wg *sync.WaitGroup) {
	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		_, _ = cpu.PercentWithContext(ctx, 0, false)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				percentages, err := cpu.PercentWithContext(ctx, 0, false)
				if err != nil || len(percentages) == 0 {
					slog.Error("getting cpu percentage", "error", err)
					continue
				}

				cpuUsageBits.Store(math.Float64bits(percentages[0]))
			}
		}
	})
}
