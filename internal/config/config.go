// Package config loads application configuration from files and environment variables.
package config

import (
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds the application configuration values.
type Config struct {
	Port        int    `koanf:"port"`
	LogLevel    string `koanf:"log_level"`
	DatabaseURL string `koanf:"database_url"`
}

// Load reads configuration from config.yaml and environment variables (APP_ prefix).
func Load() (*Config, error) {
	k := koanf.New(".")

	// Load from config file if it exists
	if err := k.Load(file.Provider("config.yaml"), yaml.Parser()); err != nil {
		// Config file is optional, ignore error
		_ = err
	}

	// Environment variables override file config (prefix: APP_)
	if err := k.Load(env.Provider("APP_", ".", func(s string) string {
		return s[4:] // strip APP_ prefix
	}), nil); err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:        8080,
		LogLevel:    "info",
		DatabaseURL: "postgres://postgres:postgres@localhost:5432/arx?sslmode=disable",
	}

	if err := k.Unmarshal("", cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
