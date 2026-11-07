package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func emptyEnv(string) string { return "" }

func TestRun_UnknownSubcommandReturnsUsageError(t *testing.T) {
	err := run(context.Background(), []string{"fly"}, emptyEnv, discardLogger())
	if !errors.Is(err, errUsage) {
		t.Fatalf("run(fly) error = %v, want errUsage", err)
	}
}

func TestRun_NoSubcommandReturnsUsageError(t *testing.T) {
	err := run(context.Background(), nil, emptyEnv, discardLogger())
	if !errors.Is(err, errUsage) {
		t.Fatalf("run() error = %v, want errUsage", err)
	}
}

func TestRun_ServeWithMissingConfigReturnsConfigError(t *testing.T) {
	err := run(context.Background(), []string{"serve"}, emptyEnv, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run(serve) error = %v, want config error mentioning DATABASE_URL", err)
	}
}

// startServer runs runHTTPServer in the background and returns its address and
// a channel that receives runHTTPServer's return value.
func startServer(t *testing.T, ctx context.Context, h http.Handler) (string, <-chan error) {
	t.Helper()
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- runHTTPServer(ctx, "127.0.0.1:0", h, 5*time.Second, discardLogger(), func(a net.Addr) { addrCh <- a })
	}()
	select {
	case a := <-addrCh:
		return "http://" + a.String(), done
	case err := <-done:
		t.Fatalf("server exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start listening within 5s")
	}
	return "", nil
}

func TestServe_ShutsDownWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	base, done := startServer(t, ctx, h)

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET before shutdown: %v", err)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHTTPServer returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5s of context cancellation")
	}
}

func TestServe_InFlightRequestFinishesDuringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond) // simulate a slow request
		_, _ = io.WriteString(w, "finished")
	})
	base, done := startServer(t, ctx, h)

	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get(base + "/")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		resCh <- result{body: string(b), err: err}
	}()

	<-started
	cancel() // shutdown begins while the request is still running

	res := <-resCh
	if res.err != nil || res.body != "finished" {
		t.Fatalf("in-flight request = (%q, %v), want (\"finished\", nil)", res.body, res.err)
	}
	if err := <-done; err != nil {
		t.Fatalf("runHTTPServer returned %v, want nil", err)
	}
}
