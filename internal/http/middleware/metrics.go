package middleware

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
	http.MethodConnect: true, http.MethodTrace: true,
}

func Metrics(meter metric.Meter) (func(http.Handler) http.Handler, error) {
	active, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of active HTTP server requests."),
	)
	if err != nil {
		return nil, fmt.Errorf("create http.server.active_requests: %w", err)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attrs := metric.WithAttributes(requestAttributes(r)...)
			active.Add(r.Context(), 1, attrs)
			defer active.Add(r.Context(), -1, attrs)
			next.ServeHTTP(w, r)
		})
	}, nil
}

func requestAttributes(r *http.Request) []attribute.KeyValue {
	method := r.Method
	if !knownMethods[method] {
		method = "_OTHER"
	}
	attrs := []attribute.KeyValue{semconv.HTTPRequestMethodKey.String(method)}
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.Routes != nil {
		if route := rctx.Routes.Find(chi.NewRouteContext(), r.Method, path); route != "" {
			attrs = append(attrs, semconv.HTTPRoute(route))
		}
	}
	return attrs
}
