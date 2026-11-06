package get_test

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
	"github.com/betharch/TripGo/internal/http/trip/get"
	"github.com/betharch/TripGo/internal/model"
)

var id = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")

func serve(uc *Mockusecase) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tripId", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	get.New(uc, slog.New(slog.DiscardHandler)).Handle(rec, req, id)
	return rec
}

func TestGetReturnsTrip(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	finishedAt := time.Date(2026, 11, 6, 12, 25, 0, 0, time.UTC)
	uc.EXPECT().Get(gomock.Any(), id).Return(model.Trip{
		ID: id, Status: trip.StatusCompleted, Price: 1450,
		StartedAt: finishedAt.Add(-25 * time.Minute), FinishedAt: &finishedAt,
	}, nil)

	rec := serve(uc)

	assert.Equal(t, http.StatusOK, rec.Code)
	var got api.Trip
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, id, got.Id)
	assert.Equal(t, api.Completed, got.Status)
	gotFinished, err := got.FinishedAt.Get()
	require.NoError(t, err)
	assert.Equal(t, finishedAt, gotFinished)
}

func TestGetMissingTripIs404(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Get(gomock.Any(), id).Return(model.Trip{}, fmt.Errorf("trip %s: %w", id, trip.ErrTripNotFound))

	rec := serve(uc)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "trip_not_found", p.Code)
	require.NotNil(t, p.Instance)
	assert.Equal(t, "/api/v1/trips/"+id.String(), *p.Instance)
}
