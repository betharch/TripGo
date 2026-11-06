package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/betharch/TripGo/internal/config"
	"github.com/betharch/TripGo/internal/container"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "trip-service:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := container.New(ctx, cfg)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return errors.Join(fmt.Errorf("listen %s: %w", cfg.HTTP.Addr, err), shutdown(c, cfg.ShutdownTimeout))
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- c.Server.Serve(ln) }()
	c.Log.Info("trip-service started", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return errors.Join(fmt.Errorf("serve http: %w", err), shutdown(c, cfg.ShutdownTimeout))
	case <-ctx.Done():
		stop()
	}
	c.Log.Info("shutdown started", "timeout", cfg.ShutdownTimeout.String())
	return shutdown(c, cfg.ShutdownTimeout)
}

func shutdown(c *container.Container, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Shutdown(ctx)
}
