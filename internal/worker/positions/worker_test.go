package positions_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	"github.com/betharch/TripGo/internal/model"
	"github.com/betharch/TripGo/internal/worker/positions"
)

const (
	staleAfter  = time.Minute
	pushTimeout = time.Second
	waitTimeout = 5 * time.Second
	batchSize   = 100
)

var (
	seenAt = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	tripA = model.StaleTrip{
		ID:         uuid.MustParse("0190f7a2-0000-7000-8000-00000000000a"),
		DriverID:   uuid.MustParse("8860b315-ec86-42eb-a17c-00000000000a"),
		LastSeenAt: seenAt,
	}
	tripB = model.StaleTrip{
		ID:         uuid.MustParse("0190f7a2-0000-7000-8000-00000000000b"),
		DriverID:   uuid.MustParse("8860b315-ec86-42eb-a17c-00000000000b"),
		LastSeenAt: seenAt.Add(time.Second),
	}
	tripC = model.StaleTrip{
		ID:         uuid.MustParse("0190f7a2-0000-7000-8000-00000000000c"),
		DriverID:   uuid.MustParse("8860b315-ec86-42eb-a17c-00000000000c"),
		LastSeenAt: seenAt.Add(2 * time.Second),
	}
)

type harness struct {
	cfg         positions.Config
	trips       *MockstaleTrips
	push        *MockpushGateway
	metrics     *Mockmetrics
	worker      *positions.Worker
	ticks       chan time.Time
	idle        chan struct{}
	rowsRead    []int
	stopScanner context.CancelFunc
	cancel      context.CancelFunc
	done        chan struct{}
}

func config(workers, queueSize int) positions.Config {
	return positions.Config{
		Workers:       workers,
		QueueSize:     queueSize,
		ScanBatchSize: batchSize,
		StaleAfter:    staleAfter,
		PushTimeout:   pushTimeout,
	}
}

func staleTrips(n int) []model.StaleTrip {
	trips := make([]model.StaleTrip, 0, n)
	for i := range n {
		trips = append(trips, model.StaleTrip{
			ID:         uuid.New(),
			DriverID:   uuid.New(),
			LastSeenAt: seenAt.Add(time.Duration(i) * time.Second),
		})
	}
	return trips
}

func start(t *testing.T, cfg positions.Config) *harness {
	t.Helper()
	return startWith(t, cfg, noop.NewTracerProvider().Tracer("test"), slog.New(slog.DiscardHandler))
}

func startWith(t *testing.T, cfg positions.Config, tracer trace.Tracer, log *slog.Logger) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	h := &harness{
		cfg:     cfg,
		trips:   NewMockstaleTrips(ctrl),
		push:    NewMockpushGateway(ctrl),
		metrics: NewMockmetrics(ctrl),
		ticks:   make(chan time.Time),
		idle:    make(chan struct{}, 64),
		done:    make(chan struct{}),
	}
	h.metrics.EXPECT().RowsRead(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, rows int) { h.rowsRead = append(h.rowsRead, rows) }).AnyTimes()
	tick := NewMockticker(ctrl)
	tick.EXPECT().C().DoAndReturn(func() <-chan time.Time {
		h.idle <- struct{}{}
		return h.ticks
	}).AnyTimes()
	tick.EXPECT().Stop()
	h.worker = positions.New(h.trips, h.push, h.metrics, tick, tracer, log, cfg)

	ctx, cancel := context.WithCancel(t.Context())
	scanning, stopScanner := context.WithCancel(ctx)
	h.cancel, h.stopScanner = cancel, stopScanner
	go func() {
		defer close(h.done)
		h.worker.Run(ctx, scanning)
	}()
	t.Cleanup(func() {
		cancel()
		wait(t, h.done)
	})
	wait(t, h.idle)
	return h
}

func wait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	var got T
	select {
	case got = <-ch:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for the worker")
	}
	return got
}

func (h *harness) scan(t *testing.T) {
	t.Helper()
	select {
	case h.ticks <- time.Time{}:
	case <-time.After(waitTimeout):
		t.Fatal("scanner did not take the tick")
	}
	wait(t, h.idle)
}

func (h *harness) stop(t *testing.T) {
	t.Helper()
	h.stopScanner()
	wait(t, h.done)
}

func (h *harness) stale(trips ...model.StaleTrip) *gomock.Call {
	return h.batch(nil, trips...)
}

func (h *harness) batch(last *model.StaleTrip, trips ...model.StaleTrip) *gomock.Call {
	return h.trips.EXPECT().ListStale(gomock.Any(), staleAfter, last, h.cfg.ScanBatchSize).Return(trips, nil)
}

