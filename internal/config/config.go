package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/caarlos0/env/v11"
)

type DatabaseConfig struct {
	URL string `env:"URL,required"`
}

type MinIOConfig struct {
	Endpoint  string `env:"ENDPOINT,required"`
	AccessKey string `env:"ACCESS_KEY,required"`
	SecretKey string `env:"SECRET_KEY,required"`
	SSLUse    bool   `env:"USE_SSL" envDefault:"false"`
	Bucket    string `env:"BUCKET" envDefault:"tasks"`
}

type ServerConfig struct {
	Port                   string        `env:"PORT" envDefault:":8080"`
	DefaultTaskStopTimeout time.Duration `env:"DEFAULT_TASK_STOP_TIMEOUT" envDefault:"5s"`
	APIToken               string        `env:"API_TOKEN"` // e.g. SERVER_API_TOKEN=secret
}

type SchedulerConfig struct {
	Interval    time.Duration `env:"INTERVAL" envDefault:"20s"`
	TaskExpires time.Duration `env:"TASK_EXPIRES" envDefault:"30s"`
}

type WorkerConfig struct {
	MaxWorkers                int           `env:"MAX_WORKERS" envDefault:"5"`
	Interval                  time.Duration `env:"WORKER_INTERVAL" envDefault:"2s"`
	ProcessTaskCleanupTimeout time.Duration `env:"PROCESS_TASK_CLEANUP_TIMEOUT" envDefault:"10s"`
}

type Config struct {
	DB        DatabaseConfig  `envPrefix:"DB_"`
	MinIO     MinIOConfig     `envPrefix:"MINIO_"`
	Server    ServerConfig    `envPrefix:"SERVER_"`
	Scheduler SchedulerConfig `envPrefix:"SCHEDULER_"`
	Worker    WorkerConfig

	TmpDir  string `env:"TMP_DIR" envDefault:"./tmp"`
	MockDir string `env:"MOCK_DIR" envDefault:"./mock"`

	MaxMemByTask int `env:"MAX_MEM_BY_TASK" envDefault:"512"`
	MaxCPUByTask int `env:"MAX_CPU_BY_TASK" envDefault:"50"`

	SysstatsCPUInterval time.Duration `env:"SYSSTATS_CPU_INTERVAL" envDefault:"1s"`

	DefaultTaskTimeoutSec int `env:"DEFAULT_TASK_TIMEOUT_SEC" envDefault:"3600"`

	WorkspaceDirsPermStr string      `env:"WORKSPACE_DIRS_PERM" envDefault:"0755"`
	WorkspaceDirsPerm    os.FileMode `env:"-"`
}

func (c *Config) Validate() error {
	if c.Worker.MaxWorkers <= 0 {
		return fmt.Errorf("MAX_WORKERS must be greater than 0, got: %d", c.Worker.MaxWorkers)
	}
	if c.MaxMemByTask <= 0 {
		return fmt.Errorf("MAX_MEM_BY_TASK must be greater than 0, got: %d", c.MaxMemByTask)
	}
	if c.MaxCPUByTask <= 0 {
		return fmt.Errorf("MAX_CPU_BY_TASK must be greater than 0, got: %d", c.MaxCPUByTask)
	}
	if c.Worker.Interval <= 0 {
		return fmt.Errorf("WORKER_INTERVAL must be positive")
	}
	if c.Scheduler.Interval <= 0 {
		return fmt.Errorf("SCHEDULER_INTERVAL must be positive")
	}

	if c.Server.Port == "" || c.Server.Port[0] != ':' {
		return fmt.Errorf("SERVER_PORT must start with colon (e.g. ':8080')")
	}

	return nil
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing env config: %w", err)
	}

	perm, err := strconv.ParseUint(cfg.WorkspaceDirsPermStr, 8, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid WORKSPACE_DIRS_PERM format, expected octal (e.g., 0755): %w", err)
	}
	cfg.WorkspaceDirsPerm = os.FileMode(perm)

	return cfg, nil
}
