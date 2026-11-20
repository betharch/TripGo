package telemetry_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/betharch/TripGo/internal/infra/telemetry"
)

const (
	success   = "success"
	failure   = "error"
	cancelled = "cancelled"
)

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	metrics := make(map[string]metricdata.Metrics, len(rm.ScopeMetrics[0].Metrics))
	for _, m := range rm.ScopeMetrics[0].Metrics {
		metrics[m.Name] = m
	}
	return metrics
}

func result(attrs attribute.Set) string {
	value, _ := attrs.Value("result")
	return value.AsString()
}

func sumByResult(t *testing.T, m metricdata.Metrics) map[string]int64 {
	t.Helper()
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "%s is not a counter", m.Name)
	assert.True(t, sum.IsMonotonic)
	values := make(map[string]int64, len(sum.DataPoints))
	for _, p := range sum.DataPoints {
		values[result(p.Attributes)] = p.Value
	}
	return values
}

func gaugeValue(t *testing.T, m metricdata.Metrics) int64 {
	t.Helper()
	gauge, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "%s is not a gauge", m.Name)
	require.Len(t, gauge.DataPoints, 1)
	return gauge.DataPoints[0].Value
}

func TestPositionMetricsMatchCatalog(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	m, err := telemetry.NewPositionMetrics(t.Context(), meter)
	require.NoError(t, err)
	require.NoError(t, telemetry.ObservePositionQueue(meter,
		func() int64 { return 7 }, func() int64 { return 3 }))

	m.PositionSaved(t.Context())
	m.StaleQueued(t.Context())
	m.StaleDropped(t.Context())
	m.StaleDropped(t.Context())
	m.RowsRead(t.Context(), 250)
	m.RowsRead(t.Context(), 0)
	m.RequestSucceeded(t.Context(), 20*time.Millisecond)
	m.RequestFailed(t.Context(), time.Second)
	m.RequestCancelled(t.Context())

	got := collect(t, reader)
	assert.Len(t, got, 7)
	assert.Equal(t, map[string]int64{"": 1}, sumByResult(t, got["tripgo.positions.saved"]))
	assert.Equal(t, map[string]int64{"queued": 1, "duplicate": 0, "dropped": 2},
		sumByResult(t, got["tripgo.stale_checks"]))

	rowsRead := got["tripgo.stale_checks.rows_read"]
	assert.Equal(t, "{row}", rowsRead.Unit)
	rows, ok := rowsRead.Data.(metricdata.Histogram[int64])
	require.True(t, ok)
	require.Len(t, rows.DataPoints, 1)
	assert.Equal(t, uint64(2), rows.DataPoints[0].Count)
	assert.Equal(t, int64(250), rows.DataPoints[0].Sum)
	assert.Equal(t, []float64{0, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000}, rows.DataPoints[0].Bounds)
	assert.Zero(t, rows.DataPoints[0].Attributes.Len())
	assert.Equal(t, map[string]int64{success: 1, failure: 1, cancelled: 1},
		sumByResult(t, got["tripgo.position_requests"]))
	assert.Equal(t, int64(7), gaugeValue(t, got["tripgo.position_requests.queue.size"]))
	assert.Equal(t, int64(3), gaugeValue(t, got["tripgo.position_requests.workers.active"]))

	duration := got["tripgo.position_requests.duration"]
	assert.Equal(t, "s", duration.Unit)
	histogram, ok := duration.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	counts := make(map[string]uint64, len(histogram.DataPoints))
	for _, p := range histogram.DataPoints {
		counts[result(p.Attributes)] = p.Count
	}
	assert.Equal(t, map[string]uint64{success: 1, failure: 1}, counts)
}

func TestPositionSeriesExistBeforeFirstEvent(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	_, err := telemetry.NewPositionMetrics(t.Context(), meter)
	require.NoError(t, err)

	got := collect(t, reader)

	assert.Equal(t, map[string]int64{"queued": 0, "duplicate": 0, "dropped": 0},
		sumByResult(t, got["tripgo.stale_checks"]))
	assert.Equal(t, map[string]int64{success: 0, failure: 0, cancelled: 0},
		sumByResult(t, got["tripgo.position_requests"]))
}
