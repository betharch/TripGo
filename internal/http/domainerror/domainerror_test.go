package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantType   string
		wantDetail string
	}{
		{
			name:       "validation error keeps its detail",
			err:        fmt.Errorf("create: %w", &trip.ValidationError{Field: "price", Reason: "must be greater than or equal to 0"}),
			wantStatus: http.StatusBadRequest, wantCode: "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "price must be greater than or equal to 0",
		},
		{
			name:       "trip not found",
			err:        fmt.Errorf("trip 42: %w", trip.ErrTripNotFound),
			wantStatus: http.StatusNotFound, wantCode: "trip_not_found",
			wantType:   "https://tripgo.example/problems/trip-not-found",
			wantDetail: "Trip was not found",
		},
		{
			name:       "trip completed",
			err:        fmt.Errorf("finish trip 42: %w", trip.ErrTripCompleted),
			wantStatus: http.StatusConflict, wantCode: "trip_completed",
			wantType:   "https://tripgo.example/problems/trip-completed",
			wantDetail: "Operation is not allowed for a completed trip",
		},
		{
			name:       "driver busy",
			err:        fmt.Errorf("%w: %w", trip.ErrDriverBusy, errors.New("duplicate key value")),
			wantStatus: http.StatusConflict, wantCode: "driver_busy",
			wantType:   "https://tripgo.example/problems/driver-busy",
			wantDetail: "Driver already has an active trip",
		},
		{
			name:       "unknown route",
			err:        domainerror.ErrRouteNotFound,
			wantStatus: http.StatusNotFound, wantCode: "not_found",
			wantType:   "https://tripgo.example/problems/not-found",
			wantDetail: "Route does not exist",
		},
		{
			name:       "method not allowed",
			err:        domainerror.ErrMethodNotAllowed,
			wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed",
			wantType:   "https://tripgo.example/problems/method-not-allowed",
			wantDetail: "Method is not allowed for this route",
		},
		{
			name:       "anything else is internal and hides details",
			err:        errors.New("insert trip: password=secret: connection refused"),
			wantStatus: http.StatusInternalServerError, wantCode: "internal_error",
			wantType:   "https://tripgo.example/problems/internal-error",
			wantDetail: "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := domainerror.Problem(tt.err)

			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantStatus, int(body.Status))
			assert.Equal(t, tt.wantCode, body.Code)
			assert.Equal(t, tt.wantType, body.Type)
			assert.NotEmpty(t, body.Title)
			require.NotNil(t, body.Detail)
			assert.Equal(t, tt.wantDetail, *body.Detail)
		})
	}
}
