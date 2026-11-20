package model

import (
	"time"

	"github.com/google/uuid"
)

type Position struct {
	ID         int64
	TripID     uuid.UUID
	Point      Point
	RecordedAt time.Time
}

type NewPosition struct {
	Point      Point
	RecordedAt time.Time
}

type StaleTrip struct {
	ID         uuid.UUID
	DriverID   uuid.UUID
	LastSeenAt time.Time
}
