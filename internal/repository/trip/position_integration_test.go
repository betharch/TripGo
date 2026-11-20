//go:build integration

package trip_test

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/infra/postgres/transaction"
	"github.com/betharch/TripGo/internal/model"
	triprepo "github.com/betharch/TripGo/internal/repository/trip"
)

const staleAfter = time.Minute

func compareIDs(a, b uuid.UUID) int {
	return bytes.Compare(a[:], b[:])
}

func staleBatches(t *testing.T, repo *triprepo.Repo, limit int) [][]model.StaleTrip {
	t.Helper()
	var (
		batches [][]model.StaleTrip
		last    *model.StaleTrip
	)
	for {
		batch, err := repo.ListStale(t.Context(), staleAfter, last, limit)
		require.NoError(t, err)
		require.LessOrEqual(t, len(batch), limit)
		batches = append(batches, batch)
		if len(batch) < limit {
			return batches
		}
		next := &batch[len(batch)-1]
		if last != nil {
			advanced := next.LastSeenAt.After(last.LastSeenAt) ||
				next.LastSeenAt.Equal(last.LastSeenAt) && compareIDs(next.ID, last.ID) > 0
			require.True(t, advanced, "batch after %s does not move forward", last.ID)
		}
		last = next
	}
}

func readOrder(batches [][]model.StaleTrip, ids ...uuid.UUID) []uuid.UUID {
	var read []uuid.UUID
	for _, s := range slices.Concat(batches...) {
		if slices.Contains(ids, s.ID) {
			read = append(read, s.ID)
		}
	}
	return read
}

func poolWith(t *testing.T, configure func(*pgx.ConnConfig)) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	require.NoError(t, err)
	configure(cfg.ConnConfig)
	configured, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(configured.Close)
	return configured
}

type queryRecorder struct {
	queries []pgx.TraceQueryStartData
}

func (r *queryRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.queries = append(r.queries, data)
	return ctx
}

func (*queryRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func planWithoutSort(t *testing.T, query pgx.TraceQueryStartData) string {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tx.Rollback(context.Background())) })
	_, err = tx.Exec(t.Context(), "SET LOCAL enable_sort = off")
	require.NoError(t, err)
	rows, err := tx.Query(t.Context(), "EXPLAIN "+query.SQL, query.Args...)
	require.NoError(t, err)
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return strings.Join(lines, "\n")
}

func savedTrip(t *testing.T, repo *triprepo.Repo, startedAt time.Time) model.Trip {
	t.Helper()
	tr := newTrip(t, uuid.New())
	tr.StartedAt = startedAt.UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Save(t.Context(), tr))
	return tr
}

func addPosition(t *testing.T, repo *triprepo.Repo, tx *transaction.Manager, tripID uuid.UUID, recordedAt time.Time) model.Position {
	t.Helper()
	p := model.Position{
		TripID:     tripID,
		Point:      model.Point{Latitude: 59.9350, Longitude: 30.3350},
		RecordedAt: recordedAt.UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, tx.Do(t.Context(), func(ctx context.Context) error {
		id, err := repo.SavePosition(ctx, p)
		if err != nil {
			return err
		}
		p.ID = id
		return repo.AdvanceLastPosition(ctx, tripID, p.RecordedAt)
	}))
	return p
}

func lastPositionAt(t *testing.T, repo *triprepo.Repo, tripID uuid.UUID) *time.Time {
	t.Helper()
	got, err := repo.Get(t.Context(), tripID)
	require.NoError(t, err)
	return got.LastPositionAt
}

func TestSavePositionSetsLastPosition(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := savedTrip(t, repo, time.Now())
	require.Nil(t, lastPositionAt(t, repo, tr.ID))
	recordedAt := time.Now().Add(-10 * time.Second)

	saved := addPosition(t, repo, tx, tr.ID, recordedAt)

	assert.Positive(t, saved.ID)
	got := lastPositionAt(t, repo, tr.ID)
	require.NotNil(t, got)
	assert.True(t, saved.RecordedAt.Equal(*got))
}

