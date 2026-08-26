package client

import (
	"context"
	"time"
)

// SleepWithContext waits for the time d. It stops early and returns the error of
// ctx if ctx is canceled or expires first.
//
// Use this function in place of time.Sleep for each retry backoff and each poll
// interval. An interrupted Terraform run must not continue to wait.
//
// If d is zero or negative, this function does not wait, but it still returns
// the error of ctx. A caller with a zero interval also learns about a
// cancellation.
func SleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
