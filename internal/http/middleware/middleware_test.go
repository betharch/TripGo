package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/middleware"
)

func TestRecoverTurnsPanicInto500(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := middleware.Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/trips", http.NoBody))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "internal_error", p.Code)
	assert.Contains(t, logs.String(), `"msg":"panic recovered"`)
	assert.Contains(t, logs.String(), `"panic":"boom"`)
	assert.Contains(t, logs.String(), `"stack":"goroutine`)
}

func routedHandler(status int, mws ...func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(mws...)
	r.Get("/api/v1/trips/{tripId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})
	return r
}

func TestLoggingWritesOneRecordWithRouteTemplate(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantLevel string
	}{
		{name: "client error is info", status: http.StatusNotFound, wantLevel: "INFO"},
		{name: "server error is error", status: http.StatusInternalServerError, wantLevel: "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := routedHandler(tt.status, middleware.Logging(slog.New(slog.NewJSONHandler(&logs, nil))))

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/trips/0190f7a2-0000-7000-8000-000000000001", http.NoBody))

			var record map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
			assert.Equal(t, tt.wantLevel, record["level"])
			assert.Equal(t, "request handled", record["msg"])
			assert.Equal(t, "GET", record["method"])
			assert.Equal(t, "/api/v1/trips/{tripId}", record["route"])
			assert.InDelta(t, tt.status, record["status"], 0)
			assert.Contains(t, record, "duration_ms")
			assert.NotContains(t, logs.String(), "0190f7a2")
		})
	}
}

func TestMetricsCountsActiveRequestsByRouteTemplate(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mw, err := middleware.Metrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	var during int64
	var route string
	r := chi.NewRouter()
	r.Use(mw)
	r.Get("/api/v1/trips/{tripId}", func(_ http.ResponseWriter, req *http.Request) {
		during, route = activeRequests(t, req.Context(), reader)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/trips/0190f7a2-0000-7000-8000-000000000001", http.NoBody))
	after, _ := activeRequests(t, context.Background(), reader)

	assert.Equal(t, int64(1), during)
	assert.Equal(t, "/api/v1/trips/{tripId}", route)
	assert.Equal(t, int64(0), after)
}

func activeRequests(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) (value int64, route string) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.active_requests" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value("http.route")
				return dp.Value, v.AsString()
			}
		}
	}
	return 0, ""
}
