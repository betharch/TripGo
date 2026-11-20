package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
)

func (r *Repo) Get(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	return r.get(ctx, id, false)
}

func (r *Repo) GetForUpdate(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	return r.get(ctx, id, true)
}

func (r *Repo) get(ctx context.Context, id uuid.UUID, forUpdate bool) (model.Trip, error) {
	builder := psql.Select(tripColumns...).From("trips").Where(sq.Eq{"id": id})
	if forUpdate {
		builder = builder.Suffix("FOR UPDATE")
	}
	query, args, err := builder.ToSql()
	if err != nil {
		return model.Trip{}, fmt.Errorf("build select trip query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	t, err := scanTrip(r.db(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Trip{}, fmt.Errorf("trip %s: %w: %w", id, trip.ErrTripNotFound, err)
	}
	if err != nil {
		return model.Trip{}, fmt.Errorf("select trip: %w", err)
	}
	return t, nil
}

func (r *Repo) ListPositions(ctx context.Context, tripID uuid.UUID) ([]model.Position, error) {
	query, args, err := psql.Select("id", "trip_id", "latitude", "longitude", "recorded_at").
		From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at", "id").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select positions query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select positions: %w", err)
	}
	positions, err := pgx.CollectRows(rows, scanPosition)
	if err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}
	return positions, nil
}

func (r *Repo) ListStale(ctx context.Context, staleAfter time.Duration, last *model.StaleTrip, limit int) ([]model.StaleTrip, error) {
	builder := psql.Select("id", "driver_id", lastSeenAt).
		From("trips").
		Where(activeOnly).
		Where(lastSeenAt+" < now() - ?::interval", staleAfter)
	if last != nil {
		builder = builder.Where("("+lastSeenAt+", id) > (?, ?)", last.LastSeenAt, last.ID)
	}
	query, args, err := builder.OrderBy(lastSeenAt, "id").Suffix("LIMIT ?", limit).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select stale trips query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select stale trips: %w", err)
	}
	stale, err := pgx.CollectRows(rows, scanStaleTrip)
	if err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return stale, nil
}
