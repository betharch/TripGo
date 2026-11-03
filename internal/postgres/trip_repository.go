package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/betharch/TripGo/internal/trip"
)

const (
	codeUniqueViolation   = "23505"
	driverActiveTripIndex = "trips_driver_active_uniq"
)

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

var tripColumns = []string{
	"id", "user_id", "driver_id",
	"start_latitude", "start_longitude", "end_latitude", "end_longitude",
	"price", "status", "started_at", "finished_at",
}

const returningTrip = "RETURNING id, user_id, driver_id, " +
	"start_latitude, start_longitude, end_latitude, end_longitude, " +
	"price, status, started_at, finished_at"

type TripRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewTripRepository(pool *pgxpool.Pool, timeout time.Duration) *TripRepository {
	return &TripRepository{pool: pool, timeout: timeout}
}

func (r *TripRepository) Create(ctx context.Context, t trip.Trip) (trip.Trip, error) {
	query, args, err := psql.Insert("trips").
		Columns("id", "user_id", "driver_id",
			"start_latitude", "start_longitude", "end_latitude", "end_longitude",
			"price", "status", "started_at").
		Values(t.ID, t.UserID, t.DriverID,
			t.Start.Latitude, t.Start.Longitude, t.End.Latitude, t.End.Longitude,
			t.Price, string(t.Status), t.StartedAt).
		Suffix(returningTrip).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build insert trip query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	created, err := scanTrip(executor(ctx, r.pool).QueryRow(ctx, query, args...))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation &&
			pgErr.ConstraintName == driverActiveTripIndex {
			return trip.Trip{}, fmt.Errorf("%w: %w", trip.ErrDriverBusy, err)
		}
		return trip.Trip{}, fmt.Errorf("insert trip: %w", err)
	}
	return created, nil
}

func (r *TripRepository) Get(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	query, args, err := psql.Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build select trip query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	t, err := scanTrip(executor(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, fmt.Errorf("trip %s: %w", id, trip.ErrNotFound)
	}
	if err != nil {
		return trip.Trip{}, fmt.Errorf("select trip: %w", err)
	}
	return t, nil
}

func (r *TripRepository) CompleteActive(ctx context.Context, id uuid.UUID, finishedAt time.Time) (trip.Trip, bool, error) {
	query, args, err := psql.Update("trips").
		Set("status", string(trip.StatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.Eq{"id": id, "status": string(trip.StatusActive)}).
		Suffix(returningTrip).
		ToSql()
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("build complete trip query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	t, err := scanTrip(executor(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, false, nil
	}
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("complete trip: %w", err)
	}
	return t, true, nil
}

func (r *TripRepository) AddStatusChange(ctx context.Context, c trip.StatusChange) error {
	var from *string
	if c.From != nil {
		s := string(*c.From)
		from = &s
	}
	query, args, err := psql.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason", "changed_at").
		Values(c.TripID, from, string(c.To), c.Reason, c.ChangedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status change query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := executor(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status change: %w", err)
	}
	return nil
}

func scanTrip(row pgx.Row) (trip.Trip, error) {
	var (
		t      trip.Trip
		status string
	)
	err := row.Scan(
		&t.ID, &t.UserID, &t.DriverID,
		&t.Start.Latitude, &t.Start.Longitude, &t.End.Latitude, &t.End.Longitude,
		&t.Price, &status, &t.StartedAt, &t.FinishedAt,
	)
	if err != nil {
		return trip.Trip{}, err
	}
	t.Status = trip.Status(status)
	return t, nil
}
