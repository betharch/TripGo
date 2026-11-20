package listpositions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/trip/listpositions"
	"github.com/betharch/TripGo/internal/model"
)

var tripID = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")

func serve(uc *Mockusecase, pathID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+pathID+"/positions", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tripId", pathID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	listpositions.New(uc, slog.New(slog.DiscardHandler)).Handle(rec, req, tripID)
	return rec
}

func TestListPositionsReturnsRouteInGivenOrder(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	recordedAt := time.Date(2026, 11, 20, 15, 0, 5, 0, time.FixedZone("", 3*60*60))
	uc.EXPECT().List(gomock.Any(), tripID).Return([]model.Position{
		{ID: 7, TripID: tripID, Point: model.Point{Latitude: 59.93, Longitude: 30.33}, RecordedAt: recordedAt},
		{ID: 3, TripID: tripID, Point: model.Point{Latitude: 59.94, Longitude: 30.34}, RecordedAt: recordedAt.Add(5 * time.Second)},
	}, nil)

	rec := serve(uc, tripID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"positions": [
		{"id": 7, "trip_id": "0190f7a2-0000-7000-8000-000000000001",
		 "coordinates": {"latitude": 59.93, "longitude": 30.33}, "recorded_at": "2026-11-20T12:00:05Z"},
		{"id": 3, "trip_id": "0190f7a2-0000-7000-8000-000000000001",
		 "coordinates": {"latitude": 59.94, "longitude": 30.34}, "recorded_at": "2026-11-20T12:00:10Z"}
	]}`, rec.Body.String())
}

func TestListPositionsOfTripWithoutPositionsIsEmptyArray(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().List(gomock.Any(), tripID).Return(nil, nil)

	rec := serve(uc, tripID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"positions": []}`, rec.Body.String())
}

func TestListPositionsOfMissingTripIs404(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().List(gomock.Any(), tripID).Return(nil, fmt.Errorf("trip %s: %w", tripID, trip.ErrTripNotFound))

	rec := serve(uc, tripID.String())

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "trip_not_found", p.Code)
}

func TestListPositionsRejectsNonCanonicalTripID(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))

	rec := serve(uc, "0190f7a2000070008000000000000001")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "invalid_request", p.Code)
}
