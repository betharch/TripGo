package positions

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/betharch/TripGo/internal/model"
)

const scanSpan = "positions.scan"

func (w *Worker) scanLoop(ctx context.Context) {
	defer w.tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.tick.C():
			w.scan(ctx)
		}
	}
}

func (w *Worker) scan(ctx context.Context) {
	ctx, span := w.tracer.Start(ctx, scanSpan)
	defer span.End()

	read, err := w.enqueueStale(ctx, span.SpanContext())
	w.metrics.RowsRead(ctx, read)
	if err != nil && ctx.Err() == nil {
		span.SetStatus(codes.Error, "stale trips scan failed")
		w.log.ErrorContext(ctx, "stale trips scan failed", "error", err)
	}
}

func (w *Worker) enqueueStale(ctx context.Context, scan trace.SpanContext) (int, error) {
	var (
		last *model.StaleTrip
		read int
	)
	for ctx.Err() == nil {
		batch, err := w.trips.ListStale(ctx, w.cfg.StaleAfter, last, w.cfg.ScanBatchSize)
		if err != nil {
			return read, err
		}
		read += len(batch)
		for _, trip := range batch {
			if w.enqueue(ctx, task{trip: trip, scan: scan}) == dropped {
				return read, nil
			}
		}
		if len(batch) < w.cfg.ScanBatchSize {
			return read, nil
		}
		last = &batch[len(batch)-1]
	}
	return read, nil
}

func (w *Worker) enqueue(ctx context.Context, t task) outcome {
	result := w.queue.offer(t)
	switch result {
	case queued:
		w.metrics.StaleQueued(ctx)
	case duplicate:
		w.metrics.StaleDuplicate(ctx)
	case dropped:
		w.metrics.StaleDropped(ctx)
		w.log.WarnContext(ctx, "position request dropped: queue is full", "trip_id", t.trip.ID.String())
	}
	return result
}
