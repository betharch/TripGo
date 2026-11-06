package trip

import (
	"context"
	"errors"
	"fmt"

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
