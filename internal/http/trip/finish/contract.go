//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

type usecase interface {
	Finish(ctx context.Context, id uuid.UUID) (model.Trip, error)
}
