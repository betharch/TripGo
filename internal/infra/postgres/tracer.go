package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const scope = "github.com/betharch/TripGo/internal/infra/postgres"

var tableName = regexp.MustCompile(`(?i)\b(?:from|into|update)\s+"?([a-z_][a-z0-9_]*)`)

type queryTracer struct {
	*otelpgx.Tracer
	duration metric.Float64Histogram
}

type queryStartKey struct{}

type queryStart struct {
	at         time.Time
	operation  string
	collection string
}

func newQueryTracer(tp trace.TracerProvider, mp metric.MeterProvider) (*queryTracer, error) {
	duration, err := mp.Meter(scope).Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of database client operations."),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("create db.client.operation.duration: %w", err)
	}
	return &queryTracer{
		Tracer: otelpgx.NewTracer(
			otelpgx.WithTracerProvider(tp),
			otelpgx.WithMeterProvider(noop.NewMeterProvider()),
		),
		duration: duration,
	}, nil
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)
	start := queryStart{
		at:         time.Now(),
		operation:  otelpgx.SQLOperationName(ctx, data.SQL),
		collection: collectionName(data.SQL),
	}
	if start.collection != "" {
		trace.SpanFromContext(ctx).SetAttributes(semconv.DBCollectionName(start.collection))
	}
	return context.WithValue(ctx, queryStartKey{}, start)
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if start, ok := ctx.Value(queryStartKey{}).(queryStart); ok {
		attrs := make([]attribute.KeyValue, 0, 3)
		if start.operation != "" {
			attrs = append(attrs, semconv.DBOperationName(start.operation))
		}
		if start.collection != "" {
			attrs = append(attrs, semconv.DBCollectionName(start.collection))
		}
		if errType := errorType(data.Err); errType != "" {
			attrs = append(attrs, semconv.ErrorTypeKey.String(errType))
			trace.SpanFromContext(ctx).SetAttributes(semconv.ErrorTypeKey.String(errType))
		}
		t.duration.Record(ctx, time.Since(start.at).Seconds(), metric.WithAttributes(attrs...))
	}
	t.Tracer.TraceQueryEnd(ctx, conn, data)
}

func (t *queryTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	ctx = t.Tracer.TracePrepareStart(ctx, conn, data)
	if collection := collectionName(data.SQL); collection != "" {
		trace.SpanFromContext(ctx).SetAttributes(semconv.DBCollectionName(collection))
	}
	return ctx
}

func (t *queryTracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	if errType := errorType(data.Err); errType != "" {
		trace.SpanFromContext(ctx).SetAttributes(semconv.ErrorTypeKey.String(errType))
	}
	t.Tracer.TracePrepareEnd(ctx, conn, data)
}

func collectionName(sql string) string {
	if m := tableName.FindStringSubmatch(sql); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

func errorType(err error) string {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return "_OTHER"
}
