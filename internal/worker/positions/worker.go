package positions

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	Workers       int
	QueueSize     int
	ScanBatchSize int
	StaleAfter    time.Duration
	PushTimeout   time.Duration
}

type Worker struct {
	trips   staleTrips
	push    pushGateway
	metrics metrics
	tick    ticker
	tracer  trace.Tracer
	log     *slog.Logger
	cfg     Config
	queue   *queue
	busy    atomic.Int64
}

func New(trips staleTrips, push pushGateway, m metrics, tick ticker, tracer trace.Tracer, log *slog.Logger, cfg Config) *Worker {
	return &Worker{
		trips:   trips,
		push:    push,
		metrics: m,
		tick:    tick,
		tracer:  tracer,
		log:     log,
		cfg:     cfg,
		queue:   newQueue(cfg.QueueSize),
	}
}

func (w *Worker) Run(ctx, scanning context.Context) {
	w.log.InfoContext(ctx, "position polling started", "workers", w.cfg.Workers, "queue_size", w.cfg.QueueSize)
	var workers sync.WaitGroup
	for range w.cfg.Workers {
		workers.Go(func() { w.work(ctx) })
	}
	w.scanLoop(scanning)
	close(w.queue.tasks)
	workers.Wait()
	w.log.InfoContext(ctx, "position polling stopped", "left_in_queue", len(w.queue.tasks))
}

func (w *Worker) Cancel(tripID uuid.UUID) {
	w.queue.cancel(tripID)
}

func (w *Worker) QueueLen() int64 {
	return int64(len(w.queue.tasks))
}

func (w *Worker) Busy() int64 {
	return w.busy.Load()
}
