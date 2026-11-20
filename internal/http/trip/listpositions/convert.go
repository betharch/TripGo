package listpositions

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/model"
)

func checkTripID(r *http.Request) error {
	if len(chi.URLParam(r, "tripId")) != 36 {
		return &trip.ValidationError{Field: "tripId", Reason: "must be a UUID"}
	}
	return nil
}

func toResponse(positions []model.Position) api.TripPositionsResponse {
	items := make([]api.TripPosition, 0, len(positions))
	for _, p := range positions {
		items = append(items, api.TripPosition{
			Id:          p.ID,
			TripId:      p.TripID,
			Coordinates: api.Coordinates{Latitude: p.Point.Latitude, Longitude: p.Point.Longitude},
			RecordedAt:  p.RecordedAt.UTC(),
		})
	}
	return api.TripPositionsResponse{Positions: items}
}
