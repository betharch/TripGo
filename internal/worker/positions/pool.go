package positions

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/betharch/TripGo/internal/model"
)

const requestSpan = "positions.request"

func (w *Worker) work(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-w.queue.tasks:
			if !ok {
				return
			}
			w.handle(ctx, t)
		}
	}
}

func (w *Worker) handle(ctx context.Context, t task) {
	if !w.queue.start(t.trip.ID) {
		w.metrics.RequestCancelled(ctx)
		return
	}
	ctx, span := w.tracer.Start(ctx, requestSpan,
		trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: t.scan}))
	defer span.End()

	took, err := w.request(ctx, t.trip)
	if err != nil {
		span.SetStatus(codes.Error, "position request failed")
		w.metrics.RequestFailed(ctx, took)
		w.log.WarnContext(ctx, "position request failed", "trip_id", t.trip.ID.String(), "error", err)
		return
	}
	w.metrics.RequestSucceeded(ctx, took)
}

func (w *Worker) request(ctx context.Context, trip model.StaleTrip) (time.Duration, error) {
	defer w.queue.finish(trip.ID)
	w.busy.Add(1)
	defer w.busy.Add(-1)

	ctx, cancel := context.WithTimeout(ctx, w.cfg.PushTimeout)
	defer cancel()
	started := time.Now()
	err := w.push.RequestPosition(ctx, trip.ID, trip.DriverID)
	return time.Since(started), err
}
