package trip

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

var (
	ErrNotFound   = errors.New("trip not found")
	ErrCompleted  = errors.New("trip already completed")
	ErrDriverBusy = errors.New("driver is busy")
)

type Point struct {
	Latitude  float64
	Longitude float64
}

type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	Start      Point
	End        Point
	Price      int64
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time
}

type NewTrip struct {
	UserID   uuid.UUID
	DriverID uuid.UUID
	Start    Point
	End      Point
	Price    int64
}

type StatusChange struct {
	TripID    uuid.UUID
	From      *Status
	To        Status
	Reason    string
	ChangedAt time.Time
}
