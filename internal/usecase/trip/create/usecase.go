package create

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
)

type Usecase struct {
	repo    tripRepository
	tx      txManager
	metrics tripMetrics
	log     *slog.Logger
	now     func() time.Time
}

func New(repo tripRepository, tx txManager, metrics tripMetrics, log *slog.Logger, now func() time.Time) *Usecase {
	return &Usecase{repo: repo, tx: tx, metrics: metrics, log: log, now: now}
}

func (u *Usecase) Create(ctx context.Context, in model.NewTrip) (model.Trip, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Trip{}, fmt.Errorf("generate trip id: %w", err)
	}
	t, err := trip.New(id, in, u.now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return model.Trip{}, err
	}
	created := model.Trip(t)

	err = u.tx.Do(ctx, func(ctx context.Context) error {
		if err := u.repo.Save(ctx, created); err != nil {
			return err
		}
		return u.repo.AddStatusChange(ctx, model.StatusChange{
			TripID:    created.ID,
			To:        trip.StatusActive,
			Reason:    "trip created",
			ChangedAt: created.StartedAt,
		})
	})
	if err != nil {
		return model.Trip{}, err
	}

	u.metrics.TripCreated(ctx)
	u.log.InfoContext(ctx, "trip created", "trip_id", created.ID.String())
	return created, nil
}
