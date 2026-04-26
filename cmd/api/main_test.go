package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fambr/arx/internal/config"
	"github.com/fambr/arx/internal/token"
	"github.com/rs/zerolog"
)

func TestNewRouter(t *testing.T) {
	issuer := token.NewIssuer(token.DefaultConfig("http://localhost:8080"), nil)
	r := newRouter(nil, nil, nil, nil, issuer, false)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestNewServer(t *testing.T) {
	srv := newServer(9999, http.NewServeMux())

	if srv.Addr != ":9999" {
		t.Errorf("expected addr :9999, got %s", srv.Addr)
	}
	if srv.ReadTimeout == 0 {
		t.Error("expected non-zero read timeout")
	}
	if srv.WriteTimeout == 0 {
		t.Error("expected non-zero write timeout")
	}
	if srv.IdleTimeout == 0 {
		t.Error("expected non-zero idle timeout")
	}
}

func TestSetupLogger(t *testing.T) {
	setupLogger("debug")
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("expected debug level, got %s", zerolog.GlobalLevel())
	}

	setupLogger("warn")
	if zerolog.GlobalLevel() != zerolog.WarnLevel {
		t.Errorf("expected warn level, got %s", zerolog.GlobalLevel())
	}

	setupLogger("invalid")
	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Errorf("expected info level for invalid input, got %s", zerolog.GlobalLevel())
	}
}

func TestRunFailsWithoutDB(t *testing.T) {
	err := run()
	if err == nil {
		t.Error("expected error from run() without a database")
	}
}

func TestServeInvalidDBURL(t *testing.T) {
	cfg := &config.Config{
		Port:        8080,
		LogLevel:    "info",
		DatabaseURL: "invalid:::///",
	}
	err := serve(cfg)
	if err == nil {
		t.Error("expected error for invalid database URL")
	}
}

func TestServeUnreachableDB(t *testing.T) {
	cfg := &config.Config{
		Port:        8080,
		LogLevel:    "info",
		DatabaseURL: "postgres://user:pass@127.0.0.1:1/testdb?sslmode=disable&connect_timeout=1",
	}
	err := serve(cfg)
	if err == nil {
		t.Error("expected error for unreachable database")
	}
}
