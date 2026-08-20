// Package config loads the engine's YAML configuration.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Bidder struct {
	SeatID   string `yaml:"seat_id"`
	Endpoint string `yaml:"endpoint"`
}

type Config struct {
	ListenAddr       string   `yaml:"listen_addr"`
	MetricsAddr      string   `yaml:"metrics_addr"`
	SemaphoreCeiling int      `yaml:"semaphore_ceiling"`
	Bidders          []Bidder `yaml:"bidders"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.MetricsAddr == "" {
		cfg.MetricsAddr = ":9090"
	}
	if cfg.SemaphoreCeiling == 0 {
		cfg.SemaphoreCeiling = 256
	}

	return &cfg, nil
}
