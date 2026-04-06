// Package runtime provides helpers for running services until an OS signal is received.
package runtime

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func RunUntilSignal(run func() error, stop func(context.Context) error, timeout time.Duration) error {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer signal.Stop(signalChannel)

	errChannel := make(chan error, 1)
	go func() {
		errChannel <- run()
	}()

	select {
	case err := <-errChannel:
		return err
	case <-signalChannel:
		if stop == nil {
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return stop(ctx)
	}
}
