package config

import "github.com/caarlos0/env/v11"

type Config struct {
	TmpDir  string `env:"TMP_DIR"`
	MockDir string `env:"MOCK_DIR" env_default:"/home/andres1m/go-projects/kursach/mock"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
