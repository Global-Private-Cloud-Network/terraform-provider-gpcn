package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
)

const incompleteJobResponse = `{"success":true,"message":"ok","data":{"jobs":[{"jobId":"job-1","isCompleted":false,"hasFailed":false}]}}`

func newPollingClient(t *testing.T, serverURL string) *client.GpcnClient {
	t.Helper()

	cfg := client.DefaultConfig(serverURL, "test-key")
	cfg.MaxRetries = 0
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return gpcnClient
}

// TestPerformLongPollingStopsOnContextCancellation verifies that canceling the
// context wakes the interval sleep. The polling loop runs for the lifetime of a
// create/update/delete, so a bare sleep here keeps Terraform running for a full
// interval per in-flight resource after the user has pressed Ctrl-C.
func TestPerformLongPollingStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	served := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(incompleteJobResponse))
		select {
		case served <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cfg := &client.PollingConfig{
		Timeout:         30 * time.Second,
		InitialInterval: 2 * time.Second,
		MaxInterval:     2 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		_, err := client.PerformLongPollingWithConfig(newPollingClient(t, server.URL), ctx, "create", "job-1", cfg)
		done <- err
	}()

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("polling never reached the server")
	}

	// Let the first poll return so the loop is inside its interval sleep.
	time.Sleep(100 * time.Millisecond)
	canceledAt := time.Now()
	cancel()

	select {
	case err := <-done:
		if waited := time.Since(canceledAt); waited > 500*time.Millisecond {
			t.Errorf("polling took %s to return after cancellation, expected it to abandon the interval sleep", waited)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("polling did not return after the context was canceled")
	}
}

// TestPerformLongPollingTimeoutAccountsForPollDuration verifies that the timeout is
// measured against time spent including the poll that just completed. Sampling
// elapsed at the top of the loop instead granted one extra full round trip past the
// deadline, which with a 60s request timeout turns a 10m polling_timeout into ~11m.
func TestPerformLongPollingTimeoutAccountsForPollDuration(t *testing.T) {
	t.Parallel()

	const pollDuration = 200 * time.Millisecond
	var pollCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount.Add(1)
		time.Sleep(pollDuration)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(incompleteJobResponse))
	}))
	defer server.Close()

	cfg := &client.PollingConfig{
		Timeout:         50 * time.Millisecond,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     1 * time.Millisecond,
	}

	start := time.Now()
	_, err := client.PerformLongPollingWithConfig(newPollingClient(t, server.URL), t.Context(), "create", "job-1", cfg)
	elapsed := time.Since(start)

	if !errors.Is(err, client.ErrLongPollingTimeout) {
		t.Fatalf("error = %v, want it to wrap ErrLongPollingTimeout", err)
	}
	if got := pollCount.Load(); got != 1 {
		t.Errorf("server saw %d polls, want 1: the timeout should be detected as soon as the first poll returns", got)
	}
	// One poll has to complete before the deadline can be observed at all, so the
	// floor is pollDuration; anything beyond that is overrun.
	if elapsed > pollDuration+100*time.Millisecond {
		t.Errorf("polling ran for %s with a %s timeout, want it to stop after the first poll (~%s)", elapsed, cfg.Timeout, pollDuration)
	}
}
