package addposition

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

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) {
	if err := checkTripID(r); err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	in, err := decode(w, r)
	if err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	saved, err := h.uc.Add(r.Context(), tripID, in)
	if err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toResponse(saved))
}
