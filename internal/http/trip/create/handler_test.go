package create_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/trip/create"
	"github.com/betharch/TripGo/internal/model"
)

const validBody = `{
	"user_id": "5cb72c04-7650-45c9-a79b-bcdba0631e0c",
	"driver_id": "8860b315-ec86-42eb-a17c-7c163d721ff5",
	"start_point": {"latitude": 59.9398, "longitude": 30.3146},
	"end_point": {"latitude": 59.9290, "longitude": 30.3626},
	"price": 0
}`

func wantInput() model.NewTrip {
	return model.NewTrip{
		UserID:   uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID: uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		Start:    model.Point{Latitude: 59.9398, Longitude: 30.3146},
		End:      model.Point{Latitude: 59.9290, Longitude: 30.3626},
		Price:    0,
	}
}

func serve(t *testing.T, uc *Mockusecase, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(body))
	rec := httptest.NewRecorder()
	create.New(uc, slog.New(slog.DiscardHandler)).Handle(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) api.Problem {
	t.Helper()
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	return p
}

func TestCreateReturns201WithTrip(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	id := uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")
	startedAt := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	uc.EXPECT().Create(gomock.Any(), wantInput()).DoAndReturn(
		func(_ any, in model.NewTrip) (model.Trip, error) {
			t, err := trip.New(id, in, startedAt)
			return model.Trip(t), err
		},
	)

	rec := serve(t, uc, validBody)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "/api/v1/trips/"+id.String(), rec.Header().Get("Location"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, id.String(), got["id"])
	assert.Equal(t, "active", got["status"])
	assert.InDelta(t, 0, got["price"], 0)
	assert.Contains(t, got, "finished_at")
	assert.Nil(t, got["finished_at"])
	assert.Contains(t, got, "last_position_at")
}

func TestCreateRejectsInvalidBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{name: "not json", body: `{"user_id":`, wantDetail: "request body must be a single JSON object"},
		{name: "two objects", body: validBody + `{}`, wantDetail: "request body must be a single JSON object"},
		{name: "missing price", body: strings.Replace(validBody, `"price": 0`, `"price": null`, 1), wantDetail: "price is required"},
		{name: "missing point coordinate", body: strings.Replace(validBody, `"latitude": 59.9398, `, ``, 1), wantDetail: "start_point.latitude is required"},
		{name: "unknown field", body: strings.Replace(validBody, `"price": 0`, `"price": 0, "promo": "x"`, 1), wantDetail: "promo is not allowed"},
		{name: "wrong type", body: strings.Replace(validBody, `"price": 0`, `"price": "free"`, 1), wantDetail: "price has invalid type"},
		{name: "non-canonical uuid", body: strings.Replace(validBody, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", "5cb72c04765045c9a79bbcdba0631e0c", 1), wantDetail: "user_id must be a UUID in the form xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))

			rec := serve(t, uc, tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			p := decodeProblem(t, rec)
			assert.Equal(t, "invalid_request", p.Code)
			require.NotNil(t, p.Detail)
			assert.Equal(t, tt.wantDetail, *p.Detail)
		})
	}
}

func TestCreateMapsUsecaseErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "domain validation", err: &trip.ValidationError{Field: "price", Reason: "must be greater than or equal to 0"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "driver busy", err: fmt.Errorf("%w: %w", trip.ErrDriverBusy, errors.New("duplicate key")), wantStatus: http.StatusConflict, wantCode: "driver_busy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Create(gomock.Any(), gomock.Any()).Return(model.Trip{}, tt.err)

			rec := serve(t, uc, validBody)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCode, decodeProblem(t, rec).Code)
		})
	}
}

func TestCreateHidesInternalErrorDetails(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Create(gomock.Any(), gomock.Any()).
		Return(model.Trip{}, errors.New("insert trip: dial tcp 10.0.0.7:5432: connect: connection refused"))

	rec := serve(t, uc, validBody)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "10.0.0.7")
	assert.NotContains(t, rec.Body.String(), "connection refused")
	assert.Equal(t, "internal_error", decodeProblem(t, rec).Code)
}
