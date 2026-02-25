package config

import "github.com/caarlos0/env/v11"

type Config struct {
	TmpDir         string `env:"TMP_DIR"`
	MockDir        string `env:"MOCK_DIR" env_default:"./mock"`
	MinIOEndpoint  string `env:"MINIO_ENDPOINT"`
	MinIOAccessKey string `env:"MINIO_ACCESS_KEY"`
	MinIOSecretKey string `env:"MINIO_SECRET_KEY"`
	MinIOSSLUse    bool   `env:"MINIO_USE_SSL"`
	MinIOBucket    string `env:"MINIO_BUCKET"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
