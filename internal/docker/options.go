package docker

type RunOption func(*RunOptions)

type RunOptions struct {
	Env     []string
	Volumes []string
	Cmd     []string
}

func defaultRunOptions() *RunOptions {
	return &RunOptions{
		Env:     []string{},
		Volumes: []string{},
		Cmd:     []string{},
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