func TestLatePositionDoesNotMoveLastPositionBack(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := savedTrip(t, repo, time.Now().Add(-time.Hour))
	fresh := addPosition(t, repo, tx, tr.ID, time.Now().Add(-5*time.Second))

	late := addPosition(t, repo, tx, tr.ID, time.Now().Add(-30*time.Minute))

	got := lastPositionAt(t, repo, tr.ID)
	require.NotNil(t, got)
	assert.True(t, fresh.RecordedAt.Equal(*got))
	positions, err := repo.ListPositions(t.Context(), tr.ID)
	require.NoError(t, err)
	require.Len(t, positions, 2)
	assert.Equal(t, late.ID, positions[0].ID)
	assert.Equal(t, fresh.ID, positions[1].ID)
}

func TestFailedAdvanceRollsBackPosition(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := savedTrip(t, repo, time.Now())
	p := model.Position{TripID: tr.ID, Point: model.Point{Latitude: 59.9350, Longitude: 30.3350}, RecordedAt: time.Now().UTC()}

	err := tx.Do(t.Context(), func(ctx context.Context) error {
		if _, err := repo.SavePosition(ctx, p); err != nil {
			return err
		}
		return repo.AdvanceLastPosition(ctx, uuid.New(), p.RecordedAt)
	})

	require.ErrorIs(t, err, trip.ErrTripNotFound)
	positions, err := repo.ListPositions(t.Context(), tr.ID)
	require.NoError(t, err)
	assert.Empty(t, positions)
	assert.Nil(t, lastPositionAt(t, repo, tr.ID))
}

func TestListPositionsOrdersByRecordedAtThenID(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := savedTrip(t, repo, time.Now().Add(-time.Hour))
	base := time.Now().Add(-10 * time.Minute)
	third := addPosition(t, repo, tx, tr.ID, base.Add(2*time.Minute))
	firstOfTwins := addPosition(t, repo, tx, tr.ID, base)
	secondOfTwins := addPosition(t, repo, tx, tr.ID, base)

	positions, err := repo.ListPositions(t.Context(), tr.ID)

	require.NoError(t, err)
	require.Len(t, positions, 3)
	assert.Equal(t, []int64{firstOfTwins.ID, secondOfTwins.ID, third.ID},
		[]int64{positions[0].ID, positions[1].ID, positions[2].ID})
	assert.Equal(t, tr.ID, positions[0].TripID)
	assert.InDelta(t, 59.9350, positions[0].Point.Latitude, 0)
	assert.InDelta(t, 30.3350, positions[0].Point.Longitude, 0)
	assert.True(t, firstOfTwins.RecordedAt.Equal(positions[0].RecordedAt))
}

func TestListPositionsOfTripWithoutPositionsIsEmpty(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tr := savedTrip(t, repo, time.Now())

	positions, err := repo.ListPositions(t.Context(), tr.ID)

	require.NoError(t, err)
	assert.NotNil(t, positions)
	assert.Empty(t, positions)
}

