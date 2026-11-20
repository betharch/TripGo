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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/betharch/TripGo/internal/config"
	"github.com/betharch/TripGo/internal/gateway/push"
	"github.com/betharch/TripGo/internal/http/health"
	"github.com/betharch/TripGo/internal/http/middleware"
	addpositionhttp "github.com/betharch/TripGo/internal/http/trip/addposition"
	createhttp "github.com/betharch/TripGo/internal/http/trip/create"
	finishhttp "github.com/betharch/TripGo/internal/http/trip/finish"
	gethttp "github.com/betharch/TripGo/internal/http/trip/get"
	listpositionshttp "github.com/betharch/TripGo/internal/http/trip/listpositions"
	"github.com/betharch/TripGo/internal/infra/postgres"
	"github.com/betharch/TripGo/internal/infra/postgres/transaction"
	"github.com/betharch/TripGo/internal/infra/telemetry"
	triprepo "github.com/betharch/TripGo/internal/repository/trip"
	"github.com/betharch/TripGo/internal/usecase/trip/addposition"
	"github.com/betharch/TripGo/internal/usecase/trip/create"
	"github.com/betharch/TripGo/internal/usecase/trip/finish"
	"github.com/betharch/TripGo/internal/usecase/trip/get"
	"github.com/betharch/TripGo/internal/usecase/trip/listpositions"
	"github.com/betharch/TripGo/internal/worker/positions"
)

const scope = "github.com/betharch/TripGo"

type Container struct {
	Server *http.Server
	Log    *slog.Logger
	pool   *pgxpool.Pool
	tel    *telemetry.Telemetry

	worker        *positions.Worker
	pushTransport *http.Transport
	stopScanner   context.CancelFunc
	cancelWorkers context.CancelFunc
	workerDone    chan struct{}
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

	handler, err := c.wire(ctx, cfg)
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

func (c *Container) wire(ctx context.Context, cfg config.Config) (http.Handler, error) {
	meter := c.tel.MeterProvider.Meter(scope)
	tripMetrics, err := telemetry.NewTripMetrics(ctx, meter)
	if err != nil {
		return nil, err
	}
	positionMetrics, err := telemetry.NewPositionMetrics(ctx, meter)
	if err != nil {
		return nil, err
	}
	activeRequests, err := middleware.Metrics(meter)
	if err != nil {
		return nil, err
	}

	tx := transaction.NewManager(c.pool, cfg.DB.QueryTimeout)
	repo := triprepo.New(c.pool, cfg.DB.QueryTimeout)
	gateway, err := c.pushGateway(cfg)
	if err != nil {
		return nil, err
	}
	c.worker = positions.New(repo, gateway, positionMetrics,
		positions.NewTicker(cfg.Positions.CheckInterval),
		c.tel.TracerProvider.Tracer(scope), c.Log,
		positions.Config{
			Workers:       cfg.Positions.Workers,
			QueueSize:     cfg.Positions.QueueSize,
			ScanBatchSize: cfg.Positions.ScanBatchSize,
			StaleAfter:    cfg.Positions.StaleAfter,
			PushTimeout:   cfg.Push.RequestTimeout,
		})
	if err := telemetry.ObservePositionQueue(meter, c.worker.QueueLen, c.worker.Busy); err != nil {
		return nil, err
	}

	srv := server{
		create:        createhttp.New(create.New(repo, tx, tripMetrics, c.Log, time.Now), c.Log),
		get:           gethttp.New(get.New(repo), c.Log),
		finish:        finishhttp.New(finish.New(repo, tx, tripMetrics, c.Log, time.Now), c.Log),
		addPosition:   addpositionhttp.New(addposition.New(repo, tx, c.worker, positionMetrics, time.Now), c.Log),
		listPositions: listpositionshttp.New(listpositions.New(repo), c.Log),
		health:        health.New(c.pool, cfg.DB.QueryTimeout, c.Log),
	}
	return newRouter(srv,
		middleware.Recover(c.Log),
		middleware.Tracing(c.tel.TracerProvider, c.tel.MeterProvider, c.tel.Propagator),
		middleware.Logging(c.Log),
		activeRequests,
	), nil
}

func (c *Container) pushGateway(cfg config.Config) (*push.Gateway, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is not *http.Transport")
	}
	c.pushTransport = base.Clone()
	c.pushTransport.MaxIdleConnsPerHost = cfg.Positions.Workers
	client := &http.Client{
		Timeout: cfg.Push.RequestTimeout,
		Transport: otelhttp.NewTransport(c.pushTransport,
			otelhttp.WithTracerProvider(c.tel.TracerProvider),
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
			otelhttp.WithPropagators(c.tel.Propagator),
		),
	}
	return push.New(cfg.Push.URL, client)
}

func (c *Container) Start(ctx context.Context) {
	ctx, c.cancelWorkers = context.WithCancel(context.WithoutCancel(ctx))
	scanning, stopScanner := context.WithCancel(ctx)
	c.stopScanner = stopScanner
	c.workerDone = make(chan struct{})
	go func() {
		defer close(c.workerDone)
		c.worker.Run(ctx, scanning)
	}()
}

func (c *Container) Shutdown(ctx context.Context) error {
	if err := c.Server.Shutdown(ctx); err != nil {
		c.Log.Error("graceful shutdown timed out, closing connections forcibly", "error", err)
		_ = c.Server.Close()
	}

	c.stopScannerAndDrainWorkers(ctx)
	c.pushTransport.CloseIdleConnections()

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

func (c *Container) stopScannerAndDrainWorkers(ctx context.Context) {
	if c.workerDone == nil {
		return
	}
	c.stopScanner()
	select {
	case <-c.workerDone:
	case <-ctx.Done():
		c.Log.Error("shutdown budget exceeded, cancelling position requests in flight")
		c.cancelWorkers()
		<-c.workerDone
	}
	c.cancelWorkers()
}

func shutdownTelemetry(ctx context.Context, tel *telemetry.Telemetry, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return tel.Shutdown(ctx)
}
