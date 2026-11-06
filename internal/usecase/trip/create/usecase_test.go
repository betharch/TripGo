package create_test

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
	"github.com/betharch/TripGo/internal/usecase/trip/create"
)

var now = time.Date(2026, 11, 6, 12, 30, 15, 123456789, time.UTC)

type deps struct {
	repo    *MocktripRepository
	tx      *MocktxManager
	metrics *MocktripMetrics
	uc      *create.Usecase
}

func newDeps(t *testing.T) deps {
	ctrl := gomock.NewController(t)
	d := deps{
		repo:    NewMocktripRepository(ctrl),
		tx:      NewMocktxManager(ctrl),
		metrics: NewMocktripMetrics(ctrl),
	}
	d.uc = create.New(d.repo, d.tx, d.metrics, slog.New(slog.DiscardHandler), func() time.Time { return now })
	return d
}

func (d deps) runInTx() {
	d.tx.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
}

func params() model.NewTrip {
	return model.NewTrip{
		UserID:   uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID: uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		Start:    model.Point{Latitude: 59.9398, Longitude: 30.3146},
		End:      model.Point{Latitude: 59.9290, Longitude: 30.3626},
		Price:    1450,
	}
}

func TestCreateSuccess(t *testing.T) {
	d := newDeps(t)
	d.runInTx()
	var saved model.Trip
	d.repo.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tr model.Trip) error { saved = tr; return nil })
	var change model.StatusChange
	d.repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, c model.StatusChange) error { change = c; return nil })
	d.metrics.EXPECT().TripCreated(gomock.Any())

	got, err := d.uc.Create(context.Background(), params())

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, got.ID)
	assert.Equal(t, params().UserID, got.UserID)
	assert.Equal(t, params().DriverID, got.DriverID)
	assert.Equal(t, int64(1450), got.Price)
	assert.Equal(t, trip.StatusActive, got.Status)
	assert.Equal(t, now.Truncate(time.Microsecond), got.StartedAt)
	assert.Nil(t, got.FinishedAt)
	assert.Equal(t, got, saved)
	assert.Equal(t, model.StatusChange{
		TripID:    got.ID,
		To:        trip.StatusActive,
		Reason:    "trip created",
		ChangedAt: got.StartedAt,
	}, change)
}

func TestCreateDriverBusy(t *testing.T) {
	d := newDeps(t)
	d.runInTx()
	d.repo.EXPECT().Save(gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("%w: %w", trip.ErrDriverBusy, errors.New("duplicate key")))

	_, err := d.uc.Create(context.Background(), params())

	require.ErrorIs(t, err, trip.ErrDriverBusy)
}

func TestCreateInvalidParamsDoesNotTouchStorage(t *testing.T) {
	d := newDeps(t)
	p := params()
	p.Price = -1

	_, err := d.uc.Create(context.Background(), p)

	var verr *trip.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "price", verr.Field)
}

func TestCreateHistoryFailure(t *testing.T) {
	d := newDeps(t)
	d.runInTx()
	d.repo.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)
	storageErr := errors.New("insert status change: connection reset")
	d.repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).Return(storageErr)

	_, err := d.uc.Create(context.Background(), params())

	require.ErrorIs(t, err, storageErr)
}
