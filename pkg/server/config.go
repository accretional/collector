package server

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Server ServerConfig `yaml:"server"`
}

type ServerConfig struct {
	DataDir     string `yaml:"data_dir"`
	Port        int    `yaml:"port"`
	Namespace   string `yaml:"namespace"`
	CollectorID string `yaml:"collector_id"`
	HealthPort  int    `yaml:"health_port"`
}

func LoadConfig(path string) (*ServerConfig, error) {
	cfg := &ServerConfig{
		DataDir:    "./data",
		Port:       50051,
		Namespace:  "shared",
		HealthPort: 8080,
	}

	if path == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			path = "config.yaml"
		}
	}

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var fileCfg fileConfig
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if fileCfg.Server.DataDir != "" {
		cfg.DataDir = fileCfg.Server.DataDir
	}
	if fileCfg.Server.Port != 0 {
		cfg.Port = fileCfg.Server.Port
	}
	if fileCfg.Server.Namespace != "" {
		cfg.Namespace = fileCfg.Server.Namespace
	}
	if fileCfg.Server.CollectorID != "" {
		cfg.CollectorID = fileCfg.Server.CollectorID
	}
	if fileCfg.Server.HealthPort != 0 {
		cfg.HealthPort = fileCfg.Server.HealthPort
	}

	return cfg, nil
}
