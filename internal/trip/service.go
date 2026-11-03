package trip

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type Repository interface {
	Create(ctx context.Context, t Trip) (Trip, error)
	Get(ctx context.Context, id uuid.UUID) (Trip, error)
	CompleteActive(ctx context.Context, id uuid.UUID, finishedAt time.Time) (t Trip, ok bool, err error)
	AddStatusChange(ctx context.Context, c StatusChange) error
}

type Service struct {
	tx   TxManager
	repo Repository
}

func NewService(tx TxManager, repo Repository) *Service {
	return &Service{tx: tx, repo: repo}
}

func (s *Service) Create(ctx context.Context, in NewTrip) (Trip, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Trip{}, fmt.Errorf("generate trip id: %w", err)
	}
	t := Trip{
		ID:        id,
		UserID:    in.UserID,
		DriverID:  in.DriverID,
		Start:     in.Start,
		End:       in.End,
		Price:     in.Price,
		Status:    StatusActive,
		StartedAt: time.Now(),
	}

	var created Trip
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		saved, err := s.repo.Create(ctx, t)
		if err != nil {
			return err
		}
		created = saved
		return s.repo.AddStatusChange(ctx, StatusChange{
			TripID:    saved.ID,
			To:        StatusActive,
			Reason:    "trip created",
			ChangedAt: saved.StartedAt,
		})
	})
	if err != nil {
		return Trip{}, err
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Trip, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Finish(ctx context.Context, id uuid.UUID) (Trip, error) {
	var finished Trip
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		t, ok, err := s.repo.CompleteActive(ctx, id, time.Now())
		if err != nil {
			return err
		}
		if !ok {
			existing, err := s.repo.Get(ctx, id)
			if err != nil {
				return err
			}
			if existing.Status == StatusCompleted {
				return ErrCompleted
			}
			return fmt.Errorf("trip %s: unexpected status %q", id, existing.Status)
		}
		finished = t
		from := StatusActive
		return s.repo.AddStatusChange(ctx, StatusChange{
			TripID:    t.ID,
			From:      &from,
			To:        StatusCompleted,
			Reason:    "finished by driver",
			ChangedAt: *t.FinishedAt,
		})
	})
	if err != nil {
		return Trip{}, err
	}
	return finished, nil
}
