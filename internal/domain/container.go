package domain

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type ContainerConfig struct {
	Image       string
	Mounts      []Mount
	Envs        []string
	Cmd         []string
	MemoryLimit int
	CPULimit    int
	GPU         bool
}
