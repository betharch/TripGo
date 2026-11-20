//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package listpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

type usecase interface {
	List(ctx context.Context, tripID uuid.UUID) ([]model.Position, error)
}
