package docker

type RunOption func(*RunOptions)

type RunOptions struct {
	Env         []string
	Volumes     []string
	Cmd         []string
	MemoryLimit int
	CPULimit    int
	GPU         bool
}

func defaultRunOptions() *RunOptions {
	return &RunOptions{
		Env:         []string{},
		Volumes:     []string{},
		Cmd:         []string{},
		MemoryLimit: 100,
		CPULimit:    100,
		GPU:         false,
	}
}

func WithEnvs(envs []string) RunOption {
	return func(opts *RunOptions) {
		if envs == nil {
			return
		}

		opts.Env = envs
	}
}

func WithVolumes(volumes []string) RunOption {
	return func(opts *RunOptions) {
		if volumes == nil {
			return
		}

		opts.Volumes = volumes
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
