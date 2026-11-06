package trip_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
)

func validInput() model.NewTrip {
	return model.NewTrip{
		UserID:   uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID: uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		Start:    model.Point{Latitude: 59.9398, Longitude: 30.3146},
		End:      model.Point{Latitude: 59.9290, Longitude: 30.3626},
		Price:    1450,
	}
}

func TestNew(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	id := uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")

	tests := []struct {
		name      string
		change    func(p *model.NewTrip)
		wantField string
	}{
		{name: "valid", change: func(*model.NewTrip) {}},
		{name: "boundaries are valid", change: func(p *model.NewTrip) {
			p.Start = model.Point{Latitude: -90, Longitude: -180}
			p.End = model.Point{Latitude: 90, Longitude: 180}
			p.Price = 0
		}},
		{name: "nil user", change: func(p *model.NewTrip) { p.UserID = uuid.Nil }, wantField: "user_id"},
		{name: "nil driver", change: func(p *model.NewTrip) { p.DriverID = uuid.Nil }, wantField: "driver_id"},
		{name: "start latitude below range", change: func(p *model.NewTrip) { p.Start.Latitude = -90.1 }, wantField: "start_point.latitude"},
		{name: "start longitude above range", change: func(p *model.NewTrip) { p.Start.Longitude = 180.1 }, wantField: "start_point.longitude"},
		{name: "end latitude above range", change: func(p *model.NewTrip) { p.End.Latitude = 90.1 }, wantField: "end_point.latitude"},
		{name: "end longitude below range", change: func(p *model.NewTrip) { p.End.Longitude = -180.1 }, wantField: "end_point.longitude"},
		{name: "negative price", change: func(p *model.NewTrip) { p.Price = -1 }, wantField: "price"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validInput()
			tt.change(&p)

			got, err := trip.New(id, p, now)

			if tt.wantField != "" {
				var verr *trip.ValidationError
				require.ErrorAs(t, err, &verr)
				assert.Equal(t, tt.wantField, verr.Field)
				assert.Equal(t, trip.Trip{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, trip.Trip{
				ID:        id,
				UserID:    p.UserID,
				DriverID:  p.DriverID,
				Start:     p.Start,
				End:       p.End,
				Price:     p.Price,
				Status:    trip.StatusActive,
				StartedAt: now,
			}, got)
		})
	}
}

func TestFinish(t *testing.T) {
	startedAt := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(25 * time.Minute)

	t.Run("active trip becomes completed", func(t *testing.T) {
		tr := trip.Trip{Status: trip.StatusActive, StartedAt: startedAt}

		err := tr.Finish(finishedAt)

		require.NoError(t, err)
		assert.Equal(t, trip.StatusCompleted, tr.Status)
		require.NotNil(t, tr.FinishedAt)
		assert.Equal(t, finishedAt, *tr.FinishedAt)
	})

	t.Run("completed trip stays unchanged", func(t *testing.T) {
		earlier := startedAt.Add(10 * time.Minute)
		tr := trip.Trip{Status: trip.StatusCompleted, StartedAt: startedAt, FinishedAt: &earlier}

		err := tr.Finish(finishedAt)

		require.ErrorIs(t, err, trip.ErrTripCompleted)
		assert.Equal(t, trip.StatusCompleted, tr.Status)
		assert.Equal(t, earlier, *tr.FinishedAt)
	})
}
