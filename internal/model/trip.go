package model

import (
	"time"

	"github.com/google/uuid"
)

type TripStatus string

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
	Status     TripStatus
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
	From      *TripStatus
	To        TripStatus
	Reason    string
	ChangedAt time.Time
}
