//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/betharch/TripGo/internal/model"
)

type usecase interface {
	Create(ctx context.Context, in model.NewTrip) (model.Trip, error)
}
