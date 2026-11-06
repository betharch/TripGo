package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

func Tracing(tp trace.TracerProvider, mp metric.MeterProvider, prop propagation.TextMapPropagator) func(http.Handler) http.Handler {
	instrument := otelhttp.NewMiddleware("trip-service",
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithMeterProvider(mp),
		otelhttp.WithPropagators(prop),
	)
	return func(next http.Handler) http.Handler {
		return instrument(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if route := routePattern(r); route != "" {
				trace.SpanFromContext(r.Context()).SetAttributes(semconv.HTTPRoute(route))
			}
		}))
	}
}
