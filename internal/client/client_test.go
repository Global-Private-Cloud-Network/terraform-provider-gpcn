package client_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"terraform-provider-gpcn/internal/client"
)

// TestDoWithRetryBodyResetOnRetry verifies that retry attempts re-send the original
// request body rather than an empty body (regression test for body exhaustion on Clone).
func TestDoWithRetryBodyResetOnRetry(t *testing.T) {
	t.Parallel()

	const wantBody = `{"key":"value"}`
	var attemptCount atomic.Int32
	var retryBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := int(attemptCount.Add(1))
		body, _ := io.ReadAll(r.Body)
		if attempt == 1 {
			// First attempt: return 500 to trigger a retry
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Second attempt: record the body and return success
		retryBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 1
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/test", bytes.NewBufferString(wantBody))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := gpcnClient.DoWithRetry(req)
	if err != nil {
		t.Fatalf("DoWithRetry returned unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if int(attemptCount.Load()) != 2 {
		t.Errorf("expected 2 attempts, got %d", attemptCount.Load())
	}
	if retryBody != wantBody {
		t.Errorf("retry body = %q, want %q", retryBody, wantBody)
	}
}

// TestDoWithRetryNoRetryOn400 verifies that a 400 response is not retried.
func TestDoWithRetryNoRetryOn400(t *testing.T) {
	t.Parallel()

	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 3
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/test", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := gpcnClient.DoWithRetry(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}

	if int(attemptCount.Load()) != 1 {
		t.Errorf("expected exactly 1 attempt for non-retryable 400, got %d", attemptCount.Load())
	}
}

// TestIsNotFound verifies that only genuine HTTP 404 responses are reported as
// "not found". Read implementations use this to remove a resource from state, and
// Delete implementations to treat an already-deleted resource as success, so a
// false positive here would silently discard a live resource from state.
func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"404", &client.HTTPError{StatusCode: 404, Body: "not found"}, true},
		{"404 with empty body", &client.HTTPError{StatusCode: 404}, true},
		{"400", &client.HTTPError{StatusCode: 400, Body: "bad request"}, false},
		{"403", &client.HTTPError{StatusCode: 403}, false},
		{"409", &client.HTTPError{StatusCode: 409}, false},
		{"500", &client.HTTPError{StatusCode: 500}, false},
		{"200", &client.HTTPError{StatusCode: 200}, false},
		{"non-HTTP error", errors.New("connection refused"), false},
		{"wrapped 404", fmt.Errorf("get network: %w", &client.HTTPError{StatusCode: 404}), true},
		{"doubly wrapped 404", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &client.HTTPError{StatusCode: 404})), true},
		{"wrapped 500", fmt.Errorf("get network: %w", &client.HTTPError{StatusCode: 500}), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := client.IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestIsNotFoundThroughRoundTrip verifies IsNotFound works on an error as it is
// actually produced by the client stack, not just on a hand-constructed HTTPError.
// http.Client wraps transport errors in *url.Error, so this covers that unwrap.
func TestIsNotFoundThroughRoundTrip(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"nope"}`))
			}))
			defer server.Close()

			cfg := client.DefaultConfig(server.URL, "test-key")
			cfg.MaxRetries = 0
			gpcnClient, err := client.NewGpcnClient(cfg)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			resp, err := gpcnClient.DoWithRetry(req)
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			want := status == http.StatusNotFound
			if got := client.IsNotFound(err); got != want {
				t.Errorf("IsNotFound(%v) = %v, want %v", err, got, want)
			}
		})
	}
}
