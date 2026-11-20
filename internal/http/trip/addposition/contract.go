//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package addposition

import (
	"context"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

type usecase interface {
	Add(ctx context.Context, tripID uuid.UUID, in model.NewPosition) (model.Position, error)
}
