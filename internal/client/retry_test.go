package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
)

// retryAfterServer answers 429 with the given retryAfter seconds until the
// second attempt, which succeeds. The attempt counter proves the retry ran.
func retryAfterServer(t *testing.T, retryAfterSeconds int) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "840")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"Rate Limited",` +
				`"message":"Too many requests. Please try again in 14 minutes.",` +
				`"retryAfter":` + strconv.Itoa(retryAfterSeconds) + `}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	return server, &attempts
}

// TestDoWithRetryHonoursRetryAfter proves the retry waits the interval the API
// asks for. The retry uses that interval instead of its own backoff. The
// configured maximum delay still bounds the wait, because a rate limiter can
// ask for fourteen minutes.
func TestDoWithRetryHonoursRetryAfter(t *testing.T) {
	t.Run("caps a long retryAfter at the maximum delay", func(t *testing.T) {
		server, attempts := retryAfterServer(t, 840)

		cfg := client.DefaultConfig(server.URL, "test-key")
		cfg.MaxRetries = 1
		cfg.InitialRetryDelay = 0
		cfg.MaxRetryDelay = 40 * time.Millisecond
		gpcnClient, err := client.NewGpcnClient(cfg)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		start := time.Now()
		resp, err := gpcnClient.DoWithRetry(req)
		elapsed := time.Since(start)
		if resp != nil {
			resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("DoWithRetry returned unexpected error: %v", err)
		}
		if got := int(attempts.Load()); got != 2 {
			t.Errorf("attempts = %d, want 2", got)
		}
		if elapsed < cfg.MaxRetryDelay {
			t.Errorf("elapsed = %s, want at least the capped delay %s", elapsed, cfg.MaxRetryDelay)
		}
		if elapsed > time.Second {
			t.Errorf("elapsed = %s, want the wait capped well under the requested 840s", elapsed)
		}
	})

	t.Run("replaces the computed backoff", func(t *testing.T) {
		server, attempts := retryAfterServer(t, 1)

		cfg := client.DefaultConfig(server.URL, "test-key")
		cfg.MaxRetries = 1
		cfg.InitialRetryDelay = 3 * time.Second
		cfg.MaxRetryDelay = 3 * time.Second
		gpcnClient, err := client.NewGpcnClient(cfg)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		start := time.Now()
		resp, err := gpcnClient.DoWithRetry(req)
		elapsed := time.Since(start)
		if resp != nil {
			resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("DoWithRetry returned unexpected error: %v", err)
		}
		if got := int(attempts.Load()); got != 2 {
			t.Errorf("attempts = %d, want 2", got)
		}
		if elapsed >= cfg.InitialRetryDelay {
			t.Errorf("elapsed = %s, want less than the computed backoff %s", elapsed, cfg.InitialRetryDelay)
		}
		if elapsed < time.Second {
			t.Errorf("elapsed = %s, want at least the requested 1s", elapsed)
		}
	})
}

// TestDoOnceSendsOneRequest proves DoOnce does not retry a 503 that DoWithRetry
// would retry.
func TestDoOnceSendsOneRequest(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"try later"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 3
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := gpcnClient.DoOnce(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected the 503 error, got a response")
	}
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected an HTTP 503 error, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected exactly 1 request, got %d", got)
	}
}
