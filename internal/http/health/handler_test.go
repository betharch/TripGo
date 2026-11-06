package health_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/health"
)

func status(t *testing.T, rec *httptest.ResponseRecorder) api.HealthResponseStatus {
	t.Helper()
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var body api.HealthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	return body.Status
}

func TestHealthDoesNotTouchDatabase(t *testing.T) {
	db := NewMockpinger(gomock.NewController(t))
	rec := httptest.NewRecorder()

	health.New(db, time.Second, slog.New(slog.DiscardHandler)).Health(rec, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, api.Ok, status(t, rec))
}

func TestReady(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantCode   int
		wantStatus api.HealthResponseStatus
	}{
		{name: "database is up", pingErr: nil, wantCode: http.StatusOK, wantStatus: api.Ok},
		{name: "database is down", pingErr: errors.New("connection refused"), wantCode: http.StatusServiceUnavailable, wantStatus: api.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewMockpinger(gomock.NewController(t))
			db.EXPECT().Ping(gomock.Any()).Return(tt.pingErr)
			rec := httptest.NewRecorder()

			health.New(db, time.Second, slog.New(slog.DiscardHandler)).Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", http.NoBody))

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Equal(t, tt.wantStatus, status(t, rec))
		})
	}
}
