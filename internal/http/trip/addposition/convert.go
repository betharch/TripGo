package addposition

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/model"
)

const maxBodyBytes = 1 << 20

type request struct {
	Coordinates *point  `json:"coordinates"`
	RecordedAt  *string `json:"recorded_at"`
}

type point struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

func checkTripID(r *http.Request) error {
	if len(chi.URLParam(r, "tripId")) != 36 {
		return &trip.ValidationError{Field: "tripId", Reason: "must be a UUID"}
	}
	return nil
}

func decode(w http.ResponseWriter, r *http.Request) (model.NewPosition, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	var req request
	if err := dec.Decode(&req); err != nil {
		return model.NewPosition{}, decodeError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return model.NewPosition{}, invalidBody()
	}
	return req.params()
}

func (req request) params() (model.NewPosition, error) {
	coordinates, err := req.Coordinates.parse()
	if err != nil {
		return model.NewPosition{}, err
	}
	if req.RecordedAt == nil {
		return model.NewPosition{}, required("recorded_at")
	}
	recordedAt, err := time.Parse(time.RFC3339, *req.RecordedAt)
	if err != nil {
		return model.NewPosition{}, &trip.ValidationError{Field: "recorded_at", Reason: "must be an RFC 3339 date-time"}
	}
	return model.NewPosition{Point: coordinates, RecordedAt: recordedAt}, nil
}

func (p *point) parse() (model.Point, error) {
	switch {
	case p == nil:
		return model.Point{}, required("coordinates")
	case p.Latitude == nil:
		return model.Point{}, required("coordinates.latitude")
	case p.Longitude == nil:
		return model.Point{}, required("coordinates.longitude")
	}
	return model.Point{Latitude: *p.Latitude, Longitude: *p.Longitude}, nil
}

func decodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &trip.ValidationError{
			Field:  "request body",
			Reason: fmt.Sprintf("must not exceed %d bytes", maxBodyBytes),
		}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return &trip.ValidationError{Field: typeErr.Field, Reason: "has invalid type"}
	}
	if name, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return &trip.ValidationError{Field: strings.Trim(name, `"`), Reason: "is not allowed"}
	}
	return invalidBody()
}

func required(field string) error {
	return &trip.ValidationError{Field: field, Reason: "is required"}
}

func invalidBody() error {
	return &trip.ValidationError{Field: "request body", Reason: "must be a single JSON object"}
}

func toResponse(p model.Position) api.TripPosition {
	return api.TripPosition{
		Id:          p.ID,
		TripId:      p.TripID,
		Coordinates: api.Coordinates{Latitude: p.Point.Latitude, Longitude: p.Point.Longitude},
		RecordedAt:  p.RecordedAt.UTC(),
	}
}
