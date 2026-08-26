package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GpcnClient wraps http.Client with configuration and retry support
type GpcnClient struct {
	httpClient *http.Client
	config     *Config
}

type authTransport struct {
	Host      string
	apiKey    string
	Transport http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	req.Header.Add("x-api-key", t.apiKey)

	// Add correlation ID to request headers if present
	if correlationID := GetCorrelationID(req.Context()); correlationID != "" {
		req.Header.Add("x-Correlation-ID", correlationID)
	}

	// Gather base URL from Host
	baseUrl, err := url.Parse(t.Host)
	if err != nil {
		return nil, fmt.Errorf("unable to parse base URL %q: %w", t.Host, err)
	}
	// Gather path URL from request
	pathUrl := req.URL.String()
	// Combine
	finalUrl := baseUrl.String() + pathUrl
	req.URL, err = url.Parse(finalUrl)
	if err != nil {
		return nil, fmt.Errorf("unable to combine base URL %q with path URL %q: %w", t.Host, pathUrl, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Handle HTTP errors - read and include response body in error for debugging
	if resp.StatusCode >= 400 {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("HTTP error %d, failed to read response body: %w", resp.StatusCode, readErr)
		}
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(bodyBytes),
		}
	}

	return resp, nil
}

// HTTPError represents an HTTP error response with status code and body
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("HTTP error %d: %s", e.StatusCode, e.Body)
	}
	return "HTTP error " + strconv.Itoa(e.StatusCode)
}

// IsRetryable returns true if the error is a transient failure that can be retried
func (e *HTTPError) IsRetryable() bool {
	// Retry on 5xx server errors and 429 rate limiting
	return e.StatusCode >= 500 || e.StatusCode == 429
}

// NewGpcnClient creates a new GPCN client with the given configuration
func NewGpcnClient(config *Config) (*GpcnClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid client configuration: %w", err)
	}

	httpClient := &http.Client{
		Timeout: config.RequestTimeout,
		Transport: &authTransport{
			Host:      config.Host,
			apiKey:    config.APIKey,
			Transport: http.DefaultTransport,
		},
	}

	return &GpcnClient{
		httpClient: httpClient,
		config:     config,
	}, nil
}

// HTTPClient returns the underlying http.Client for backwards compatibility
func (c *GpcnClient) HTTPClient() *http.Client {
	return c.httpClient
}

// NewGpcnClientFromHTTPClient creates a GpcnClient from an existing http.Client (for testing)
func NewGpcnClientFromHTTPClient(httpClient *http.Client) *GpcnClient {
	return &GpcnClient{
		httpClient: httpClient,
		config:     DefaultConfig("", ""),
	}
}

// Config returns the client configuration
func (c *GpcnClient) Config() *Config {
	return c.config
}

// DoWithRetry performs an HTTP request with exponential backoff retry
func (c *GpcnClient) DoWithRetry(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	var lastErr error
	delay := c.config.InitialRetryDelay

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		// Clone the request for retry (body needs special handling)
		reqClone := req.Clone(ctx)

		// req.Clone() shallow-copies Body, so after the first attempt reads it the
		// reader is exhausted. Reset it on retries using GetBody, which is set
		// automatically by http.NewRequestWithContext for bytes.Buffer, bytes.Reader,
		// and strings.Reader bodies.
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to get request body for retry attempt %d: %w", attempt, err)
			}
			reqClone.Body = body
		}

		//nolint:gosec // G704: URL is constructed from validated config, not user input
		resp, err := c.httpClient.Do(reqClone)

		if err == nil {
			return resp, nil
		}

		lastErr = err

		// A canceled or expired caller context is not a transient failure:
		// retrying cannot succeed, and backing off first would keep Terraform
		// sleeping after the user has already interrupted the run. Check the
		// context itself rather than the error, so that a per-request timeout
		// (http.Client.Timeout) stays retryable.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%w: %w", ctxErr, err)
		}

		// Check if error is retryable
		var httpErr *HTTPError
		if ok := isHTTPError(err, &httpErr); ok && !httpErr.IsRetryable() {
			return nil, err // Don't retry non-retryable errors
		}

		// Don't sleep after the last attempt
		if attempt < c.config.MaxRetries {
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, fmt.Errorf("%w: %w", sleepErr, lastErr)
			}
			// Exponential backoff with cap
			delay *= 2
			if delay > c.config.MaxRetryDelay {
				delay = c.config.MaxRetryDelay
			}
		}
	}

	return nil, fmt.Errorf("%w: %w", ErrMaxRetriesExceeded, lastErr)
}

// sleepWithContext waits for d to elapse, returning early with the context's
// error if ctx is canceled or expires first. It replaces bare time.Sleep calls
// so that retry backoff and polling intervals stay interruptible.
func sleepWithContext(ctx context.Context, d time.Duration) error {
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

// isHTTPError checks if the error is an HTTPError and assigns it to target
func isHTTPError(err error, target **HTTPError) bool {
	if httpErr, ok := errors.AsType[*HTTPError](err); ok {
		*target = httpErr
		return true
	}
	return false
}

// IsNotFound reports whether err was caused by an HTTP 404 response.
//
// Read implementations use this to detect that a resource was deleted outside
// of Terraform, so it can be removed from state rather than failing every
// subsequent operation. Delete implementations use it to treat an
// already-deleted resource as success.
func IsNotFound(err error) bool {
	var httpErr *HTTPError
	if ok := isHTTPError(err, &httpErr); ok {
		return httpErr.StatusCode == http.StatusNotFound
	}
	return false
}
