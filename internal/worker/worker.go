// Package worker manages background job processing with River.
package worker

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/rs/zerolog/log"
)

// NewConfig builds the River client configuration with default queue settings.
func NewConfig() (*river.Config, *river.Workers) {
	workers := river.NewWorkers()

	// Register workers here
	// river.AddWorker(workers, &YourWorker{})

	cfg := &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers: workers,
	}

	return cfg, workers
}

// Start initializes and starts the River worker client.
func Start(ctx context.Context, pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	cfg, _ := NewConfig()

	riverClient, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, err
	}

	if err := riverClient.Start(ctx); err != nil {
		return nil, err
	}

	log.Info().Msg("river worker started")
	return riverClient, nil
}
