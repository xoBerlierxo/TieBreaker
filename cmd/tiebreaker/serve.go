package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/config"
	httpapi "github.com/xoBerlierxo/TieBreaker/internal/http"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// Server timeouts (SPEC §14). They protect against slow or malicious clients
// holding connections open. SSE routes (M4) extend the write deadline per
// request with http.ResponseController.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
)

// serve runs the HTTP API until ctx is cancelled.
func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	db, err := store.OpenPostgres(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.ClosePostgres(db); err != nil {
			logger.Warn("closing postgres", slog.Any("error", err))
		}
	}()

	rdb, err := store.OpenRedis(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := rdb.Close(); err != nil {
			logger.Warn("closing redis", slog.Any("error", err))
		}
	}()

	router := httpapi.NewRouter(httpapi.Deps{
		Logger:       logger,
		Postgres:     store.PostgresPinger{DB: db},
		Redis:        store.RedisPinger{Client: rdb},
		MaxBodyBytes: cfg.HTTP.MaxBodyBytes,
	})

	return runHTTPServer(ctx, cfg.HTTPAddr, router, cfg.HTTP.ShutdownGrace, logger, nil)
}

// runHTTPServer serves h on addr until ctx is cancelled, then shuts down
// gracefully: it stops accepting new connections and waits up to grace for
// in-flight requests to finish (SPEC §14).
//
// onListen, if not nil, is called with the real listening address. Tests use
// it with addr "127.0.0.1:0" to get a free port.
func runHTTPServer(ctx context.Context, addr string, h http.Handler, grace time.Duration,
	logger *slog.Logger, onListen func(net.Addr),
) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Serve blocks, so it runs in its own goroutine. The buffered channel lets
	// that goroutine finish even if nobody is receiving any more.
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	logger.Info("http server listening", slog.String("addr", ln.Addr().String()))
	if onListen != nil {
		onListen(ln.Addr())
	}

	select {
	case err := <-serveErr:
		// Serve stopped on its own, which only happens on a real error.
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down http server", slog.String("grace", grace.String()))
	// ctx is already cancelled, so the shutdown deadline needs a fresh context.
	// WithoutCancel keeps ctx's values but drops its cancellation.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	// After Shutdown, Serve returns http.ErrServerClosed. Wait for it, so the
	// goroutine has definitely exited before we return.
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	logger.Info("http server stopped")
	return nil
}
