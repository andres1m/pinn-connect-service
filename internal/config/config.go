package config

import "github.com/caarlos0/env/v11"

type Config struct {
	DBURL string `env:"DB_URL"`

	TmpDir     string `env:"TMP_DIR"`
	MockDir    string `env:"MOCK_DIR" envDefault:"./mock"`
	MaxWorkers int    `env:"MAX_WORKERS" envDefault:"5"`

	MinIOEndpoint  string `env:"MINIO_ENDPOINT"`
	MinIOAccessKey string `env:"MINIO_ACCESS_KEY"`
	MinIOSecretKey string `env:"MINIO_SECRET_KEY"`
	MinIOSSLUse    bool   `env:"MINIO_USE_SSL"`
	MinIOBucket    string `env:"MINIO_BUCKET"`

	ServerPort string `env:"SERVER_PORT" envDefault:":8080"`

	MaxMem int `env:"MAX_MEM" envDefault:"512"`
	MaxCPU int `env:"MAX_CPU" envDefault:"50"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
