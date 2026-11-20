package addposition_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
	"github.com/betharch/TripGo/internal/usecase/trip/addposition"
)

var (
	tripID     = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")
	now        = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	recordedAt = time.Date(2026, 11, 20, 11, 59, 55, 123456789, time.UTC)
)

type deps struct {
	repo     *MocktripRepository
	tx       *MocktxManager
	requests *MockpositionRequests
	metrics  *MockpositionMetrics
	uc       *addposition.Usecase
}

func newDeps(t *testing.T) deps {
	ctrl := gomock.NewController(t)
	d := deps{
		repo:     NewMocktripRepository(ctrl),
		tx:       NewMocktxManager(ctrl),
		requests: NewMockpositionRequests(ctrl),
		metrics:  NewMockpositionMetrics(ctrl),
	}
	d.uc = addposition.New(d.repo, d.tx, d.requests, d.metrics, func() time.Time { return now })
	return d
}

type txKey struct{}

func (d deps) expectTx(commitErr error) {
	d.tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			if err := fn(context.WithValue(ctx, txKey{}, true)); err != nil {
				return err
			}
			return commitErr
		},
	)
}

func inTx() gomock.Matcher {
	return gomock.Cond(func(ctx context.Context) bool { return ctx.Value(txKey{}) != nil })
}

func input() model.NewPosition {
	return model.NewPosition{Point: model.Point{Latitude: 59.9350, Longitude: 30.3350}, RecordedAt: recordedAt}
}

func TestAddPositionToActiveTrip(t *testing.T) {
	d := newDeps(t)
	d.expectTx(nil)
	want := model.Position{
		TripID:     tripID,
		Point:      model.Point{Latitude: 59.9350, Longitude: 30.3350},
		RecordedAt: recordedAt.Truncate(time.Microsecond),
	}
	gomock.InOrder(
		d.repo.EXPECT().GetForUpdate(inTx(), tripID).Return(model.Trip{ID: tripID, Status: trip.StatusActive}, nil),
		d.repo.EXPECT().SavePosition(inTx(), want).Return(int64(42), nil),
		d.repo.EXPECT().AdvanceLastPosition(inTx(), tripID, want.RecordedAt).Return(nil),
		d.requests.EXPECT().Cancel(tripID),
		d.metrics.EXPECT().PositionSaved(gomock.Not(inTx())),
	)

	got, err := d.uc.Add(context.Background(), tripID, input())

	require.NoError(t, err)
	want.ID = 42
	assert.Equal(t, want, got)
}

func TestAddInvalidPositionSkipsStorage(t *testing.T) {
	d := newDeps(t)
	in := input()
	in.RecordedAt = now.Add(2 * time.Minute)

	_, err := d.uc.Add(context.Background(), tripID, in)

	var verr *trip.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "recorded_at", verr.Field)
}

func TestAddPositionToMissingTrip(t *testing.T) {
	d := newDeps(t)
	d.expectTx(nil)
	d.repo.EXPECT().GetForUpdate(inTx(), tripID).
		Return(model.Trip{}, fmt.Errorf("trip %s: %w", tripID, trip.ErrTripNotFound))

	_, err := d.uc.Add(context.Background(), tripID, input())

	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestAddPositionToCompletedTrip(t *testing.T) {
	d := newDeps(t)
	d.expectTx(nil)
	d.repo.EXPECT().GetForUpdate(inTx(), tripID).
		Return(model.Trip{ID: tripID, Status: trip.StatusCompleted}, nil)

	_, err := d.uc.Add(context.Background(), tripID, input())

	require.ErrorIs(t, err, trip.ErrTripCompleted)
}

func TestAddPositionStorageFailure(t *testing.T) {
	d := newDeps(t)
	d.expectTx(nil)
	d.repo.EXPECT().GetForUpdate(inTx(), tripID).Return(model.Trip{ID: tripID, Status: trip.StatusActive}, nil)
	d.repo.EXPECT().SavePosition(inTx(), gomock.Any()).Return(int64(42), nil)
	storageErr := errors.New("advance last position: connection reset")
	d.repo.EXPECT().AdvanceLastPosition(inTx(), tripID, gomock.Any()).Return(storageErr)

	got, err := d.uc.Add(context.Background(), tripID, input())

	require.ErrorIs(t, err, storageErr)
	assert.Equal(t, model.Position{}, got)
}

func TestAddPositionCommitFailureKeepsRequestAndCounter(t *testing.T) {
	d := newDeps(t)
	commitErr := errors.New("commit: connection reset")
	d.expectTx(commitErr)
	d.repo.EXPECT().GetForUpdate(inTx(), tripID).Return(model.Trip{ID: tripID, Status: trip.StatusActive}, nil)
	d.repo.EXPECT().SavePosition(inTx(), gomock.Any()).Return(int64(42), nil)
	d.repo.EXPECT().AdvanceLastPosition(inTx(), tripID, gomock.Any()).Return(nil)

	got, err := d.uc.Add(context.Background(), tripID, input())

	require.ErrorIs(t, err, commitErr)
	assert.Equal(t, model.Position{}, got)
}
