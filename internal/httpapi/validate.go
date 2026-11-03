package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/trip"
)

const maxBodyBytes = 1 << 20

var (
	tripDataFields   = schemaFields(reflect.TypeFor[api.TripData]())
	coordinateFields = schemaFields(reflect.TypeFor[api.Coordinates]())
)

type fieldSet struct {
	allowed  []string
	required []string
}

func schemaFields(t reflect.Type) fieldSet {
	var fs fieldSet
	for i := range t.NumField() {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		fs.allowed = append(fs.allowed, name)
		if !strings.Contains(opts, "omitempty") {
			fs.required = append(fs.required, name)
		}
	}
	return fs
}

type validationError struct {
	msg string
}

func (e *validationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &validationError{msg: fmt.Sprintf(format, args...)}
}

func decodeTripData(w http.ResponseWriter, r *http.Request) (trip.NewTrip, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return trip.NewTrip{}, invalid("request body must not exceed %d bytes", maxBodyBytes)
		}
		return trip.NewTrip{}, invalid("cannot read request body")
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return trip.NewTrip{}, invalid("request body must be a single JSON object")
	}
	if err := checkFields("", top, tripDataFields); err != nil {
		return trip.NewTrip{}, err
	}
	for _, name := range []string{"start_point", "end_point"} {
		var point map[string]json.RawMessage
		if err := json.Unmarshal(top[name], &point); err != nil {
			return trip.NewTrip{}, invalid("%s must be an object", name)
		}
		if err := checkFields(name+".", point, coordinateFields); err != nil {
			return trip.NewTrip{}, err
		}
	}

	for _, name := range []string{"user_id", "driver_id"} {
		if err := checkUUID(name, top[name]); err != nil {
			return trip.NewTrip{}, err
		}
	}

	var data api.TripData
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			return trip.NewTrip{}, invalid("%s has invalid type", typeErr.Field)
		}
		return trip.NewTrip{}, invalid("request body does not match the TripData schema")
	}

	if err := checkPoint("start_point", data.StartPoint); err != nil {
		return trip.NewTrip{}, err
	}
	if err := checkPoint("end_point", data.EndPoint); err != nil {
		return trip.NewTrip{}, err
	}
	if data.Price < 0 {
		return trip.NewTrip{}, invalid("price must be greater than or equal to 0")
	}

	return trip.NewTrip{
		UserID:   data.UserId,
		DriverID: data.DriverId,
		Start:    trip.Point{Latitude: data.StartPoint.Latitude, Longitude: data.StartPoint.Longitude},
		End:      trip.Point{Latitude: data.EndPoint.Latitude, Longitude: data.EndPoint.Longitude},
		Price:    data.Price,
	}, nil
}

func checkFields(prefix string, obj map[string]json.RawMessage, fields fieldSet) error {
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		if !slices.Contains(fields.allowed, key) {
			return invalid("unknown field %q", prefix+key)
		}
	}
	for _, name := range fields.required {
		raw, ok := obj[name]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return invalid("%s%s is required", prefix, name)
		}
	}
	return nil
}

func checkUUID(name string, raw json.RawMessage) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return invalid("%s must be a string", name)
	}
	if !isCanonicalUUID(s) {
		return invalid("%s must be a UUID in the form xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", name)
	}
	if s == uuid.Nil.String() {
		return invalid("%s must not be the nil UUID", name)
	}
	return nil
}

func isCanonicalUUID(s string) bool {
	return len(s) == 36 && uuid.Validate(s) == nil
}

func checkPoint(name string, p api.Coordinates) error {
	if p.Latitude < -90 || p.Latitude > 90 {
		return invalid("%s.latitude must be between -90 and 90", name)
	}
	if p.Longitude < -180 || p.Longitude > 180 {
		return invalid("%s.longitude must be between -180 and 180", name)
	}
	return nil
}
