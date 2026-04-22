// Package database manages PostgreSQL connection pooling.
package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// ParseConfig parses a database URL into a pgxpool configuration.
func ParseConfig(databaseURL string) (*pgxpool.Config, error) {
	return pgxpool.ParseConfig(databaseURL)
}

// Connect establishes a connection pool to the database and verifies connectivity.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	log.Info().Msg("connected to database")
	return pool, nil
}
