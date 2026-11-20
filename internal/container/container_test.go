package container

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/betharch/TripGo/internal/infra/telemetry"
	"github.com/betharch/TripGo/internal/model"
	"github.com/betharch/TripGo/internal/worker/positions"
)

const waitTimeout = 5 * time.Second

type staleTripsStub struct {
	trips []model.StaleTrip
}

func (s staleTripsStub) ListStale(_ context.Context, _ time.Duration, last *model.StaleTrip, _ int) ([]model.StaleTrip, error) {
	if last != nil {
		return nil, nil
	}
	return s.trips, nil
}

type pushStub func(ctx context.Context, tripID uuid.UUID) error

func (p pushStub) RequestPosition(ctx context.Context, tripID, _ uuid.UUID) error {
	return p(ctx, tripID)
}

type tickerStub struct {
	ticks chan time.Time
}

func (t tickerStub) C() <-chan time.Time { return t.ticks }

func (tickerStub) Stop() {}

func newPolling(t *testing.T, log *slog.Logger, push pushStub, trip model.StaleTrip) (*Container, chan<- time.Time) {
	t.Helper()
	metrics, err := telemetry.NewPositionMetrics(t.Context(), metricnoop.NewMeterProvider().Meter("test"))
	require.NoError(t, err)
	ticks := make(chan time.Time)
	c := &Container{
		Log: log,
		worker: positions.New(staleTripsStub{trips: []model.StaleTrip{trip}}, push, metrics,
			tickerStub{ticks: ticks}, tracenoop.NewTracerProvider().Tracer("test"), log,
			positions.Config{Workers: 1, QueueSize: 1, ScanBatchSize: 2, StaleAfter: time.Minute, PushTimeout: time.Hour}),
	}
	return c, ticks
}

func tick(t *testing.T, ticks chan<- time.Time, failure string) {
	t.Helper()
	select {
	case ticks <- time.Time{}:
	case <-time.After(waitTimeout):
		t.Fatal(failure)
	}
}

func receive[T any](t *testing.T, ch <-chan T, failure string) T {
	t.Helper()
	var got T
	select {
	case got = <-ch:
	case <-time.After(waitTimeout):
		t.Fatal(failure)
	}
	return got
}

type workerStub struct {
	done              chan struct{}
	scannerStopped    int
	cancelledInFlight int
}

func (w *workerStub) cancel() {
	select {
	case <-w.done:
	default:
		w.cancelledInFlight++
		close(w.done)
	}
}

func newContainer(logs *bytes.Buffer, w *workerStub) *Container {
	return &Container{
		Log:           slog.New(slog.NewJSONHandler(logs, nil)),
		workerDone:    w.done,
		stopScanner:   func() { w.scannerStopped++ },
		cancelWorkers: w.cancel,
	}
}

func TestDrainWithinBudgetDoesNotCancelRequestsInFlight(t *testing.T) {
	var logs bytes.Buffer
	w := &workerStub{done: make(chan struct{})}
	c := newContainer(&logs, w)
	c.stopScanner = func() {
		w.scannerStopped++
		close(w.done)
	}

	c.stopScannerAndDrainWorkers(t.Context())

	assert.Equal(t, 1, w.scannerStopped)
	assert.Zero(t, w.cancelledInFlight)
	assert.Empty(t, logs.String())
}

func TestExpiredBudgetCancelsRequestsInFlight(t *testing.T) {
	var logs bytes.Buffer
	w := &workerStub{done: make(chan struct{})}
	c := newContainer(&logs, w)
	expired, cancel := context.WithCancel(t.Context())
	cancel()

	c.stopScannerAndDrainWorkers(expired)

	assert.Equal(t, 1, w.scannerStopped)
	assert.Equal(t, 1, w.cancelledInFlight)
	assert.Contains(t, logs.String(), `"level":"ERROR"`)
	assert.Contains(t, logs.String(), "shutdown budget exceeded")
}

func TestShutdownStepIsSkippedWhenWorkersWereNotStarted(t *testing.T) {
	c := &Container{}

	assert.NotPanics(t, func() { c.stopScannerAndDrainWorkers(t.Context()) })
}

func TestWorkersOutliveSignalContextAndStopOnShutdown(t *testing.T) {
	trip := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	requested := make(chan uuid.UUID, 1)
	c, ticks := newPolling(t, slog.New(slog.DiscardHandler), func(_ context.Context, tripID uuid.UUID) error {
		requested <- tripID
		return nil
	}, trip)
	signal, interrupt := context.WithCancel(t.Context())

	c.Start(signal)
	interrupt()

	tick(t, ticks, "scanner stopped together with the signal context")
	assert.Equal(t, trip.ID, receive(t, requested, "worker stopped together with the signal context"))

	c.stopScannerAndDrainWorkers(t.Context())

	select {
	case <-c.workerDone:
	default:
		t.Fatal("workers are still running after shutdown")
	}
}

func TestExpiredBudgetAbortsRequestInFlightAndWaitsForWorkers(t *testing.T) {
	var logs bytes.Buffer
	trip := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	requested := make(chan uuid.UUID, 1)
	c, ticks := newPolling(t, slog.New(slog.NewJSONHandler(&logs, nil)), func(ctx context.Context, tripID uuid.UUID) error {
		requested <- tripID
		<-ctx.Done()
		return ctx.Err()
	}, trip)
	c.Start(t.Context())
	tick(t, ticks, "scanner did not take the tick")
	receive(t, requested, "worker did not start the request")
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	returned := make(chan struct{})

	go func() {
		defer close(returned)
		c.stopScannerAndDrainWorkers(expired)
	}()
	receive(t, returned, "request in flight was not aborted")

	select {
	case <-c.workerDone:
	default:
		t.Fatal("shutdown step returned before the workers stopped")
	}
	assert.Contains(t, logs.String(), "shutdown budget exceeded")
	assert.Contains(t, logs.String(), "context canceled")
}
