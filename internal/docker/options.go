package docker

import (
	"github.com/docker/docker/api/types/mount"
)

type RunOption func(*RunOptions)

type RunOptions struct {
	Env         []string
	Mounts      []mount.Mount
	Cmd         []string
	MemoryLimit int
	CPULimit    int
	GPU         bool
}

func defaultRunOptions() *RunOptions {
	return &RunOptions{
		Env:         []string{},
		Mounts:      []mount.Mount{},
		Cmd:         []string{},
		MemoryLimit: 100,
		CPULimit:    100,
		GPU:         false,
	}
}

func WithEnvs(envs ...string) RunOption {
	return func(opts *RunOptions) {
		if envs == nil {
			return
		}

		opts.Env = envs
	}
}

func WithMounts(mounts ...mount.Mount) RunOption {
	return func(opts *RunOptions) {
		if mounts == nil {
			return
		}

		opts.Mounts = mounts
	}
}

func WithCmds(cmd []string) RunOption {
	return func(opts *RunOptions) {
		if cmd == nil {
			return
		}

		opts.Cmd = cmd
	}
}

func WithMemoryLimit(limit int) RunOption {
	return func(opts *RunOptions) {
		if limit <= 0 {
			return
		}

		opts.MemoryLimit = limit
	}
}

func WithCPULimit(limit int) RunOption {
	return func(opts *RunOptions) {
		if limit <= 0 {
			return
		}

		opts.CPULimit = limit
	}
}

func WithGPU(enable bool) RunOption {
	return func(opts *RunOptions) {
		opts.GPU = enable
	}
}