func TestListStale(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	now := time.Now()

	silentLongest := savedTrip(t, repo, now.Add(-3*time.Hour))
	silentAfterPosition := savedTrip(t, repo, now.Add(-4*time.Hour))
	lastPosition := addPosition(t, repo, tx, silentAfterPosition.ID, now.Add(-2*time.Hour))
	silentTwoMinutes := savedTrip(t, repo, now.Add(-2*time.Minute))
	positionOlderThanStart := savedTrip(t, repo, now.Add(-30*time.Second))
	earlyPosition := addPosition(t, repo, tx, positionOlderThanStart.ID, now.Add(-5*time.Minute))
	justStarted := savedTrip(t, repo, now)
	freshPosition := savedTrip(t, repo, now.Add(-3*time.Hour))
	addPosition(t, repo, tx, freshPosition.ID, now.Add(-5*time.Second))
	completed := savedTrip(t, repo, now.Add(-3*time.Hour))
	finished := trip.Trip(completed)
	require.NoError(t, finished.Finish(now.UTC().Truncate(time.Microsecond)))
	require.NoError(t, repo.Complete(t.Context(), model.Trip(finished)))

	stale := slices.Concat(staleBatches(t, repo, 100)...)

	position := func(id uuid.UUID) int {
		return slices.IndexFunc(stale, func(s model.StaleTrip) bool { return s.ID == id })
	}
	longest, afterPosition := position(silentLongest.ID), position(silentAfterPosition.ID)
	require.NotEqual(t, -1, longest)
	require.NotEqual(t, -1, afterPosition)
	assert.Less(t, longest, afterPosition)
	assert.Equal(t, silentLongest.DriverID, stale[longest].DriverID)
	assert.True(t, silentLongest.StartedAt.Equal(stale[longest].LastSeenAt))
	assert.True(t, lastPosition.RecordedAt.Equal(stale[afterPosition].LastSeenAt))
	assert.NotEqual(t, -1, position(silentTwoMinutes.ID))
	early := position(positionOlderThanStart.ID)
	require.NotEqual(t, -1, early)
	assert.True(t, earlyPosition.RecordedAt.Equal(stale[early].LastSeenAt))
	assert.Equal(t, -1, position(justStarted.ID))
	assert.Equal(t, -1, position(freshPosition.ID))
	assert.Equal(t, -1, position(completed.ID))
}

func TestListStaleReadsTripsWithEqualTimeOnceAcrossBatches(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	startedAt := time.Now().Add(-2 * time.Hour)
	twins := make([]uuid.UUID, 0, 5)
	for range cap(twins) {
		twins = append(twins, savedTrip(t, repo, startedAt).ID)
	}
	slices.SortFunc(twins, compareIDs)

	batches := staleBatches(t, repo, 2)

	assert.Equal(t, twins, readOrder(batches, twins...))
}

func TestListStaleFollowsLastSeenOrderAcrossBatches(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	now := time.Now()
	byID := make([]uuid.UUID, 0, 3)
	for range cap(byID) {
		byID = append(byID, savedTrip(t, repo, now.Add(-9*time.Hour)).ID)
	}
	slices.SortFunc(byID, compareIDs)
	addPosition(t, repo, tx, byID[0], now.Add(-1*time.Hour))
	addPosition(t, repo, tx, byID[1], now.Add(-2*time.Hour))
	addPosition(t, repo, tx, byID[2], now.Add(-3*time.Hour))

	batches := staleBatches(t, repo, 1)

	assert.Equal(t, []uuid.UUID{byID[2], byID[1], byID[0]}, readOrder(batches, byID...))
}

func TestListStaleReadsTripWithLastPositionBeforeYearOne(t *testing.T) {
	repo := triprepo.New(pool, timeout)
	tx := transaction.NewManager(pool, timeout)
	tr := savedTrip(t, repo, time.Now().Add(-time.Hour))
	addPosition(t, repo, tx, tr.ID, time.Date(0, 6, 1, 0, 0, 0, 0, time.UTC))

	batches := staleBatches(t, repo, 100)

	assert.Equal(t, []uuid.UUID{tr.ID}, readOrder(batches, tr.ID))
}

func TestListStaleQueriesUseStaleIndex(t *testing.T) {
	recorder := &queryRecorder{}
	repo := triprepo.New(poolWith(t, func(cfg *pgx.ConnConfig) { cfg.Tracer = recorder }), timeout)

	_, err := repo.ListStale(t.Context(), staleAfter, nil, 100)
	require.NoError(t, err)
	_, err = repo.ListStale(t.Context(), staleAfter, &model.StaleTrip{ID: uuid.New(), LastSeenAt: time.Now()}, 100)
	require.NoError(t, err)

	require.Len(t, recorder.queries, 2)
	for _, query := range recorder.queries {
		plan := planWithoutSort(t, query)
		assert.Contains(t, plan, "Index Scan using trips_stale_idx", query.SQL)
		assert.NotContains(t, plan, "Sort", query.SQL)
		assert.NotContains(t, plan, "Filter", query.SQL)
	}
}
