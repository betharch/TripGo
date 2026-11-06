package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/betharch/TripGo/internal/http/domainerror"
)

var errPanic = errors.New("panic in handler")

func Recover(log *slog.Logger) func(http.Handler) http.Handler {
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
				log.ErrorContext(r.Context(), "panic recovered",
					"panic", fmt.Sprint(p), "method", r.Method, "path", r.URL.Path,
					"stack", string(debug.Stack()))
				domainerror.Write(w, r, errPanic)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
