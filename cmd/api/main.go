// Package main is the entrypoint for the API server.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/config"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/database"
	"github.com/fambr/arx/internal/handler"
	"github.com/fambr/arx/internal/token"
	"github.com/fambr/arx/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("application error")
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	setupLogger(cfg.LogLevel)

	return serve(cfg)
}

func serve(cfg *config.Config) error {
	pool, err := database.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	riverClient, err := worker.Start(context.Background(), pool)
	if err != nil {
		return fmt.Errorf("start river worker: %w", err)
	}

	masterKey := os.Getenv("ARX_MASTER_KEY")
	if masterKey == "" {
		return fmt.Errorf("ARX_MASTER_KEY env var is required")
	}

	keyManager, err := crypto.NewLocalKeyManager(masterKey)
	if err != nil {
		return fmt.Errorf("create key manager: %w", err)
	}

	cacheClient, err := cache.Connect(context.Background(), cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer func() { _ = cacheClient.Close() }()

	issuer := token.NewIssuer(token.DefaultConfig(cfg.Issuer), keyManager)

	r := newRouter(pool, riverClient, cacheClient, keyManager, issuer, cfg.TestMode)
	srv := newServer(cfg.Port, r)

	go func() {
		log.Info().Int("port", cfg.Port).Msg("server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := riverClient.Stop(ctx); err != nil {
		log.Error().Err(err).Msg("river shutdown error")
	}
	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}

	return nil
}

func newRouter(
	db *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
	cacheClient *cache.Client,
	keyManager crypto.KeyManager,
	issuer *token.Issuer,
	testMode bool,
) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	handler.Register(r, db, riverClient, cacheClient, keyManager, issuer, testMode)
	return r
}

func newServer(port int, h http.Handler) *http.Server {
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      h,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func setupLogger(level string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()
}