func (h *harness) failedBatch(last *model.StaleTrip, err error) *gomock.Call {
	return h.trips.EXPECT().ListStale(gomock.Any(), staleAfter, last, h.cfg.ScanBatchSize).Return(nil, err)
}

func endedSpan(t *testing.T, recorder *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %s is not recorded", name)
	return nil
}

type gate struct {
	started chan uuid.UUID
	release chan struct{}
	once    sync.Once
}

func newGate(t *testing.T) *gate {
	g := &gate{started: make(chan uuid.UUID, 64), release: make(chan struct{})}
	t.Cleanup(g.open)
	return g
}

func (g *gate) open() {
	g.once.Do(func() { close(g.release) })
}

func (g *gate) hold(ctx context.Context, tripID, _ uuid.UUID) error {
	g.started <- tripID
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestTripInQueueIsNotQueuedAgain(t *testing.T) {
	h := start(t, config(0, 10))
	h.stale(tripA, tripB).Times(2)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)
	h.metrics.EXPECT().StaleDuplicate(gomock.Any()).Times(2)

	h.scan(t)
	h.scan(t)

	assert.Equal(t, int64(2), h.worker.QueueLen())
}

func TestCancelledTaskKeepsTripOutOfQueueUntilWorkerTakesIt(t *testing.T) {
	h := start(t, config(0, 10))
	h.stale(tripA).Times(2)
	h.metrics.EXPECT().StaleQueued(gomock.Any())
	h.metrics.EXPECT().StaleDuplicate(gomock.Any())

	h.scan(t)
	h.worker.Cancel(tripA.ID)
	h.scan(t)

	assert.Equal(t, int64(1), h.worker.QueueLen())
}

func TestTripBeingRequestedIsNotQueuedAgain(t *testing.T) {
	h := start(t, config(1, 10))
	g := newGate(t)
	h.stale(tripA).Times(2)
	h.metrics.EXPECT().StaleQueued(gomock.Any())
	h.metrics.EXPECT().StaleDuplicate(gomock.Any())
	h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).DoAndReturn(g.hold)
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any())

	h.scan(t)
	wait(t, g.started)
	h.scan(t)
	g.open()
	h.stop(t)
}

func TestDroppedTripIsQueuedOnLaterTick(t *testing.T) {
	h := start(t, config(1, 1))
	g := newGate(t)
	gomock.InOrder(h.stale(tripA), h.stale(tripB, tripC), h.stale(tripC))
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(3)
	h.metrics.EXPECT().StaleDropped(gomock.Any())
	for _, trip := range []model.StaleTrip{tripA, tripB, tripC} {
		h.push.EXPECT().RequestPosition(gomock.Any(), trip.ID, trip.DriverID).DoAndReturn(g.hold)
	}
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any()).Times(3)

	h.scan(t)
	assert.Equal(t, tripA.ID, wait(t, g.started))
	h.scan(t)
	g.open()
	assert.Equal(t, tripB.ID, wait(t, g.started))
	h.scan(t)
	h.stop(t)
}

func TestFullQueueDropsTripAndEndsScanWithoutBlockingScanner(t *testing.T) {
	var logs bytes.Buffer
	h := startWith(t, config(0, 1), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	h.stale(tripA, tripB, tripC)
	h.metrics.EXPECT().StaleQueued(gomock.Any())
	h.metrics.EXPECT().StaleDropped(gomock.Any())

	h.scan(t)
	h.stop(t)

	assert.Equal(t, int64(1), h.worker.QueueLen())
	assert.Equal(t, []int{3}, h.rowsRead)
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"WARN"`))
	assert.NotContains(t, logs.String(), tripA.ID.String())
	assert.Contains(t, logs.String(), tripB.ID.String())
	assert.NotContains(t, logs.String(), tripC.ID.String())
}

func TestScannerReadsBatchesUntilShortOne(t *testing.T) {
	trips := staleTrips(250)
	h := start(t, config(0, len(trips)))
	gomock.InOrder(
		h.batch(nil, trips[:100]...),
		h.batch(&trips[99], trips[100:200]...),
		h.batch(&trips[199], trips[200:]...),
	)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(len(trips))

	h.scan(t)

	assert.Equal(t, int64(len(trips)), h.worker.QueueLen())
	assert.Equal(t, []int{250}, h.rowsRead)
}

func TestScannerStopsAfterFirstBatchWhenQueueIsFull(t *testing.T) {
	var logs bytes.Buffer
	trips := staleTrips(250)
	h := startWith(t, config(0, 50), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	h.batch(nil, trips[:100]...)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(50)
	h.metrics.EXPECT().StaleDropped(gomock.Any())

	h.scan(t)

	assert.Equal(t, int64(50), h.worker.QueueLen())
	assert.Equal(t, []int{100}, h.rowsRead)
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"WARN"`))
	assert.Contains(t, logs.String(), trips[50].ID.String())
}

