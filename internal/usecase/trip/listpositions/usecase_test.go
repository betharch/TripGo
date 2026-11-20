package listpositions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
	"github.com/betharch/TripGo/internal/usecase/trip/listpositions"
)

var tripID = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")

func TestListPositionsOfExistingTrip(t *testing.T) {
	repo := NewMocktripRepository(gomock.NewController(t))
	route := []model.Position{
		{ID: 1, TripID: tripID, RecordedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)},
		{ID: 2, TripID: tripID, RecordedAt: time.Date(2026, 11, 20, 12, 0, 5, 0, time.UTC)},
	}
	gomock.InOrder(
		repo.EXPECT().Get(gomock.Any(), tripID).Return(model.Trip{ID: tripID, Status: trip.StatusCompleted}, nil),
		repo.EXPECT().ListPositions(gomock.Any(), tripID).Return(route, nil),
	)

	got, err := listpositions.New(repo).List(context.Background(), tripID)

	require.NoError(t, err)
	assert.Equal(t, route, got)
}

func TestListPositionsOfMissingTrip(t *testing.T) {
	repo := NewMocktripRepository(gomock.NewController(t))
	repo.EXPECT().Get(gomock.Any(), tripID).Return(model.Trip{}, fmt.Errorf("trip %s: %w", tripID, trip.ErrTripNotFound))

	_, err := listpositions.New(repo).List(context.Background(), tripID)

	require.ErrorIs(t, err, trip.ErrTripNotFound)
}
