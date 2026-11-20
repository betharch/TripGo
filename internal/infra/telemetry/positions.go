package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const resultKey = "result"

var (
	resultQueued    = metric.WithAttributes(attribute.String(resultKey, "queued"))
	resultDuplicate = metric.WithAttributes(attribute.String(resultKey, "duplicate"))
	resultDropped   = metric.WithAttributes(attribute.String(resultKey, "dropped"))
	resultSuccess   = metric.WithAttributes(attribute.String(resultKey, "success"))
	resultError     = metric.WithAttributes(attribute.String(resultKey, "error"))
	resultCancelled = metric.WithAttributes(attribute.String(resultKey, "cancelled"))
)

type PositionMetrics struct {
	saved       metric.Int64Counter
	staleChecks metric.Int64Counter
	rowsRead    metric.Int64Histogram
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
}

func NewPositionMetrics(ctx context.Context, meter metric.Meter) (*PositionMetrics, error) {
	saved, err := meter.Int64Counter("tripgo.positions.saved",
		metric.WithUnit("{position}"), metric.WithDescription("Number of saved trip positions."))
	if err != nil {
		return nil, fmt.Errorf("create tripgo.positions.saved: %w", err)
	}
	staleChecks, err := meter.Int64Counter("tripgo.stale_checks",
		metric.WithUnit("{check}"), metric.WithDescription("What the scanner did with a stale trip."))
	if err != nil {
		return nil, fmt.Errorf("create tripgo.stale_checks: %w", err)
	}
	rowsRead, err := meter.Int64Histogram("tripgo.stale_checks.rows_read",
		metric.WithUnit("{row}"), metric.WithDescription("Number of stale trips the scanner read in one tick."),
		metric.WithExplicitBucketBoundaries(0, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000))
	if err != nil {
		return nil, fmt.Errorf("create tripgo.stale_checks.rows_read: %w", err)
	}
	requests, err := meter.Int64Counter("tripgo.position_requests",
		metric.WithUnit("{request}"), metric.WithDescription("Outcome of position requests."))
	if err != nil {
		return nil, fmt.Errorf("create tripgo.position_requests: %w", err)
	}
	duration, err := meter.Float64Histogram("tripgo.position_requests.duration",
		metric.WithUnit("s"), metric.WithDescription("Duration of Push Service calls."),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10))
	if err != nil {
		return nil, fmt.Errorf("create tripgo.position_requests.duration: %w", err)
	}

	saved.Add(ctx, 0)
	for _, result := range []metric.MeasurementOption{resultQueued, resultDuplicate, resultDropped} {
		staleChecks.Add(ctx, 0, result)
	}
	for _, result := range []metric.MeasurementOption{resultSuccess, resultError, resultCancelled} {
		requests.Add(ctx, 0, result)
	}
	return &PositionMetrics{
		saved: saved, staleChecks: staleChecks, rowsRead: rowsRead, requests: requests, duration: duration,
	}, nil
}

func ObservePositionQueue(meter metric.Meter, queueSize, activeWorkers func() int64) error {
	_, err := meter.Int64ObservableGauge("tripgo.position_requests.queue.size",
		metric.WithUnit("{task}"), metric.WithDescription("Number of tasks in the queue."),
		metric.WithInt64Callback(observe(queueSize)))
	if err != nil {
		return fmt.Errorf("create tripgo.position_requests.queue.size: %w", err)
	}
	_, err = meter.Int64ObservableGauge("tripgo.position_requests.workers.active",
		metric.WithUnit("{worker}"), metric.WithDescription("Number of busy workers."),
		metric.WithInt64Callback(observe(activeWorkers)))
	if err != nil {
		return fmt.Errorf("create tripgo.position_requests.workers.active: %w", err)
	}
	return nil
}

func observe(value func() int64) metric.Int64Callback {
	return func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(value())
		return nil
	}
}

func (m *PositionMetrics) PositionSaved(ctx context.Context) {
	m.saved.Add(ctx, 1)
}

func (m *PositionMetrics) StaleQueued(ctx context.Context) {
	m.staleChecks.Add(ctx, 1, resultQueued)
}

func (m *PositionMetrics) StaleDuplicate(ctx context.Context) {
	m.staleChecks.Add(ctx, 1, resultDuplicate)
}

func (m *PositionMetrics) StaleDropped(ctx context.Context) {
	m.staleChecks.Add(ctx, 1, resultDropped)
}

func (m *PositionMetrics) RowsRead(ctx context.Context, rows int) {
	m.rowsRead.Record(ctx, int64(rows))
}

func (m *PositionMetrics) RequestSucceeded(ctx context.Context, took time.Duration) {
	m.requests.Add(ctx, 1, resultSuccess)
	m.duration.Record(ctx, took.Seconds(), resultSuccess)
}

func (m *PositionMetrics) RequestFailed(ctx context.Context, took time.Duration) {
	m.requests.Add(ctx, 1, resultError)
	m.duration.Record(ctx, took.Seconds(), resultError)
}

func (m *PositionMetrics) RequestCancelled(ctx context.Context) {
	m.requests.Add(ctx, 1, resultCancelled)
}
