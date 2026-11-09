package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type counterFunc func(context.Context) (int64, error)

func (f counterFunc) Count(ctx context.Context) (int64, error) { return f(ctx) }

// syncBuffer is a goroutine-safe bytes.Buffer for capturing logs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestUserCountReporter_LogsPeriodicallyAndStops(t *testing.T) {
	var calls atomic.Int32
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	r := NewUserCountReporter(counterFunc(func(context.Context) (int64, error) {
		calls.Add(1)
		return 3, nil
	}), 10*time.Millisecond, logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	deadline := time.After(2 * time.Second)
	for calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("only %d ticks observed", calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if out := logs.String(); !strings.Contains(out, "total users") || !strings.Contains(out, "count=3") {
		t.Errorf("expected count log, got:\n%s", out)
	}
}

func TestUserCountReporter_LogsErrors(t *testing.T) {
	logs := &syncBuffer{}
	r := NewUserCountReporter(counterFunc(func(context.Context) (int64, error) {
		return 0, errors.New("db unavailable")
	}), time.Hour, slog.New(slog.NewTextHandler(logs, nil)))

	r.report(context.Background())
	if !strings.Contains(logs.String(), "db unavailable") {
		t.Errorf("error not logged: %s", logs.String())
	}
}

func TestUserCountReporter_SilentOnShutdown(t *testing.T) {
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewUserCountReporter(counterFunc(func(ctx context.Context) (int64, error) {
		return 0, ctx.Err()
	}), time.Hour, slog.New(slog.NewTextHandler(logs, nil)))

	r.report(ctx)
	if strings.Contains(logs.String(), "count users failed") {
		t.Errorf("cancellation during shutdown should not be logged as error: %s", logs.String())
	}
}
