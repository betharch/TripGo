package addposition

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
)

type Usecase struct {
	repo     tripRepository
	tx       txManager
	requests positionRequests
	metrics  positionMetrics
	now      func() time.Time
}

func New(repo tripRepository, tx txManager, requests positionRequests, metrics positionMetrics, now func() time.Time) *Usecase {
	return &Usecase{repo: repo, tx: tx, requests: requests, metrics: metrics, now: now}
}

func (u *Usecase) Add(ctx context.Context, tripID uuid.UUID, in model.NewPosition) (model.Position, error) {
	position, err := trip.NewPosition(tripID, in, u.now())
	if err != nil {
		return model.Position{}, err
	}
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		stored, err := u.repo.GetForUpdate(ctx, tripID)
		if err != nil {
			return err
		}
		locked := trip.Trip(stored)
		if err := locked.AcceptPosition(); err != nil {
			return fmt.Errorf("add position to trip %s: %w", tripID, err)
		}
		if position.ID, err = u.repo.SavePosition(ctx, position); err != nil {
			return err
		}
		return u.repo.AdvanceLastPosition(ctx, tripID, position.RecordedAt)
	})
	if err != nil {
		return model.Position{}, err
	}

	u.requests.Cancel(tripID)
	u.metrics.PositionSaved(ctx)
	return position, nil
}
