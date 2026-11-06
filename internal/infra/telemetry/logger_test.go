package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/betharch/TripGo/internal/infra/telemetry"
)

func lastRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var record map[string]any
	require.NoError(t, json.Unmarshal(lines[len(lines)-1], &record))
	return record
}

func TestLoggerAddsTraceIDsInsideSpan(t *testing.T) {
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo, "trip-service")
	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()

	log.InfoContext(ctx, "trip created", "trip_id", "42")

	record := lastRecord(t, &buf)
	assert.Equal(t, "trip-service", record["service"])
	assert.Equal(t, "trip created", record["msg"])
	assert.Equal(t, span.SpanContext().TraceID().String(), record["trace_id"])
	assert.Equal(t, span.SpanContext().SpanID().String(), record["span_id"])
	assert.Regexp(t, `Z$`, record["time"])
}

func TestLoggerOmitsTraceIDsOutsideSpan(t *testing.T) {
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo, "trip-service")

	log.InfoContext(context.Background(), "trip-service started")

	record := lastRecord(t, &buf)
	assert.NotContains(t, record, "trace_id")
	assert.NotContains(t, record, "span_id")
}

func TestLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelWarn, "trip-service")

	log.Info("hidden")

	assert.Empty(t, buf.String())
}
