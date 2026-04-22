// Package handler provides HTTP request handlers.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	db    *pgxpool.Pool
	river *river.Client[pgx.Tx]
}

// Register mounts all HTTP routes on the given router.
func Register(r chi.Router, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) {
	h := &Handler{db: db, river: riverClient}

	r.Get("/health", h.Health)
}

// Health returns the service health status.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
