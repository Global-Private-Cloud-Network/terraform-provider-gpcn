package client_test

import (
	"bytes"
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
