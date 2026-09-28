package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/betharch/TripGo/internal/config"
	"github.com/betharch/TripGo/internal/httpapi"
	"github.com/betharch/TripGo/internal/postgres"
	"github.com/betharch/TripGo/internal/trip"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "trip-service:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	trips := trip.NewService(
		postgres.NewTxManager(pool, cfg.DB.QueryTimeout),
		postgres.NewTripRepository(pool, cfg.DB.QueryTimeout),
	)
	handler := httpapi.NewHandler(trips, pool.Ping, cfg.DB.QueryTimeout, log)

	srv := &http.Server{
		Handler:           httpapi.NewRouter(handler, log),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	ln, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		pool.Close()
		return fmt.Errorf("listen %s: %w", cfg.HTTP.Addr, err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("trip-service started", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		pool.Close()
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
		stop()
	}
	log.Info("shutdown started", "timeout", cfg.ShutdownTimeout.String())
	return shutdown(srv, pool, cfg.ShutdownTimeout, log)
}

func shutdown(srv *http.Server, pool *pgxpool.Pool, timeout time.Duration, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown timed out, closing connections forcibly", "err", err)
		_ = srv.Close()
	}

	closed := make(chan struct{})
	go func() {
		pool.Close()
		close(closed)
	}()
	select {
	case <-closed:
		log.Info("shutdown completed")
		return nil
	case <-ctx.Done():
		log.Error("shutdown budget exceeded, exiting without waiting for database connections")
		return fmt.Errorf("shutdown timed out: %w", ctx.Err())
	}
}
