package positions

import (
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/betharch/TripGo/internal/model"
)

type state int

const (
	waiting state = iota + 1
	cancelled
)

type outcome int

const (
	queued outcome = iota
	duplicate
	dropped
)

type task struct {
	trip model.StaleTrip
	scan trace.SpanContext
}

type queue struct {
	mu      sync.Mutex
	inQueue map[uuid.UUID]state
	tasks   chan task
}

func newQueue(size int) *queue {
	return &queue{inQueue: make(map[uuid.UUID]state), tasks: make(chan task, size)}
}

func (q *queue) offer(t task) outcome {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.inQueue[t.trip.ID]; ok {
		return duplicate
	}
	select {
	case q.tasks <- t:
		q.inQueue[t.trip.ID] = waiting
		return queued
	default:
		return dropped
	}
}

func (q *queue) cancel(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.inQueue[tripID]; ok {
		q.inQueue[tripID] = cancelled
	}
}

func (q *queue) start(tripID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.inQueue[tripID] == waiting {
		return true
	}
	delete(q.inQueue, tripID)
	return false
}

func (q *queue) finish(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inQueue, tripID)
}
