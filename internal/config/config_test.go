package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log_level info, got %s", cfg.LogLevel)
	}
	if cfg.DatabaseURL == "" {
		t.Error("expected non-empty database URL")
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log_level debug, got %s", cfg.LogLevel)
	}
}

func TestLoadFromYAMLFile(t *testing.T) {
	content := []byte("port: 3000\nlog_level: warn\n")
	if err := os.WriteFile("config.yaml", content, 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove("config.yaml") }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Port)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("expected log_level warn, got %s", cfg.LogLevel)
	}
}
