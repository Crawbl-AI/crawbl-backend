// Package runtime provides process lifecycle helpers for crawbl binaries.
package runtime

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// RunUntilSignal runs the supplied Run function until it returns, until it
// returns an error, or until the process receives SIGINT/SIGTERM/SIGQUIT/SIGHUP.
// On signal, the stop function is called with a timeout context; if stop is nil,
// the process exits immediately. Any signal is propagated through ctx; callers
// must honor ctx.Done inside their Run function.
func RunUntilSignal(run func() error, stop func(context.Context) error, timeout time.Duration) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx, stop_ := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer stop_()

	g, _ := errgroup.WithContext(ctx)

	// Run the main function.
	g.Go(func() error { return run() })

	// Wait for completion or signal.
	if err := g.Wait(); err != nil {
		return err
	}

	// If signal was received, run cleanup.
	if ctx.Err() != nil && stop != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), timeout)
		defer stopCancel()
		return stop(stopCtx)
	}

	return nil
}
