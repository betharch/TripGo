//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

type (
	staleTrips interface {
		ListStale(ctx context.Context, staleAfter time.Duration, last *model.StaleTrip, limit int) ([]model.StaleTrip, error)
	}

	pushGateway interface {
		RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
	}

	metrics interface {
		StaleQueued(ctx context.Context)
		StaleDuplicate(ctx context.Context)
		StaleDropped(ctx context.Context)
		RowsRead(ctx context.Context, rows int)
		RequestSucceeded(ctx context.Context, took time.Duration)
		RequestFailed(ctx context.Context, took time.Duration)
		RequestCancelled(ctx context.Context)
	}

	ticker interface {
		C() <-chan time.Time
		Stop()
	}
)
