package addposition_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/trip/addposition"
	"github.com/betharch/TripGo/internal/model"
)

const validBody = `{
	"coordinates": {"latitude": 59.9350, "longitude": 30.3350},
	"recorded_at": "2026-11-20T15:00:05.123456789+03:00"
}`

var tripID = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")

func wantInput() model.NewPosition {
	return model.NewPosition{
		Point:      model.Point{Latitude: 59.9350, Longitude: 30.3350},
		RecordedAt: time.Date(2026, 11, 20, 15, 0, 5, 123456789, time.FixedZone("", 3*60*60)),
	}
}

func serve(uc *Mockusecase, pathID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+pathID+"/positions", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tripId", pathID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	addposition.New(uc, slog.New(slog.DiscardHandler)).Handle(rec, req, tripID)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) api.Problem {
	t.Helper()
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	return p
}

func TestAddPositionReturns201WithPosition(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	var got model.NewPosition
	uc.EXPECT().Add(gomock.Any(), tripID, gomock.Any()).DoAndReturn(
		func(_ context.Context, id uuid.UUID, in model.NewPosition) (model.Position, error) {
			got = in
			return model.Position{ID: 42, TripID: id, Point: in.Point, RecordedAt: in.RecordedAt.Truncate(time.Microsecond)}, nil
		},
	)

	rec := serve(uc, tripID.String(), validBody)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, wantInput().Point, got.Point)
	assert.True(t, wantInput().RecordedAt.Equal(got.RecordedAt))
	assert.JSONEq(t, `{
		"id": 42,
		"trip_id": "0190f7a2-0000-7000-8000-000000000001",
		"coordinates": {"latitude": 59.9350, "longitude": 30.3350},
		"recorded_at": "2026-11-20T12:00:05.123456Z"
	}`, rec.Body.String())
}

func TestAddPositionRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name       string
		pathID     string
		body       string
		wantDetail string
	}{
		{name: "not json", body: `{"coordinates":`, wantDetail: "request body must be a single JSON object"},
		{name: "two objects", body: validBody + `{}`, wantDetail: "request body must be a single JSON object"},
		{name: "missing coordinates", body: `{"recorded_at": "2026-11-20T12:00:05Z"}`, wantDetail: "coordinates is required"},
		{name: "missing longitude", body: strings.Replace(validBody, `, "longitude": 30.3350`, ``, 1), wantDetail: "coordinates.longitude is required"},
		{name: "missing recorded_at", body: `{"coordinates": {"latitude": 59.9350, "longitude": 30.3350}}`, wantDetail: "recorded_at is required"},
		{name: "recorded_at is null", body: `{"coordinates": {"latitude": 1, "longitude": 1}, "recorded_at": null}`, wantDetail: "recorded_at is required"},
		{name: "recorded_at is not a date-time", body: strings.Replace(validBody, "2026-11-20T15:00:05.123456789+03:00", "yesterday", 1), wantDetail: "recorded_at must be an RFC 3339 date-time"},
		{name: "recorded_at without zone", body: strings.Replace(validBody, "+03:00", "", 1), wantDetail: "recorded_at must be an RFC 3339 date-time"},
		{name: "wrong type", body: strings.Replace(validBody, `"latitude": 59.9350`, `"latitude": "north"`, 1), wantDetail: "coordinates.latitude has invalid type"},
		{name: "unknown field", body: strings.Replace(validBody, `"recorded_at"`, `"speed": 60, "recorded_at"`, 1), wantDetail: "speed is not allowed"},
		{name: "non-canonical trip id", pathID: "0190f7a2000070008000000000000001", body: validBody, wantDetail: "tripId must be a UUID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			pathID := tt.pathID
			if pathID == "" {
				pathID = tripID.String()
			}

			rec := serve(uc, pathID, tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			p := decodeProblem(t, rec)
			assert.Equal(t, "invalid_request", p.Code)
			require.NotNil(t, p.Detail)
			assert.Equal(t, tt.wantDetail, *p.Detail)
		})
	}
}

func TestAddPositionMapsUsecaseErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "domain validation", err: &trip.ValidationError{Field: "recorded_at", Reason: "must not be more than a minute in the future"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "trip not found", err: fmt.Errorf("trip %s: %w", tripID, trip.ErrTripNotFound), wantStatus: http.StatusNotFound, wantCode: "trip_not_found"},
		{name: "trip completed", err: fmt.Errorf("add position to trip %s: %w", tripID, trip.ErrTripCompleted), wantStatus: http.StatusConflict, wantCode: "trip_completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Add(gomock.Any(), tripID, gomock.Any()).Return(model.Position{}, tt.err)

			rec := serve(uc, tripID.String(), validBody)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCode, decodeProblem(t, rec).Code)
		})
	}
}

func TestAddPositionHidesInternalErrorDetails(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Add(gomock.Any(), tripID, gomock.Any()).
		Return(model.Position{}, errors.New("insert position: dial tcp 10.0.0.7:5432: connect: connection refused"))

	rec := serve(uc, tripID.String(), validBody)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "10.0.0.7")
	assert.NotContains(t, rec.Body.String(), "connection refused")
	assert.Equal(t, "internal_error", decodeProblem(t, rec).Code)
}
