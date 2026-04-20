package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

type Handler struct {
	db    *pgxpool.Pool
	river *river.Client[pgx.Tx]
}

func Register(r chi.Router, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) {
	h := &Handler{db: db, river: riverClient}

	r.Get("/health", h.Health)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
