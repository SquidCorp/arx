package worker

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
)

func TestNewConfig(t *testing.T) {
	cfg, workers := NewConfig(nil)

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if workers == nil {
		t.Fatal("expected non-nil workers")
	}

	q, ok := cfg.Queues[river.QueueDefault]
	if !ok {
		t.Fatal("expected default queue to be configured")
	}
	if q.MaxWorkers != 10 {
		t.Errorf("expected 10 max workers, got %d", q.MaxWorkers)
	}
}

func TestStartNilPool(t *testing.T) {
	_, err := Start(context.Background(), nil)
	if err == nil {
		t.Error("expected error when starting with nil pool")
	}
}
