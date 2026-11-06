package get_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/domain/trip"
	"github.com/betharch/TripGo/internal/model"
	"github.com/betharch/TripGo/internal/usecase/trip/get"
)

var id = uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")

func TestGetNotFound(t *testing.T) {
	repo := NewMocktripRepository(gomock.NewController(t))
	repo.EXPECT().Get(gomock.Any(), id).Return(model.Trip{}, fmt.Errorf("trip %s: %w", id, trip.ErrTripNotFound))

	_, err := get.New(repo).Get(context.Background(), id)

	require.ErrorIs(t, err, trip.ErrTripNotFound)
}
