package container

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/health"
	createhttp "github.com/betharch/TripGo/internal/http/trip/create"
	finishhttp "github.com/betharch/TripGo/internal/http/trip/finish"
	gethttp "github.com/betharch/TripGo/internal/http/trip/get"
)

func do(method, path string) *httptest.ResponseRecorder {
	log := slog.New(slog.DiscardHandler)
	mux := newRouter(server{
		create: createhttp.New(nil, log),
		get:    gethttp.New(nil, log),
		finish: finishhttp.New(nil, log),
		health: health.New(nil, time.Second, log),
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, http.NoBody))
	return rec
}

func problem(t *testing.T, rec *httptest.ResponseRecorder) api.Problem {
	t.Helper()
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	return p
}

func TestUnknownRouteIs404Problem(t *testing.T) {
	rec := do(http.MethodGet, "/api/v1/nope")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", problem(t, rec).Code)
}

func TestWrongMethodIs405WithAllow(t *testing.T) {
	rec := do(http.MethodDelete, "/api/v1/trips")

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "POST", rec.Header().Get("Allow"))
	assert.Equal(t, "method_not_allowed", problem(t, rec).Code)
}

func TestTripIDMustBeCanonicalUUID(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "not a uuid", method: http.MethodGet, path: "/api/v1/trips/abc"},
		{name: "uuid without dashes", method: http.MethodGet, path: "/api/v1/trips/0190f7a2000070008000000000000001"},
		{name: "finish with braces", method: http.MethodPost, path: "/api/v1/trips/%7B0190f7a2-0000-7000-8000-000000000001%7D/finish"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(tt.method, tt.path)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			p := problem(t, rec)
			assert.Equal(t, "invalid_request", p.Code)
			require.NotNil(t, p.Detail)
			assert.Equal(t, "tripId must be a UUID", *p.Detail)
		})
	}
}

func TestHealthIsServed(t *testing.T) {
	rec := do(http.MethodGet, "/health")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}
