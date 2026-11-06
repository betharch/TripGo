package container

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	api "github.com/betharch/TripGo/internal/generated"
	"github.com/betharch/TripGo/internal/http/domainerror"
	"github.com/betharch/TripGo/internal/http/health"
	createhttp "github.com/betharch/TripGo/internal/http/trip/create"
	finishhttp "github.com/betharch/TripGo/internal/http/trip/finish"
	gethttp "github.com/betharch/TripGo/internal/http/trip/get"
)

type server struct {
	create *createhttp.Handler
	get    *gethttp.Handler
	finish *finishhttp.Handler
	health *health.Handler
}

var _ api.ServerInterface = server{}

func (s server) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	s.create.Handle(w, r)
}

func (s server) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.get.Handle(w, r, tripID)
}

func (s server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.finish.Handle(w, r, tripID)
}

func (s server) Health(w http.ResponseWriter, r *http.Request) {
	s.health.Health(w, r)
}

func (s server) Ready(w http.ResponseWriter, r *http.Request) {
	s.health.Ready(w, r)
}

func newRouter(s server, middlewares ...func(http.Handler) http.Handler) http.Handler {
	mux := chi.NewRouter()
	mux.Use(middlewares...)
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		domainerror.Write(w, r, domainerror.ErrRouteNotFound)
	})
	mux.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", strings.Join(allowedMethods(mux, r.URL.Path), ", "))
		domainerror.Write(w, r, domainerror.ErrMethodNotAllowed)
	})
	return api.HandlerWithOptions(s, api.ChiServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: domainerror.ParamError,
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
