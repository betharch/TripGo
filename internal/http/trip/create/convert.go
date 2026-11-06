package create

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/model"
)

const maxBodyBytes = 1 << 20

type request struct {
	UserID     *string `json:"user_id"`
	DriverID   *string `json:"driver_id"`
	StartPoint *point  `json:"start_point"`
	EndPoint   *point  `json:"end_point"`
	Price      *int64  `json:"price"`
}

type point struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

func decode(w http.ResponseWriter, r *http.Request) (model.NewTrip, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	var req request
	if err := dec.Decode(&req); err != nil {
		return model.NewTrip{}, decodeError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return model.NewTrip{}, invalidBody()
	}
	return req.params()
}

func (req request) params() (model.NewTrip, error) {
	userID, err := parseID("user_id", req.UserID)
	if err != nil {
		return model.NewTrip{}, err
	}
	driverID, err := parseID("driver_id", req.DriverID)
	if err != nil {
		return model.NewTrip{}, err
	}
	start, err := req.StartPoint.parse("start_point")
	if err != nil {
		return model.NewTrip{}, err
	}
	end, err := req.EndPoint.parse("end_point")
	if err != nil {
		return model.NewTrip{}, err
	}
	if req.Price == nil {
		return model.NewTrip{}, required("price")
	}
	return model.NewTrip{UserID: userID, DriverID: driverID, Start: start, End: end, Price: *req.Price}, nil
}

func parseID(field string, value *string) (uuid.UUID, error) {
	if value == nil {
		return uuid.Nil, required(field)
	}
	id, err := uuid.Parse(*value)
	if err != nil || len(*value) != 36 {
		return uuid.Nil, &trip.ValidationError{
			Field:  field,
			Reason: "must be a UUID in the form xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		}
	}
	return id, nil
}

func (p *point) parse(field string) (model.Point, error) {
	switch {
	case p == nil:
		return model.Point{}, required(field)
	case p.Latitude == nil:
		return model.Point{}, required(field + ".latitude")
	case p.Longitude == nil:
		return model.Point{}, required(field + ".longitude")
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

func toResponse(t model.Trip) api.Trip {
	finishedAt := nullable.NewNullNullable[time.Time]()
	if t.FinishedAt != nil {
		finishedAt = nullable.NewNullableWithValue(t.FinishedAt.UTC())
	}
	return api.Trip{
		Id:             t.ID,
		UserId:         t.UserID,
		DriverId:       t.DriverID,
		StartPoint:     api.Coordinates{Latitude: t.Start.Latitude, Longitude: t.Start.Longitude},
		EndPoint:       api.Coordinates{Latitude: t.End.Latitude, Longitude: t.End.Longitude},
		Price:          t.Price,
		Status:         api.TripStatus(t.Status),
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     finishedAt,
		LastPositionAt: nullable.NewNullNullable[time.Time](),
	}
}
