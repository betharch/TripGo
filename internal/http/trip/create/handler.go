package create

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/betharch/TripGo/internal/http/domainerror"
)

type Handler struct {
	uc  usecase
	log *slog.Logger
}

func New(uc usecase, log *slog.Logger) *Handler {
	return &Handler{uc: uc, log: log}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	in, err := decode(w, r)
	if err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	created, err := h.uc.Create(r.Context(), in)
	if err != nil {
		domainerror.Respond(w, r, h.log, err)
		return
	}
	w.Header().Set("Location", "/api/v1/trips/"+created.ID.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toResponse(created))
}
