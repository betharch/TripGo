package trip

import (
	"context"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/betharch/TripGo/internal/infra/postgres/transaction"
)

const (
	codeUniqueViolation   = "23505"
	driverActiveTripIndex = "trips_driver_active_uniq"
)

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

type Repo struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func New(pool *pgxpool.Pool, timeout time.Duration) *Repo {
	return &Repo{pool: pool, timeout: timeout}
}

func (r *Repo) db(ctx context.Context) querier {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return r.pool
}
