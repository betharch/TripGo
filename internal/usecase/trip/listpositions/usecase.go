package listpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

type Usecase struct {
	repo tripRepository
}

func New(repo tripRepository) *Usecase {
	return &Usecase{repo: repo}
}

func (u *Usecase) List(ctx context.Context, tripID uuid.UUID) ([]model.Position, error) {
	if _, err := u.repo.Get(ctx, tripID); err != nil {
		return nil, err
	}
	return u.repo.ListPositions(ctx, tripID)
}
