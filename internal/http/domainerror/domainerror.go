package domainerror

import (
	"errors"
	"net/http"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
)

var (
	ErrRouteNotFound    = errors.New("route not found")
	ErrMethodNotAllowed = errors.New("method not allowed")
)

type kind struct {
	status  int32
	typeURI string
	title   string
	code    string
}

var (
	invalidRequest = kind{
		http.StatusBadRequest,
		"https://tripgo.example/problems/invalid-request", "Invalid request", "invalid_request",
	}
	tripNotFound = kind{
		http.StatusNotFound,
		"https://tripgo.example/problems/trip-not-found", "Trip not found", "trip_not_found",
	}
	tripCompleted = kind{
		http.StatusConflict,
		"https://tripgo.example/problems/trip-completed", "Trip completed", "trip_completed",
	}
	driverBusy = kind{
		http.StatusConflict,
		"https://tripgo.example/problems/driver-busy", "Driver busy", "driver_busy",
	}
	internalError = kind{
		http.StatusInternalServerError,
		"https://tripgo.example/problems/internal-error", "Internal Server Error", "internal_error",
	}
	routeNotFound = kind{
		http.StatusNotFound,
		"https://tripgo.example/problems/not-found", "Not found", "not_found",
	}
	methodNotAllowed = kind{
		http.StatusMethodNotAllowed,
		"https://tripgo.example/problems/method-not-allowed", "Method not allowed", "method_not_allowed",
	}
)

func Problem(err error) (status int, body api.Problem) {
	var invalid *trip.ValidationError
	switch {
	case errors.As(err, &invalid):
		return invalidRequest.problem(invalid.Error())
	case errors.Is(err, trip.ErrTripNotFound):
		return tripNotFound.problem("Trip was not found")
	case errors.Is(err, trip.ErrTripCompleted):
		return tripCompleted.problem("Operation is not allowed for a completed trip")
	case errors.Is(err, trip.ErrDriverBusy):
		return driverBusy.problem("Driver already has an active trip")
	case errors.Is(err, ErrRouteNotFound):
		return routeNotFound.problem("Route does not exist")
	case errors.Is(err, ErrMethodNotAllowed):
		return methodNotAllowed.problem("Method is not allowed for this route")
	default:
		return internalError.problem("Internal server error")
	}
}

func (k kind) problem(detail string) (status int, body api.Problem) {
	return int(k.status), api.Problem{
		Type:   k.typeURI,
		Title:  k.title,
		Status: k.status,
		Detail: &detail,
		Code:   k.code,
	}
}
