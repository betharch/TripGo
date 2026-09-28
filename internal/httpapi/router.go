package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/go-chi/chi/v5"

	api "github.com/betharch/TripGo/internal/generated"
)

func NewRouter(h *Handler, log *slog.Logger) http.Handler {
	mux := chi.NewRouter()
	mux.Use(recoverer(log))
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, problemRouteNotFound, "Route does not exist")
	})
	mux.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", strings.Join(allowedMethods(mux, r.URL.Path), ", "))
		writeProblem(w, r, problemMethodNotAllowed, "Method is not allowed for this route")
	})

	return api.HandlerWithOptions(h, api.ChiServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: paramError,
	})
}

func allowedMethods(routes chi.Routes, path string) []string {
	var allowed []string
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		if routes.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}
	return allowed
}

func paramError(w http.ResponseWriter, r *http.Request, err error) {
	detail := "Invalid request parameters"
	var formatErr *api.InvalidParamFormatError
	if errors.As(err, &formatErr) {
		detail = fmt.Sprintf("Parameter %s must be a UUID", formatErr.ParamName)
	}
	writeProblem(w, r, problemInvalidRequest, detail)
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if p == http.ErrAbortHandler {
					panic(p)
				}
				log.ErrorContext(r.Context(), "panic in handler",
					"panic", p, "method", r.Method, "path", r.URL.Path, "stack", string(debug.Stack()))
				writeProblem(w, r, problemInternal, "Internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
