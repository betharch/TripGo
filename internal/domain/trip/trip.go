package trip

import (
	"time"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

const (
	StatusActive    model.TripStatus = "active"
	StatusCompleted model.TripStatus = "completed"
)

type Trip model.Trip

func New(id uuid.UUID, in model.NewTrip, now time.Time) (Trip, error) {
	if err := validate(in); err != nil {
		return Trip{}, err
	}
	return Trip{
		ID:        id,
		UserID:    in.UserID,
		DriverID:  in.DriverID,
		Start:     in.Start,
		End:       in.End,
		Price:     in.Price,
		Status:    StatusActive,
		StartedAt: now,
	}, nil
}

func (t *Trip) Finish(at time.Time) error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	t.Status = StatusCompleted
	t.FinishedAt = &at
	return nil
}

func validate(in model.NewTrip) error {
	if in.UserID == uuid.Nil {
		return &ValidationError{Field: "user_id", Reason: "must not be the nil UUID"}
	}
	if in.DriverID == uuid.Nil {
		return &ValidationError{Field: "driver_id", Reason: "must not be the nil UUID"}
	}
	if err := validatePoint("start_point", in.Start); err != nil {
		return err
	}
	if err := validatePoint("end_point", in.End); err != nil {
		return err
	}
	if in.Price < 0 {
		return &ValidationError{Field: "price", Reason: "must be greater than or equal to 0"}
	}
	return nil
}

func validatePoint(name string, p model.Point) error {
	if p.Latitude < -90 || p.Latitude > 90 {
		return &ValidationError{Field: name + ".latitude", Reason: "must be between -90 and 90"}
	}
	if p.Longitude < -180 || p.Longitude > 180 {
		return &ValidationError{Field: name + ".longitude", Reason: "must be between -180 and 180"}
	}
	return nil
}
