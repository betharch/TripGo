package trip

import (
	"time"

	"github.com/google/uuid"

	"github.com/betharch/TripGo/internal/model"
)

const maxRecordedAhead = time.Minute

func NewPosition(tripID uuid.UUID, in model.NewPosition, now time.Time) (model.Position, error) {
	if err := validatePoint("coordinates", in.Point); err != nil {
		return model.Position{}, err
	}
	if in.RecordedAt.After(now.Add(maxRecordedAhead)) {
		return model.Position{}, &ValidationError{
			Field:  "recorded_at",
			Reason: "must not be more than a minute in the future",
		}
	}
	recordedAt := in.RecordedAt.UTC().Truncate(time.Microsecond)
	if recordedAt.Year() < 0 {
		return model.Position{}, &ValidationError{
			Field:  "recorded_at",
			Reason: "must not be before year 0000 in UTC",
		}
	}
	return model.Position{TripID: tripID, Point: in.Point, RecordedAt: recordedAt}, nil
}

func (t *Trip) AcceptPosition() error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	return nil
}
