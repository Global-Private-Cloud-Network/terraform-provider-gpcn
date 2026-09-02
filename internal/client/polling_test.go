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

// config.Timeout must limit the full operation, not only the interval between
// polls. A check on the elapsed time cannot stop a poll that is in progress: the
// poll continues for its request timeout and all its retries.
func TestPerformLongPollingTimeoutBoundsInFlightPoll(t *testing.T) {
	t.Parallel()

	var pollCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount.Add(1)
		// The job status endpoint does not answer.
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.RequestTimeout = 100 * time.Millisecond
	cfg.MaxRetries = 3
	cfg.InitialRetryDelay = 50 * time.Millisecond
	cfg.MaxRetryDelay = 200 * time.Millisecond
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	pollCfg := &client.PollingConfig{
		Timeout:         200 * time.Millisecond,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     1 * time.Millisecond,
	}

	start := time.Now()
	_, err = client.PerformLongPollingWithConfig(gpcnClient, t.Context(), "create", "job-1", pollCfg)
	elapsed := time.Since(start)

	if !errors.Is(err, client.ErrLongPollingTimeout) {
		t.Fatalf("error = %v, want it to wrap ErrLongPollingTimeout", err)
	}
	// The request timeout and the retry backoff together are approximately 750ms.
	// A limit of 500ms detects a poll that continues past the deadline.
	if elapsed > 500*time.Millisecond {
		t.Errorf("polling ran for %s with a %s timeout, want the deadline to cut off the in-flight poll", elapsed, pollCfg.Timeout)
	}
	// DoWithRetry can correctly make a second attempt before the deadline. All
	// four attempts must not complete.
	if got := pollCount.Load(); got > 2 {
		t.Errorf("server saw %d attempts, want no more than 2: the deadline should cut the retry schedule short", got)
	}
}

// The loop has its own deadline. An interrupted run must still give
// context.Canceled, not a polling timeout, so that a caller can tell the two
// conditions apart.
func TestPerformLongPollingCancellationIsNotReportedAsTimeout(t *testing.T) {
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
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want it to wrap context.Canceled", err)
		}
		if errors.Is(err, client.ErrLongPollingTimeout) {
			t.Errorf("error = %v, want a cancellation not to be reported as a polling timeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("polling did not return after the context was canceled")
	}
}

// A per-request timeout also wraps context.DeadlineExceeded. Reporting it as a
// polling timeout names the wrong cause and claims a 30-second deadline expired
// after 50 milliseconds, so the conversion must also require pollCtx to be
// expired.
func TestPerformLongPollingRequestTimeoutIsNotAPollingTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond) // always exceeds RequestTimeout below
	}))
	defer server.Close()

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.RequestTimeout = 50 * time.Millisecond
	cfg.MaxRetries = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	pollCfg := &client.PollingConfig{Timeout: 30 * time.Second, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}

	start := time.Now()
	_, err = client.PerformLongPollingWithConfig(gpcnClient, t.Context(), "create", "job-1", pollCfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from the timed-out request")
	}
	if errors.Is(err, client.ErrLongPollingTimeout) {
		t.Errorf("error = %v, want a request timeout, not a polling timeout: only %s of the %s deadline had passed",
			err, elapsed.Round(time.Millisecond), pollCfg.Timeout)
	}
}

// The response that accompanies an error carries the resource ID, so a create
// that the run interrupted can still name what the API made.
func TestPerformLongPollingReturnsLastResourceIDOnFailure(t *testing.T) {
	t.Parallel()

	const resourceID = "res-77"
	var polls atomic.Int32
	served := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"jobs":[{"jobId":"job-1","isCompleted":false,"resourceId":"` + resourceID + `"}]}}`))
		select {
		case served <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	var got *client.JobStatusMultiResponse
	go func() {
		resp, err := client.PerformLongPollingWithConfig(newPollingClient(t, server.URL), ctx, "create", "job-1",
			&client.PollingConfig{Timeout: 30 * time.Second, InitialInterval: 2 * time.Second, MaxInterval: 2 * time.Second})
		got = resp
		done <- err
	}()

	<-served
	time.Sleep(100 * time.Millisecond)
	cancel()

	if err := <-done; err == nil {
		t.Fatal("expected an error after cancellation")
	}
	id, err := client.GetJobResourceID(got)
	if err != nil {
		t.Fatalf("GetJobResourceID on the returned response: %v", err)
	}
	if id != resourceID {
		t.Errorf("resource ID = %q, want %q", id, resourceID)
	}
}

// A job with no resource ID is an error. The base URL alone addresses the
// collection, so an empty ID reads every resource instead of one, and it gives
// a PartialCreateError nothing to name.
func TestGetJobResourceIDRejectsEmptyID(t *testing.T) {
	t.Parallel()

	resp := &client.JobStatusMultiResponse{}
	resp.Data.Jobs = []client.JobResponse{{JobID: "job-1", ResourceId: ""}}

	if _, err := client.GetJobResourceID(resp); !errors.Is(err, client.ErrEmptyResourceID) {
		t.Errorf("error = %v, want ErrEmptyResourceID", err)
	}
}

// PartialCreateFromPoll must not claim a resource it cannot name.
func TestPartialCreateFromPollWithoutResourceID(t *testing.T) {
	t.Parallel()

	cause := errors.New("polling failed")
	if got := client.PartialCreateFromPoll(nil, cause); !errors.Is(got, cause) || errors.Is(got, context.Canceled) {
		t.Errorf("PartialCreateFromPoll(nil, cause) = %v, want the cause unchanged", got)
	}

	var partial *client.PartialCreateError
	if errors.As(client.PartialCreateFromPoll(nil, cause), &partial) {
		t.Error("PartialCreateFromPoll reported a partial create with no resource ID")
	}
}
