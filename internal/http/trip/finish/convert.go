package finish

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oapi-codegen/nullable"

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

func toResponse(t model.Trip) api.Trip {
	return api.Trip{
		Id:             t.ID,
		UserId:         t.UserID,
		DriverId:       t.DriverID,
		StartPoint:     api.Coordinates{Latitude: t.Start.Latitude, Longitude: t.Start.Longitude},
		EndPoint:       api.Coordinates{Latitude: t.End.Latitude, Longitude: t.End.Longitude},
		Price:          t.Price,
		Status:         api.TripStatus(t.Status),
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     nullableTime(t.FinishedAt),
		LastPositionAt: nullableTime(t.LastPositionAt),
	}
}

func nullableTime(t *time.Time) nullable.Nullable[time.Time] {
	if t == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(t.UTC())
}
