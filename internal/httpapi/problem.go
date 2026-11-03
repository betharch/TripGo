package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/trip"
)

type problemKind struct {
	status  int32
	typeURI string
	title   string
	code    string
}

var (
	problemInvalidRequest = problemKind{
		http.StatusBadRequest,
		"https://tripgo.example/problems/invalid-request", "Invalid request", "invalid_request",
	}
	problemTripNotFound = problemKind{
		http.StatusNotFound,
		"https://tripgo.example/problems/trip-not-found", "Trip not found", "trip_not_found",
	}
	problemTripCompleted = problemKind{
		http.StatusConflict,
		"https://tripgo.example/problems/trip-completed", "Trip completed", "trip_completed",
	}
	problemDriverBusy = problemKind{
		http.StatusConflict,
		"https://tripgo.example/problems/driver-busy", "Driver busy", "driver_busy",
	}
	problemInternal = problemKind{
		http.StatusInternalServerError,
		"https://tripgo.example/problems/internal-error", "Internal Server Error", "internal_error",
	}

	problemRouteNotFound = problemKind{
		http.StatusNotFound,
		"https://tripgo.example/problems/not-found", "Not found", "not_found",
	}
	problemMethodNotAllowed = problemKind{
		http.StatusMethodNotAllowed,
		"https://tripgo.example/problems/method-not-allowed", "Method not allowed", "method_not_allowed",
	}
)

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, trip.ErrNotFound):
		writeProblem(w, r, problemTripNotFound, "Trip was not found")
	case errors.Is(err, trip.ErrCompleted):
		writeProblem(w, r, problemTripCompleted, "Operation is not allowed for a completed trip")
	case errors.Is(err, trip.ErrDriverBusy):
		writeProblem(w, r, problemDriverBusy, "Driver already has an active trip")
	default:
		h.log.ErrorContext(r.Context(), "request failed",
			"method", r.Method, "path", r.URL.Path, "err", err)
		writeProblem(w, r, problemInternal, "Internal server error")
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, kind problemKind, detail string) {
	instance := r.URL.Path
	writeBody(w, "application/problem+json", int(kind.status), api.Problem{
		Type:     kind.typeURI,
		Title:    kind.title,
		Status:   kind.status,
		Detail:   &detail,
		Instance: &instance,
		Code:     kind.code,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	writeBody(w, "application/json", status, body)
}

func writeBody(w http.ResponseWriter, contentType string, status int, body any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
