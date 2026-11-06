package finish

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/http/domainerror"
)

type Handler struct {
	uc  usecase
	log *slog.Logger
}

func New(uc usecase, log *slog.Logger) *Handler {
	return &Handler{uc: uc, log: log}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := checkTripID(r); err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	t, err := h.uc.Finish(r.Context(), id)
	if err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toResponse(t))
}
