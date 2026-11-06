//go:build integration

package trip_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/infra/postgres/transaction"
	"github.com/betharch/TripGo/internal/model"
	triprepo "github.com/betharch/TripGo/internal/repository/trip"
)

const timeout = 3 * time.Second

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is not set: run tripgoctl environment start and make migrate")
		os.Exit(1)
	}
	var err error
	pool, err = pgxpool.New(context.Background(), url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect to database:", err)
		os.Exit(1)
	}
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func newTrip(t *testing.T, driverID uuid.UUID) model.Trip {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	tr, err := trip.New(id, model.NewTrip{
		UserID:   uuid.New(),
		DriverID: driverID,
		Start:    model.Point{Latitude: 59.9398, Longitude: 30.3146},
		End:      model.Point{Latitude: 59.9290, Longitude: 30.3626},
		Price:    1450,
	}, time.Now().UTC().Truncate(time.Microsecond))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := pool.Exec(ctx, "DELETE FROM trip_status_history WHERE trip_id = $1", tr.ID)
		assert.NoError(t, err)
		_, err = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tr.ID)
		assert.NoError(t, err)
	})
	return model.Trip(tr)
}

func historyRows(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", id).Scan(&n))
	return n
}

func utc(tr model.Trip) model.Trip {
	tr.StartedAt = tr.StartedAt.UTC()
	if tr.FinishedAt != nil {
		finished := tr.FinishedAt.UTC()
		tr.FinishedAt = &finished
	}
	return tr
}

func TestSaveAndGet(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tr := newTrip(t, uuid.New())

	require.NoError(t, repo.Save(t.Context(), tr))
	got, err := repo.Get(t.Context(), tr.ID)

	require.NoError(t, err)
	assert.Equal(t, tr, utc(got))
}

func TestGetMissingTrip(t *testing.T) {
	_, err := triprepo.New(pool, timeout).Get(t.Context(), uuid.New())

	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestSecondActiveTripForDriverIsBusy(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	driverID := uuid.New()
	require.NoError(t, repo.Save(t.Context(), newTrip(t, driverID)))

	err := repo.Save(t.Context(), newTrip(t, driverID))

	require.ErrorIs(t, err, trip.ErrDriverBusy)
}

func TestFinishInTransaction(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := newTrip(t, uuid.New())
	require.NoError(t, repo.Save(t.Context(), tr))

	err := tx.Do(t.Context(), func(ctx context.Context) error {
		stored, err := repo.GetForUpdate(ctx, tr.ID)
		if err != nil {
			return err
		}
		locked := trip.Trip(stored)
		if err := locked.Finish(time.Now().UTC().Truncate(time.Microsecond)); err != nil {
			return err
		}
		return repo.Complete(ctx, model.Trip(locked))
	})
	require.NoError(t, err)

	got, err := repo.Get(t.Context(), tr.ID)
	require.NoError(t, err)
	assert.Equal(t, trip.StatusCompleted, got.Status)
	assert.NotNil(t, got.FinishedAt)
}

func TestCompleteMissingTrip(t *testing.T) {
	finished := trip.Trip(newTrip(t, uuid.New()))
	require.NoError(t, finished.Finish(time.Now().UTC()))

	err := triprepo.New(pool, timeout).Complete(t.Context(), model.Trip(finished))

	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestFailedSecondInsertRollsBackBoth(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := newTrip(t, uuid.New())

	err := tx.Do(t.Context(), func(ctx context.Context) error {
		if err := repo.Save(ctx, tr); err != nil {
			return err
		}
		return repo.AddStatusChange(ctx, model.StatusChange{
			TripID: tr.ID, To: model.TripStatus("bogus"), Reason: "trip created", ChangedAt: tr.StartedAt,
		})
	})

	require.Error(t, err)
	_, err = repo.Get(t.Context(), tr.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
	assert.Zero(t, historyRows(t, tr.ID))
}

func TestNestedDoJoinsOuterTransaction(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := newTrip(t, uuid.New())
	abort := errors.New("abort outer transaction")

	err := tx.Do(t.Context(), func(ctx context.Context) error {
		if err := repo.Save(ctx, tr); err != nil {
			return err
		}
		if err := tx.Do(ctx, func(ctx context.Context) error {
			_, err := repo.Get(ctx, tr.ID)
			return err
		}); err != nil {
			return err
		}
		return abort
	})

	require.ErrorIs(t, err, abort)
	_, err = repo.Get(t.Context(), tr.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestPanicRollsBack(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := newTrip(t, uuid.New())

	assert.PanicsWithValue(t, "boom", func() {
		_ = tx.Do(t.Context(), func(ctx context.Context) error {
			if err := repo.Save(ctx, tr); err != nil {
				return err
			}
			panic("boom")
		})
	})

	_, err := repo.Get(t.Context(), tr.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}
