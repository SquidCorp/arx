package database

import (
	"context"
	"testing"
)

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig("postgres://user:pass@localhost:5432/testdb?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConfig() error: %v", err)
	}

	if cfg.ConnConfig.Host != "localhost" {
		t.Errorf("expected host localhost, got %s", cfg.ConnConfig.Host)
	}
	if cfg.ConnConfig.Port != 5432 {
		t.Errorf("expected port 5432, got %d", cfg.ConnConfig.Port)
	}
	if cfg.ConnConfig.Database != "testdb" {
		t.Errorf("expected database testdb, got %s", cfg.ConnConfig.Database)
	}
}

func TestParseConfigInvalid(t *testing.T) {
	_, err := ParseConfig("not-a-valid-url:::///")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestConnectInvalidURL(t *testing.T) {
	_, err := Connect(context.Background(), "not-a-valid-url:::///")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestConnectUnreachable(t *testing.T) {
	// Valid URL format but unreachable host — tests the pool creation/ping error path.
	_, err := Connect(context.Background(), "postgres://user:pass@127.0.0.1:1/testdb?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Error("expected error for unreachable database")
	}
}
