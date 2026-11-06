package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	api "github.com/betharch/TripGo/internal/generated"
)

type Handler struct {
	db      pinger
	timeout time.Duration
	log     *slog.Logger
}

func New(db pinger, timeout time.Duration, log *slog.Logger) *Handler {
	return &Handler{db: db, timeout: timeout, log: log}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, api.Ok)
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		h.log.WarnContext(r.Context(), "readiness check failed", "error", err)
		write(w, http.StatusServiceUnavailable, api.Unavailable)
		return
	}
	write(w, http.StatusOK, api.Ok)
}

func write(w http.ResponseWriter, code int, status api.HealthResponseStatus) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: status})
}