func TestEveryTickStartsFromFirstBatch(t *testing.T) {
	cfg := config(0, 10)
	cfg.ScanBatchSize = 2
	h := start(t, cfg)
	gomock.InOrder(
		h.batch(nil, tripA, tripB),
		h.batch(&tripB, tripC),
		h.batch(nil, tripA, tripB),
		h.batch(&tripB, tripC),
	)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(3)
	h.metrics.EXPECT().StaleDuplicate(gomock.Any()).Times(3)

	h.scan(t)
	h.scan(t)

	assert.Equal(t, int64(3), h.worker.QueueLen())
	assert.Equal(t, []int{3, 3}, h.rowsRead)
}

func TestFullLastBatchIsFollowedByEmptyOne(t *testing.T) {
	cfg := config(0, 10)
	cfg.ScanBatchSize = 2
	h := start(t, cfg)
	gomock.InOrder(
		h.batch(nil, tripA, tripB),
		h.batch(&tripB),
	)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)

	h.scan(t)

	assert.Equal(t, int64(2), h.worker.QueueLen())
	assert.Equal(t, []int{2}, h.rowsRead)
}

func TestFailedBatchKeepsEarlierTripsAndCountsRowsRead(t *testing.T) {
	var logs bytes.Buffer
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	cfg := config(0, 10)
	cfg.ScanBatchSize = 2
	h := startWith(t, cfg, provider.Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	gomock.InOrder(
		h.batch(nil, tripA, tripB),
		h.failedBatch(&tripB, errors.New("select stale trips: connection refused")),
	)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)

	h.scan(t)

	assert.Equal(t, int64(2), h.worker.QueueLen())
	assert.Equal(t, []int{2}, h.rowsRead)
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"ERROR"`))
	assert.Equal(t, codes.Error, endedSpan(t, recorder, "positions.scan").Status().Code)
}

func TestStoppedScannerReadsNoMoreBatches(t *testing.T) {
	var logs bytes.Buffer
	cfg := config(0, 10)
	cfg.ScanBatchSize = 2
	h := startWith(t, cfg, noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	h.batch(nil, tripA, tripB).
		Do(func(context.Context, time.Duration, *model.StaleTrip, int) { h.stopScanner() })
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)

	h.scan(t)
	wait(t, h.done)

	assert.Equal(t, []int{2}, h.rowsRead)
	assert.NotContains(t, logs.String(), `"level":"ERROR"`)
	assert.Contains(t, logs.String(), `"msg":"position polling stopped","left_in_queue":2`)
}

func TestStopCancelsScanQueryInFlight(t *testing.T) {
	var logs bytes.Buffer
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	h := startWith(t, config(0, 10), provider.Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	h.batch(nil).DoAndReturn(
		func(ctx context.Context, _ time.Duration, _ *model.StaleTrip, _ int) ([]model.StaleTrip, error) {
			h.stopScanner()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	)

	h.scan(t)
	wait(t, h.done)

	assert.Equal(t, []int{0}, h.rowsRead)
	assert.NotContains(t, logs.String(), `"level":"ERROR"`)
	assert.Equal(t, codes.Unset, endedSpan(t, recorder, "positions.scan").Status().Code)
}

func TestTickThatStartsWithFullQueueStillReadsAndReportsDrop(t *testing.T) {
	var logs bytes.Buffer
	h := startWith(t, config(0, 1), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	gomock.InOrder(h.stale(tripA), h.stale(tripA, tripB))
	h.metrics.EXPECT().StaleQueued(gomock.Any())
	h.metrics.EXPECT().StaleDuplicate(gomock.Any())
	h.metrics.EXPECT().StaleDropped(gomock.Any())

	h.scan(t)
	h.scan(t)

	assert.Equal(t, []int{1, 2}, h.rowsRead)
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"WARN"`))
	assert.Contains(t, logs.String(), tripB.ID.String())
}

