package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTP            HTTP
	DB              DB
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

type HTTP struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type DB struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

func Load() (Config, error) {
	var p parser
	cfg := Config{
		HTTP: HTTP{
			Addr:              p.required("HTTP_ADDR"),
			ReadHeaderTimeout: p.duration("HTTP_READ_HEADER_TIMEOUT"),
			ReadTimeout:       p.duration("HTTP_READ_TIMEOUT"),
			WriteTimeout:      p.duration("HTTP_WRITE_TIMEOUT"),
			IdleTimeout:       p.duration("HTTP_IDLE_TIMEOUT"),
		},
		DB: DB{
			URL:             p.required("DATABASE_URL"),
			MaxConns:        p.intAtLeast("DATABASE_MAX_CONNS", 1),
			MinConns:        p.intAtLeast("DATABASE_MIN_CONNS", 0),
			MaxConnLifetime: p.duration("DATABASE_MAX_CONN_LIFETIME"),
			ConnectTimeout:  p.duration("DATABASE_CONNECT_TIMEOUT"),
			QueryTimeout:    p.duration("DATABASE_QUERY_TIMEOUT"),
		},
		LogLevel:        p.level("LOG_LEVEL"),
		ShutdownTimeout: p.duration("SHUTDOWN_TIMEOUT"),
	}
	if cfg.DB.MaxConns > 0 && cfg.DB.MinConns > cfg.DB.MaxConns {
		p.fail("DATABASE_MIN_CONNS", fmt.Errorf("must not exceed DATABASE_MAX_CONNS (%d)", cfg.DB.MaxConns))
	}

	if err := errors.Join(p.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

type parser struct {
	errs []error
}

func (p *parser) fail(name string, err error) {
	p.errs = append(p.errs, fmt.Errorf("%s: %w", name, err))
}

func (p *parser) required(name string) string {
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		p.fail(name, errors.New("is required"))
		return ""
	}
	return value
}

func (p *parser) duration(name string) time.Duration {
	raw := p.required(name)
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		p.fail(name, err)
		return 0
	}
	if d <= 0 {
		p.fail(name, fmt.Errorf("must be positive, got %s", d))
		return 0
	}
	return d
}

func (p *parser) intAtLeast(name string, minValue int32) int32 {
	raw := p.required(name)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		p.fail(name, err)
		return 0
	}
	if int32(n) < minValue {
		p.fail(name, fmt.Errorf("must be at least %d, got %d", minValue, n))
		return 0
	}
	return int32(n)
}

func (p *parser) level(name string) slog.Level {
	raw := p.required(name)
	if raw == "" {
		return 0
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		p.fail(name, fmt.Errorf("%w (want debug, info, warn or error)", err))
		return 0
	}
	return lvl
}
