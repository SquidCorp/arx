package oauth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// CleanupArgs are the arguments for the expired OAuth flow cleanup worker.
type CleanupArgs struct{}

// Kind returns the unique job kind identifier for OAuth flow cleanup jobs.
func (CleanupArgs) Kind() string { return "oauth_flow_cleanup" }

// InsertOpts configures the job as a periodic cron (every 5 minutes).
func (CleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: "default",
	}
}

// CleanupWorker expires pending OAuth flows that have passed their expiry time.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	pool *pgxpool.Pool
}

// NewCleanupWorker creates a CleanupWorker backed by the given connection pool.
func NewCleanupWorker(pool *pgxpool.Pool) *CleanupWorker {
	return &CleanupWorker{pool: pool}
}

// Work marks all expired pending OAuth flows as "expired".
func (w *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupArgs]) error {
	tag, err := w.pool.Exec(ctx, `
		UPDATE oauth_pending_flows
		SET status = 'expired'
		WHERE status = 'pending' AND expires_at < NOW()
	`)
	if err != nil {
		return fmt.Errorf("cleanup expired oauth flows: %w", err)
	}

	if tag.RowsAffected() > 0 {
		// Log would go here in production; for now, the job completes silently.
		_ = tag.RowsAffected()
	}

	return nil
}