func TestPositionBetweenQueueingAndProcessingCancelsRequest(t *testing.T) {
	h := start(t, config(1, 10))
	g := newGate(t)
	h.stale(tripA, tripB)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)
	h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).DoAndReturn(g.hold)
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any())
	h.metrics.EXPECT().RequestCancelled(gomock.Any())

	h.scan(t)
	assert.Equal(t, tripA.ID, wait(t, g.started))
	h.worker.Cancel(tripB.ID)
	g.open()
	h.stop(t)
}

func TestPoolRunsExactlyConfiguredNumberOfRequestsAtOnce(t *testing.T) {
	const workers = 3
	trips := staleTrips(2 * workers)
	h := start(t, config(workers, 10))
	g := newGate(t)
	var (
		mu             sync.Mutex
		inFlight, peak int
	)
	enter := func(delta int) {
		mu.Lock()
		defer mu.Unlock()
		inFlight += delta
		peak = max(peak, inFlight)
	}
	h.stale(trips...)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(len(trips))
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Times(len(trips)).DoAndReturn(
		func(ctx context.Context, tripID, driverID uuid.UUID) error {
			enter(1)
			defer enter(-1)
			return g.hold(ctx, tripID, driverID)
		},
	)
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any()).Times(len(trips))

	h.scan(t)
	for range workers {
		wait(t, g.started)
	}
	assert.Equal(t, int64(workers), h.worker.Busy())
	assert.Equal(t, int64(len(trips)-workers), h.worker.QueueLen())
	g.open()
	h.stop(t)

	assert.Equal(t, workers, peak)
	assert.Zero(t, h.worker.Busy())
	assert.Zero(t, h.worker.QueueLen())
}

func TestFailedRequestDoesNotStopWorker(t *testing.T) {
	var logs bytes.Buffer
	h := startWith(t, config(1, 10), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	unavailable := errors.New("push service is unavailable: status 503")
	h.stale(tripA, tripB)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)
	gomock.InOrder(
		h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).Return(unavailable),
		h.metrics.EXPECT().RequestFailed(gomock.Any(), gomock.Any()),
		h.push.EXPECT().RequestPosition(gomock.Any(), tripB.ID, tripB.DriverID).Return(nil),
		h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any()),
	)

	h.scan(t)
	h.stop(t)

	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"WARN"`))
	assert.Contains(t, logs.String(), tripA.ID.String())
	assert.Contains(t, logs.String(), unavailable.Error())
}

func TestFailedRequestLetsNextTickQueueTripAgain(t *testing.T) {
	h := start(t, config(1, 10))
	failed := make(chan struct{}, 1)
	h.stale(tripA).Times(2)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(2)
	gomock.InOrder(
		h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).Return(context.DeadlineExceeded),
		h.metrics.EXPECT().RequestFailed(gomock.Any(), gomock.Any()).Do(func(context.Context, time.Duration) { failed <- struct{}{} }),
		h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).Return(nil),
		h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any()),
	)

	h.scan(t)
	wait(t, failed)
	h.scan(t)
	h.stop(t)
}

func TestStoppedScannerLetsWorkersDrainQueue(t *testing.T) {
	var logs bytes.Buffer
	h := startWith(t, config(1, 10), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	g := newGate(t)
	h.stale(tripA, tripB, tripC)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(3)
	for _, trip := range []model.StaleTrip{tripA, tripB, tripC} {
		h.push.EXPECT().RequestPosition(gomock.Any(), trip.ID, trip.DriverID).DoAndReturn(g.hold)
	}
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any()).Times(3)

	h.scan(t)
	wait(t, g.started)
	h.stopScanner()
	g.open()
	wait(t, h.done)

	assert.Zero(t, h.worker.QueueLen())
	assert.Contains(t, logs.String(), "position polling started")
	assert.Contains(t, logs.String(), `"msg":"position polling stopped","left_in_queue":0`)
}

func TestCancelledContextStopsScannerAndWorkers(t *testing.T) {
	var logs bytes.Buffer
	h := startWith(t, config(1, 10), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	g := newGate(t)
	h.stale(tripA, tripB, tripC)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Times(3)
	h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).DoAndReturn(g.hold)
	h.metrics.EXPECT().RequestFailed(gomock.Any(), gomock.Any())

	h.scan(t)
	wait(t, g.started)
	h.cancel()
	wait(t, h.done)

	assert.Equal(t, int64(2), h.worker.QueueLen())
	assert.Zero(t, h.worker.Busy())
	assert.Contains(t, logs.String(), `"msg":"position polling stopped","left_in_queue":2`)
}

func TestEveryQueuedTripIsRequestedOrCancelledUnderConcurrentCancels(t *testing.T) {
	const (
		scans      = 20
		cancellers = 4
		cancels    = 2000
	)
	trips := staleTrips(16)
	h := start(t, config(4, 8))
	var queued, duplicate, dropped, requested, cancelled atomic.Int64
	count := func(n *atomic.Int64) func(context.Context) {
		return func(context.Context) { n.Add(1) }
	}
	h.stale(trips...).Times(scans)
	h.metrics.EXPECT().StaleQueued(gomock.Any()).Do(count(&queued)).AnyTimes()
	h.metrics.EXPECT().StaleDuplicate(gomock.Any()).Do(count(&duplicate)).AnyTimes()
	h.metrics.EXPECT().StaleDropped(gomock.Any()).Do(count(&dropped)).AnyTimes()
	h.metrics.EXPECT().RequestCancelled(gomock.Any()).Do(count(&cancelled)).AnyTimes()
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any()).AnyTimes()
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(context.Context, uuid.UUID, uuid.UUID) error {
			requested.Add(1)
			return nil
		},
	)

	var senders sync.WaitGroup
	for range cancellers {
		senders.Go(func() {
			for i := range cancels {
				h.worker.Cancel(trips[i%len(trips)].ID)
			}
		})
	}
	for range scans {
		h.scan(t)
	}
	senders.Wait()
	h.stop(t)

	assert.LessOrEqual(t, dropped.Load(), int64(scans))
	assert.LessOrEqual(t, queued.Load()+duplicate.Load()+dropped.Load(), int64(scans*len(trips)))
	assert.Equal(t, queued.Load(), requested.Load()+cancelled.Load())
	assert.Zero(t, h.worker.QueueLen())
	assert.Zero(t, h.worker.Busy())
}

func TestRequestHasDeadlineOfPushTimeout(t *testing.T) {
	cfg := config(1, 10)
	cfg.PushTimeout = time.Hour
	h := start(t, cfg)
	left := make(chan time.Duration, 1)
	h.stale(tripA)
	h.metrics.EXPECT().StaleQueued(gomock.Any())
	h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).DoAndReturn(
		func(ctx context.Context, _, _ uuid.UUID) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				left <- 0
				return nil
			}
			left <- time.Until(deadline)
			return nil
		},
	)
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any())

	h.scan(t)
	h.stop(t)

	got := wait(t, left)
	assert.Positive(t, got)
	assert.LessOrEqual(t, got, cfg.PushTimeout)
}

func TestScannerSurvivesFailedScan(t *testing.T) {
	var logs bytes.Buffer
	h := startWith(t, config(0, 10), noop.NewTracerProvider().Tracer("test"), slog.New(slog.NewJSONHandler(&logs, nil)))
	gomock.InOrder(
		h.failedBatch(nil, errors.New("select stale trips: connection refused")),
		h.stale(tripA),
	)
	h.metrics.EXPECT().StaleQueued(gomock.Any())

	h.scan(t)
	h.scan(t)
	h.stop(t)

	assert.Equal(t, int64(1), h.worker.QueueLen())
	assert.Equal(t, []int{0, 1}, h.rowsRead)
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"ERROR"`))
	assert.Contains(t, logs.String(), "stale trips scan failed")
}

