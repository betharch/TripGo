package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/betharch/TripGo/internal/config"
	"github.com/betharch/TripGo/internal/http/health"
	"github.com/betharch/TripGo/internal/http/middleware"
	createhttp "github.com/betharch/TripGo/internal/http/trip/create"
	finishhttp "github.com/betharch/TripGo/internal/http/trip/finish"
	gethttp "github.com/betharch/TripGo/internal/http/trip/get"
	"github.com/betharch/TripGo/internal/infra/postgres"
	"github.com/betharch/TripGo/internal/infra/postgres/transaction"
	"github.com/betharch/TripGo/internal/infra/telemetry"
	triprepo "github.com/betharch/TripGo/internal/repository/trip"
	"github.com/betharch/TripGo/internal/usecase/trip/create"
	"github.com/betharch/TripGo/internal/usecase/trip/finish"
	"github.com/betharch/TripGo/internal/usecase/trip/get"
)

const scope = "github.com/betharch/TripGo"

type Container struct {
	Server *http.Server
	Log    *slog.Logger
	pool   *pgxpool.Pool
	tel    *telemetry.Telemetry
}

func New(ctx context.Context, cfg config.Config) (*Container, error) {
	log := telemetry.NewLogger(os.Stdout, cfg.LogLevel, cfg.ServiceName)
	tel, err := telemetry.Setup(ctx, log)
	if err != nil {
		return nil, err
	}
	pool, err := postgres.NewPool(ctx, cfg.DB, tel.TracerProvider, tel.MeterProvider)
	if err != nil {
		return nil, errors.Join(err, shutdownTelemetry(ctx, tel, cfg.ShutdownTimeout))
	}
	c := &Container{Log: log, pool: pool, tel: tel}

	handler, err := c.handler(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, errors.Join(err, shutdownTelemetry(ctx, tel, cfg.ShutdownTimeout))
	}
	c.Server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	return c, nil
}

func (c *Container) handler(ctx context.Context, cfg config.Config) (http.Handler, error) {
	meter := c.tel.MeterProvider.Meter(scope)
	tripMetrics, err := telemetry.NewTripMetrics(ctx, meter)
	if err != nil {
		return nil, err
	}
	activeRequests, err := middleware.Metrics(meter)
	if err != nil {
		return nil, err
	}

	tx := transaction.NewManager(c.pool, cfg.DB.QueryTimeout)
	repo := triprepo.New(c.pool, cfg.DB.QueryTimeout)
	srv := server{
		create: createhttp.New(create.New(repo, tx, tripMetrics, c.Log, time.Now), c.Log),
		get:    gethttp.New(get.New(repo), c.Log),
		finish: finishhttp.New(finish.New(repo, tx, tripMetrics, c.Log, time.Now), c.Log),
		health: health.New(c.pool, cfg.DB.QueryTimeout, c.Log),
	}
	return newRouter(srv,
		middleware.Recover(c.Log),
		middleware.Tracing(c.tel.TracerProvider, c.tel.MeterProvider, c.tel.Propagator),
		middleware.Logging(c.Log),
		activeRequests,
	), nil
}

func (c *Container) Shutdown(ctx context.Context) error {
	if err := c.Server.Shutdown(ctx); err != nil {
		c.Log.Error("graceful shutdown timed out, closing connections forcibly", "error", err)
		_ = c.Server.Close()
	}

	closed := make(chan struct{})
	go func() {
		c.pool.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-ctx.Done():
		c.Log.Error("shutdown budget exceeded, exiting without waiting for database connections")
		return fmt.Errorf("shutdown timed out: %w", ctx.Err())
	}

	if err := c.tel.Shutdown(ctx); err != nil {
		c.Log.Warn("telemetry flush failed", "error", err)
	}
	c.Log.Info("shutdown completed")
	return nil
}

func shutdownTelemetry(ctx context.Context, tel *telemetry.Telemetry, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return tel.Shutdown(ctx)
}
