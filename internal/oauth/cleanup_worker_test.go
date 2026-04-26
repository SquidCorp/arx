package oauth

import (
	"testing"

	"github.com/riverqueue/river"
)

func TestCleanupArgs_Kind(t *testing.T) {
	args := CleanupArgs{}
	if args.Kind() != "oauth_flow_cleanup" {
		t.Errorf("Kind() = %q, want %q", args.Kind(), "oauth_flow_cleanup")
	}
}

func TestCleanupArgs_InsertOpts(t *testing.T) {
	args := CleanupArgs{}
	opts := args.InsertOpts()
	if opts.Queue != "default" {
		t.Errorf("Queue = %q, want %q", opts.Queue, "default")
	}
}

// Verify CleanupWorker satisfies the river.Worker interface at compile time.
var _ river.Worker[CleanupArgs] = (*CleanupWorker)(nil)
