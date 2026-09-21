package client

import (
	"encoding/json"
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
	// Reported by AuthCheck at configure time. Both stay zero until then.
	entityID    string
	permissions []string
}

type authTransport struct {
	Host      string
	apiKey    string
	Transport http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	req.Header.Set("x-api-key", t.apiKey)

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
		return nil, newHTTPError(resp, bodyBytes)
	}

	return resp, nil
}

// HTTPError represents an HTTP error response with status code and body
type HTTPError struct {
	StatusCode int
	Body       string
	Code       string
	Message    string
	Details    map[string]any
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		if e.Code != "" {
			return fmt.Sprintf("HTTP %d (%s): %s", e.StatusCode, e.Code, e.Message)
		}
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
	}
	if e.Body != "" {
		return fmt.Sprintf("HTTP error %d: %s", e.StatusCode, e.Body)
	}
	return "HTTP error " + strconv.Itoa(e.StatusCode)
}

// errorEnvelope covers the error bodies the GPCN API can answer with. Every
// field is a pointer, because presence is what tells the shapes apart: the
// dominant shape puts the sentence at the top level, and the rate limiter puts
// it inside "error" with no top-level message at all.
type errorEnvelope struct {
	Message *string `json:"message"`
	Error   *struct {
		Code       *string        `json:"code"`
		Message    *string        `json:"message"`
		RetryAfter *int           `json:"retryAfter"`
		Details    map[string]any `json:"details"`
	} `json:"error"`
}

// newHTTPError parses the response body once, where it is read. A body that
// matches no known shape keeps Code and Message empty, so Error() falls back to
// the raw form.
func newHTTPError(resp *http.Response, body []byte) *HTTPError {
	httpErr := &HTTPError{
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}

	var envelope errorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil {
		switch {
		case envelope.Message != nil && envelope.Error != nil:
			httpErr.Message = *envelope.Message
			httpErr.Code = derefString(envelope.Error.Code)
			httpErr.Details = envelope.Error.Details
		case envelope.Error != nil && envelope.Error.Message != nil:
			httpErr.Message = *envelope.Error.Message
			httpErr.Code = derefString(envelope.Error.Code)
			if envelope.Error.RetryAfter != nil {
				httpErr.RetryAfter = time.Duration(*envelope.Error.RetryAfter) * time.Second
			}
		case envelope.Message != nil:
			httpErr.Message = *envelope.Message
		}
	}

	if httpErr.RetryAfter == 0 {
		httpErr.RetryAfter = retryAfterHeader(resp.Header)
	}

	return httpErr
}

// retryAfterHeader reads the seconds form of Retry-After. The HTTP-date form is
// ignored, because the rate limiter only ever sends seconds.
func retryAfterHeader(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
	var lastErr error
	delay := c.config.InitialRetryDelay

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		// Clone the request for retry (body needs special handling)
		reqClone := req.Clone(req.Context())

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

		// Check if error is retryable
		var httpErr *HTTPError
		if ok := isHTTPError(err, &httpErr); ok && !httpErr.IsRetryable() {
			return nil, err // Don't retry non-retryable errors
		}

		// Don't sleep after the last attempt
		if attempt < c.config.MaxRetries {
			time.Sleep(c.retryWait(delay, httpErr))
			// Exponential backoff with cap
			delay *= 2
			if delay > c.config.MaxRetryDelay {
				delay = c.config.MaxRetryDelay
			}
		}
	}

	return nil, fmt.Errorf("%w: %w", ErrMaxRetriesExceeded, lastErr)
}

// retryWait returns how long to wait before the next attempt. An API that says
// when to come back is obeyed in place of the computed backoff, but the
// configured maximum still bounds the wait: a rate limiter can ask for
// fourteen minutes, which no apply should sit through.
func (c *GpcnClient) retryWait(backoff time.Duration, httpErr *HTTPError) time.Duration {
	if httpErr == nil || httpErr.RetryAfter <= 0 {
		return backoff
	}
	if c.config.MaxRetryDelay > 0 && httpErr.RetryAfter > c.config.MaxRetryDelay {
		return c.config.MaxRetryDelay
	}
	return httpErr.RetryAfter
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
	return hasStatus(err, http.StatusNotFound)
}

// IsForbidden reports whether err was caused by an HTTP 403 response.
//
// A 403 is a refusal the caller cannot retry away: the role lacks the
// permission, the tenant lacks the feature, or the operation is walled off.
// Read implementations separate it from a 404, which removes state.
func IsForbidden(err error) bool {
	return hasStatus(err, http.StatusForbidden)
}

// IsConflict reports whether err was caused by an HTTP 409 response.
//
// The API answers 409 when the request fights live state, for example a name
// already taken or a container that still holds children.
func IsConflict(err error) bool {
	return hasStatus(err, http.StatusConflict)
}

// IsUnauthorized reports whether err was caused by an HTTP 401 response.
//
// Every credential failure answers the same 401, so this means the API key no
// longer authenticates, never that the key lacks a permission.
func IsUnauthorized(err error) bool {
	return hasStatus(err, http.StatusUnauthorized)
}

// ErrorCode returns the machine-readable code the API sent, or "" when err is
// not an HTTPError or carried an unparseable body.
func ErrorCode(err error) string {
	var httpErr *HTTPError
	if ok := isHTTPError(err, &httpErr); ok {
		return httpErr.Code
	}
	return ""
}

// HasErrorCode reports whether err carries exactly the given API error code.
// An empty code never matches, so a transport failure cannot answer yes.
func HasErrorCode(err error, code string) bool {
	if code == "" {
		return false
	}
	return ErrorCode(err) == code
}

func hasStatus(err error, status int) bool {
	var httpErr *HTTPError
	if ok := isHTTPError(err, &httpErr); ok {
		return httpErr.StatusCode == status
	}
	return false
}
