package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oapi-codegen/nullable"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/trip"
)

type Handler struct {
	trips       *trip.Service
	ping        func(ctx context.Context) error
	pingTimeout time.Duration
	log         *slog.Logger
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(trips *trip.Service, ping func(ctx context.Context) error, pingTimeout time.Duration, log *slog.Logger) *Handler {
	return &Handler{trips: trips, ping: ping, pingTimeout: pingTimeout, log: log}
}

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	in, err := decodeTripData(w, r)
	if err != nil {
		var badRequest *validationError
		if errors.As(err, &badRequest) {
			writeProblem(w, r, problemInvalidRequest, badRequest.msg)
			return
		}
		h.writeError(w, r, err)
		return
	}

	created, err := h.trips.Create(r.Context(), in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/trips/"+created.ID.String())
	writeJSON(w, http.StatusCreated, toAPITrip(created))
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	if !canonicalTripID(w, r) {
		return
	}
	t, err := h.trips.Get(r.Context(), tripID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPITrip(t))
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	if !canonicalTripID(w, r) {
		return
	}
	t, err := h.trips.Finish(r.Context(), tripID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPITrip(t))
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.pingTimeout)
	defer cancel()
	if err := h.ping(ctx); err != nil {
		h.log.WarnContext(r.Context(), "readiness check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func canonicalTripID(w http.ResponseWriter, r *http.Request) bool {
	if isCanonicalUUID(chi.URLParam(r, "tripId")) {
		return true
	}
	writeProblem(w, r, problemInvalidRequest, "Parameter tripId must be a UUID")
	return false
}

func toAPITrip(t trip.Trip) api.Trip {
	finishedAt := nullable.NewNullNullable[time.Time]()
	if t.FinishedAt != nil {
		finishedAt = nullable.NewNullableWithValue(t.FinishedAt.UTC())
	}
	return api.Trip{
		Id:             t.ID,
		UserId:         t.UserID,
		DriverId:       t.DriverID,
		StartPoint:     api.Coordinates{Latitude: t.Start.Latitude, Longitude: t.Start.Longitude},
		EndPoint:       api.Coordinates{Latitude: t.End.Latitude, Longitude: t.End.Longitude},
		Price:          t.Price,
		Status:         api.TripStatus(t.Status),
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     finishedAt,
		LastPositionAt: nullable.NewNullNullable[time.Time](),
	}
}
