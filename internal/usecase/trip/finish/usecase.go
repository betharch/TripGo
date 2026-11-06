package finish

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

func (u *Usecase) Finish(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	var finished model.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		stored, err := u.repo.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		t := trip.Trip(stored)
		if err := t.Finish(u.now().UTC().Truncate(time.Microsecond)); err != nil {
			return fmt.Errorf("finish trip %s: %w", id, err)
		}
		finished = model.Trip(t)
		if err := u.repo.Complete(ctx, finished); err != nil {
			return err
		}
		return u.repo.AddStatusChange(ctx, model.StatusChange{
			TripID:    finished.ID,
			From:      &stored.Status,
			To:        finished.Status,
			Reason:    "finished by driver",
			ChangedAt: *finished.FinishedAt,
		})
	})
	if err != nil {
		return model.Trip{}, err
	}

	u.metrics.TripCompleted(ctx)
	u.log.InfoContext(ctx, "trip completed", "trip_id", finished.ID.String())
	return finished, nil
}
