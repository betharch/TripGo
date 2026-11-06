package finish_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
	"github.com/betharch/TripGo/internal/usecase/trip/finish"
)

var (
	id        = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")
	startedAt = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	now       = time.Date(2026, 11, 6, 12, 25, 0, 987654321, time.UTC)
)

type deps struct {
	repo    *MocktripRepository
	tx      *MocktxManager
	metrics *MocktripMetrics
	uc      *finish.Usecase
}

func newDeps(t *testing.T) deps {
	ctrl := gomock.NewController(t)
	d := deps{
		repo:    NewMocktripRepository(ctrl),
		tx:      NewMocktxManager(ctrl),
		metrics: NewMocktripMetrics(ctrl),
	}
	d.uc = finish.New(d.repo, d.tx, d.metrics, slog.New(slog.DiscardHandler), func() time.Time { return now })
	d.tx.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	return d
}

func activeTrip() model.Trip {
	return model.Trip{ID: id, Status: trip.StatusActive, StartedAt: startedAt}
}

func TestFinishActiveTrip(t *testing.T) {
	d := newDeps(t)
	d.repo.EXPECT().GetForUpdate(gomock.Any(), id).Return(activeTrip(), nil)
	var completed model.Trip
	d.repo.EXPECT().Complete(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tr model.Trip) error { completed = tr; return nil })
	var change model.StatusChange
	d.repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, c model.StatusChange) error { change = c; return nil })
	d.metrics.EXPECT().TripCompleted(gomock.Any())

	got, err := d.uc.Finish(context.Background(), id)

	require.NoError(t, err)
	finishedAt := now.Truncate(time.Microsecond)
	assert.Equal(t, trip.StatusCompleted, got.Status)
	require.NotNil(t, got.FinishedAt)
	assert.Equal(t, finishedAt, *got.FinishedAt)
	assert.Equal(t, got, completed)
	active := trip.StatusActive
	assert.Equal(t, model.StatusChange{
		TripID:    id,
		From:      &active,
		To:        trip.StatusCompleted,
		Reason:    "finished by driver",
		ChangedAt: finishedAt,
	}, change)
}

func TestFinishCompletedTrip(t *testing.T) {
	d := newDeps(t)
	done := startedAt.Add(10 * time.Minute)
	d.repo.EXPECT().GetForUpdate(gomock.Any(), id).
		Return(model.Trip{ID: id, Status: trip.StatusCompleted, StartedAt: startedAt, FinishedAt: &done}, nil)

	_, err := d.uc.Finish(context.Background(), id)

	require.ErrorIs(t, err, trip.ErrTripCompleted)
}

func TestFinishMissingTrip(t *testing.T) {
	d := newDeps(t)
	d.repo.EXPECT().GetForUpdate(gomock.Any(), id).
		Return(model.Trip{}, fmt.Errorf("trip %s: %w", id, trip.ErrTripNotFound))

	_, err := d.uc.Finish(context.Background(), id)

	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestFinishStorageFailure(t *testing.T) {
	d := newDeps(t)
	d.repo.EXPECT().GetForUpdate(gomock.Any(), id).Return(activeTrip(), nil)
	storageErr := errors.New("complete trip: connection reset")
	d.repo.EXPECT().Complete(gomock.Any(), gomock.Any()).Return(storageErr)

	_, err := d.uc.Finish(context.Background(), id)

	require.ErrorIs(t, err, storageErr)
}
