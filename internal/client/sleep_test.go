package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
)

func TestSleepWithContextWaitsForTheFullDuration(t *testing.T) {
	t.Parallel()

	start := time.Now()
	if err := client.SleepWithContext(t.Context(), 50*time.Millisecond); err != nil {
		t.Fatalf("SleepWithContext returned %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("SleepWithContext returned after %s, want it to wait the full 50ms", elapsed)
	}
}

func TestSleepWithContextReturnsEarlyOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := client.SleepWithContext(ctx, 10*time.Second)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("SleepWithContext took %s to notice cancellation, want it to wake immediately", elapsed)
	}
}

// A caller with a retry delay of zero must also learn that the run stopped. The
// interval must not control that result.
func TestSleepWithContextNonPositiveDurationStillReportsCancellation(t *testing.T) {
	t.Parallel()

	if err := client.SleepWithContext(t.Context(), 0); err != nil {
		t.Errorf("error = %v, want nil for a zero duration on a live context", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.SleepWithContext(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled for a zero duration on a canceled context", err)
	}
}
