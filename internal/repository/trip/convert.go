package trip

import (
	"github.com/jackc/pgx/v5"

	"github.com/betharch/TripGo/internal/model"
)

var tripColumns = []string{
	"id", "user_id", "driver_id",
	"start_latitude", "start_longitude", "end_latitude", "end_longitude",
	"price", "status", "started_at", "finished_at",
}

func scanTrip(row pgx.Row) (model.Trip, error) {
	var (
		t      model.Trip
		status string
	)
	err := row.Scan(
		&t.ID, &t.UserID, &t.DriverID,
		&t.Start.Latitude, &t.Start.Longitude, &t.End.Latitude, &t.End.Longitude,
		&t.Price, &status, &t.StartedAt, &t.FinishedAt,
	)
	if err != nil {
		return model.Trip{}, err
	}
	t.Status = model.TripStatus(status)
	return t, nil
}