func TestRequestSpanIsNewTraceLinkedToScanSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	h := startWith(t, config(1, 10), provider.Tracer("test"), slog.New(slog.DiscardHandler))
	var queryCtx, requestCtx trace.SpanContext
	h.stale(tripA).Do(func(ctx context.Context, _ time.Duration, _ *model.StaleTrip, _ int) {
		queryCtx = trace.SpanContextFromContext(ctx)
	})
	h.metrics.EXPECT().StaleQueued(gomock.Any())
	h.push.EXPECT().RequestPosition(gomock.Any(), tripA.ID, tripA.DriverID).DoAndReturn(
		func(ctx context.Context, _, _ uuid.UUID) error {
			requestCtx = trace.SpanContextFromContext(ctx)
			return nil
		},
	)
	h.metrics.EXPECT().RequestSucceeded(gomock.Any(), gomock.Any())

	h.scan(t)
	h.stop(t)

	scan, request := endedSpan(t, recorder, "positions.scan"), endedSpan(t, recorder, "positions.request")
	assert.True(t, queryCtx.Equal(scan.SpanContext()))
	assert.False(t, request.Parent().IsValid())
	assert.NotEqual(t, scan.SpanContext().TraceID(), request.SpanContext().TraceID())
	require.Len(t, request.Links(), 1)
	assert.True(t, request.Links()[0].SpanContext.Equal(scan.SpanContext()))
	assert.True(t, requestCtx.Equal(request.SpanContext()))
}
