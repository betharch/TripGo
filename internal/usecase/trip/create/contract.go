//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/betharch/TripGo/internal/model"
)

type (
	tripRepository interface {
		Save(ctx context.Context, t model.Trip) error
		AddStatusChange(ctx context.Context, c model.StatusChange) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		TripCreated(ctx context.Context)
	}
)
