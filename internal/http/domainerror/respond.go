package domainerror

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
)

func Respond(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	status, body := Problem(err)
	if status >= http.StatusInternalServerError {
		log.ErrorContext(r.Context(), "request failed", "error", err)
		trace.SpanFromContext(r.Context()).SetAttributes(semconv.ErrorTypeKey.String(strconv.Itoa(status)))
	}
	send(w, r, status, body)
}

func Write(w http.ResponseWriter, r *http.Request, err error) {
	status, body := Problem(err)
	send(w, r, status, body)
}

func ParamError(w http.ResponseWriter, r *http.Request, err error) {
	invalid := &trip.ValidationError{Field: "request parameters", Reason: "are invalid"}
	var formatErr *api.InvalidParamFormatError
	if errors.As(err, &formatErr) {
		invalid = &trip.ValidationError{Field: formatErr.ParamName, Reason: "must be a UUID"}
	}
	Write(w, r, invalid)
}

func send(w http.ResponseWriter, r *http.Request, status int, body api.Problem) {
	instance := r.URL.Path
	body.Instance = &instance
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
