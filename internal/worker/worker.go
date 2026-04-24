// Package worker manages background job processing with River.
package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/rs/zerolog/log"
)

// HealthArgs are the arguments for the health check worker.
type HealthArgs struct{}

// Kind returns the unique job kind identifier for health check jobs.
func (HealthArgs) Kind() string { return "health_check" }

// HealthWorker is a no-op worker used to satisfy River's requirement
// that at least one worker is registered.
type HealthWorker struct {
	river.WorkerDefaults[HealthArgs]
}

// Work executes the health check job, which is a no-op.
func (w *HealthWorker) Work(_ context.Context, _ *river.Job[HealthArgs]) error {
	return nil
}

// NewConfig builds the River client configuration with default queue settings.
func NewConfig() (*river.Config, *river.Workers) {
	workers := river.NewWorkers()

	river.AddWorker(workers, &HealthWorker{})

	cfg := &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers: workers,
	}

	return cfg, workers
}

// Start initializes and starts the River worker client.
// It runs River's schema migrations before starting the client.
func Start(ctx context.Context, pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}

	driver := riverpgxv5.New(pool)

	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		return nil, fmt.Errorf("create river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("run river migrations: %w", err)
	}

	cfg, _ := NewConfig()

	riverClient, err := river.NewClient(driver, cfg)
	if err != nil {
		return nil, err
	}

	if err := riverClient.Start(ctx); err != nil {
		return nil, err
	}

	log.Info().Msg("river worker started")
	return riverClient, nil
}
