// Package worker contains background jobs that drive the application.
package worker

import (
	"context"
	"log/slog"
	"time"
)

// UserCounter is the minimal dependency the reporter needs.
type UserCounter interface {
	Count(ctx context.Context) (int64, error)
}

// UserCountReporter periodically logs the total number of users.
type UserCountReporter struct {
	counter  UserCounter
	interval time.Duration
	logger   *slog.Logger
}

// NewUserCountReporter builds a reporter that ticks every interval.
func NewUserCountReporter(counter UserCounter, interval time.Duration, logger *slog.Logger) *UserCountReporter {
	return &UserCountReporter{counter: counter, interval: interval, logger: logger}
}

// Run blocks, logging the user count on every tick, until ctx is cancelled.
// Call it in its own goroutine.
func (r *UserCountReporter) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.Info("user count reporter started", "interval", r.interval.String())
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("user count reporter stopped")
			return
		case <-ticker.C:
			r.report(ctx)
		}
	}
}

func (r *UserCountReporter) report(parent context.Context) {
	// Bound each query so a slow database cannot pile up ticks.
	ctx, cancel := context.WithTimeout(parent, r.interval)
	defer cancel()
	n, err := r.counter.Count(ctx)
	switch {
	case err == nil:
		r.logger.Info("total users", "count", n)
	case parent.Err() != nil:
		// Shutting down; a cancelled query is expected, not an error.
	default:
		r.logger.Error("count users failed", "error", err)
	}
}
