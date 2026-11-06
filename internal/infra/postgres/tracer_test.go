package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const trips = "trips"

func TestCollectionName(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{sql: "INSERT INTO trips (id,user_id) VALUES ($1,$2)", want: trips},
		{sql: "SELECT id, status FROM trips WHERE id = $1 FOR UPDATE", want: trips},
		{sql: "UPDATE trips SET status = $1 WHERE id = $2", want: trips},
		{sql: "insert into trip_status_history (trip_id) values ($1)", want: "trip_status_history"},
		{sql: "begin isolation level read committed", want: ""},
		{sql: "commit", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			assert.Equal(t, tt.want, collectionName(tt.sql))
		})
	}
}

func TestErrorType(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "no error", err: nil, want: ""},
		{name: "no rows is not an error", err: fmt.Errorf("select: %w", pgx.ErrNoRows), want: ""},
		{name: "postgres error gives SQLSTATE", err: fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"}), want: "23505"},
		{name: "other error", err: errors.New("conn closed"), want: "_OTHER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errorType(tt.err))
		})
	}
}

func TestPrepareSpanGetsCollectionAndErrorType(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer, err := newQueryTracer(tp, noop.NewMeterProvider())
	require.NoError(t, err)

	ctx, query := tp.Tracer("test").Start(context.Background(), "INSERT")
	ctx = tracer.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{SQL: "INSERT INTO trips (id) VALUES ($1)"})
	tracer.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{Err: &pgconn.PgError{Code: "42P01"}})
	query.End()

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	attrs := attribute.NewSet(spans[0].Attributes()...)
	collection, _ := attrs.Value(semconv.DBCollectionNameKey)
	assert.Equal(t, trips, collection.AsString())
	errType, _ := attrs.Value(semconv.ErrorTypeKey)
	assert.Equal(t, "42P01", errType.AsString())
}
