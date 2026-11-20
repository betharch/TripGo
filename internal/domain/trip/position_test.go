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

func TestNewPosition(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	tripID := uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")
	valid := model.NewPosition{
		Point:      model.Point{Latitude: 59.9350, Longitude: 30.3350},
		RecordedAt: now.Add(-5 * time.Second),
	}

	tests := []struct {
		name      string
		change    func(p *model.NewPosition)
		wantField string
	}{
		{name: "valid", change: func(*model.NewPosition) {}},
		{name: "boundaries are valid", change: func(p *model.NewPosition) {
			p.Point = model.Point{Latitude: -90, Longitude: 180}
		}},
		{name: "recorded long ago", change: func(p *model.NewPosition) { p.RecordedAt = now.Add(-48 * time.Hour) }},
		{name: "exactly a minute ahead", change: func(p *model.NewPosition) { p.RecordedAt = now.Add(time.Minute) }},
		{name: "start of year 0000 in UTC", change: func(p *model.NewPosition) {
			p.RecordedAt = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
		}},
		{name: "latitude above range", change: func(p *model.NewPosition) { p.Point.Latitude = 90.1 }, wantField: "coordinates.latitude"},
		{name: "longitude below range", change: func(p *model.NewPosition) { p.Point.Longitude = -180.1 }, wantField: "coordinates.longitude"},
		{name: "more than a minute ahead", change: func(p *model.NewPosition) {
			p.RecordedAt = now.Add(time.Minute + time.Microsecond)
		}, wantField: "recorded_at"},
		{name: "before year 0000 once shifted to UTC", change: func(p *model.NewPosition) {
			p.RecordedAt = time.Date(0, 1, 1, 0, 0, 0, 0, time.FixedZone("", 60*60))
		}, wantField: "recorded_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.change(&in)

			got, err := trip.NewPosition(tripID, in, now)

			if tt.wantField != "" {
				var verr *trip.ValidationError
				require.ErrorAs(t, err, &verr)
				assert.Equal(t, tt.wantField, verr.Field)
				assert.Equal(t, model.Position{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, model.Position{TripID: tripID, Point: in.Point, RecordedAt: in.RecordedAt}, got)
		})
	}
}

func TestNewPositionNormalizesRecordedAt(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	moscow := time.FixedZone("MSK", 3*60*60)
	recordedAt := time.Date(2026, 11, 20, 14, 59, 30, 123456789, moscow)

	got, err := trip.NewPosition(uuid.New(), model.NewPosition{RecordedAt: recordedAt}, now)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 11, 20, 11, 59, 30, 123456000, time.UTC), got.RecordedAt)
	assert.Equal(t, time.UTC, got.RecordedAt.Location())
}

func TestAcceptPosition(t *testing.T) {
	t.Run("active trip accepts positions", func(t *testing.T) {
		tr := trip.Trip{Status: trip.StatusActive}

		require.NoError(t, tr.AcceptPosition())
	})

	t.Run("completed trip rejects positions", func(t *testing.T) {
		tr := trip.Trip{Status: trip.StatusCompleted}

		require.ErrorIs(t, tr.AcceptPosition(), trip.ErrTripCompleted)
	})
}

func TestAcceptedPositionCanBeEncodedAsJSON(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	earliest := time.Date(0, 1, 1, 0, 59, 59, 0, time.FixedZone("", 59*60+59))

	got, err := trip.NewPosition(uuid.New(), model.NewPosition{RecordedAt: earliest}, now)

	require.NoError(t, err)
	_, err = got.RecordedAt.MarshalJSON()
	require.NoError(t, err)
}
