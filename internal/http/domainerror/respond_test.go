package domainerror_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/http/domainerror"
)

func TestRespondLogsOnlyServerErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantLogs   int
	}{
		{name: "client error is not logged", err: trip.ErrTripNotFound, wantStatus: http.StatusNotFound, wantLogs: 0},
		{name: "server error is logged once", err: errors.New("select trip: connection reset"), wantStatus: http.StatusInternalServerError, wantLogs: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			rec := httptest.NewRecorder()

			domainerror.Respond(rec, httptest.NewRequest(http.MethodGet, "/api/v1/trips", http.NoBody),
				slog.New(slog.NewJSONHandler(&logs, nil)), tt.err)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantLogs, strings.Count(logs.String(), `"msg":"request failed"`))
		})
	}
}
