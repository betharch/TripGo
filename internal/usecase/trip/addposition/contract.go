//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package addposition

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

type (
	tripRepository interface {
		GetForUpdate(ctx context.Context, id uuid.UUID) (model.Trip, error)
		SavePosition(ctx context.Context, p model.Position) (int64, error)
		AdvanceLastPosition(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	positionRequests interface {
		Cancel(tripID uuid.UUID)
	}

	positionMetrics interface {
		PositionSaved(ctx context.Context)
	}
)
