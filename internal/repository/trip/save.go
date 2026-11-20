package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
)

func (r *Repo) Save(ctx context.Context, t model.Trip) error {
	query, args, err := psql.Insert("trips").
		Columns("id", "user_id", "driver_id",
			"start_latitude", "start_longitude", "end_latitude", "end_longitude",
			"price", "status", "started_at").
		Values(t.ID, t.UserID, t.DriverID,
			t.Start.Latitude, t.Start.Longitude, t.End.Latitude, t.End.Longitude,
			t.Price, string(t.Status), t.StartedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db(ctx).Exec(ctx, query, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation &&
			pgErr.ConstraintName == driverActiveTripIndex {
			return fmt.Errorf("%w: %w", trip.ErrDriverBusy, err)
		}
		return fmt.Errorf("insert trip: %w", err)
	}
	return nil
}

func (r *Repo) Complete(ctx context.Context, t model.Trip) error {
	query, args, err := psql.Update("trips").
		Set("status", string(t.Status)).
		Set("finished_at", t.FinishedAt).
		Set("updated_at", t.FinishedAt).
		Where(sq.Eq{"id": t.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build complete trip query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("complete trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("complete trip %s: %w", t.ID, trip.ErrTripNotFound)
	}
	return nil
}

func (r *Repo) SavePosition(ctx context.Context, p model.Position) (int64, error) {
	query, args, err := psql.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(p.TripID, p.Point.Latitude, p.Point.Longitude, p.RecordedAt).
		Suffix("RETURNING id").
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build insert position query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id int64
	if err := r.db(ctx).QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert position: %w", err)
	}
	return id, nil
}

func (r *Repo) AdvanceLastPosition(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error {
	query, args, err := psql.Update("trips").
		Set("last_position_at", sq.Expr("GREATEST(last_position_at, ?)", recordedAt)).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": tripID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build advance last position query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("advance last position: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("advance last position of trip %s: %w", tripID, trip.ErrTripNotFound)
	}
	return nil
}

func (r *Repo) AddStatusChange(ctx context.Context, c model.StatusChange) error {
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
	if _, err := r.db(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status change: %w", err)
	}
	return nil
}
