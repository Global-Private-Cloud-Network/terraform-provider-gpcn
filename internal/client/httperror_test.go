package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
)

// errorFromStatusBody drives a single failing response through the real client
// stack, so the assertions cover the parse where it happens: authTransport.
func errorFromStatusBody(t *testing.T, status int, body string, headers map[string]string) error {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 0
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, doErr := gpcnClient.DoWithRetry(req)
	if resp != nil {
		resp.Body.Close()
	}
	if doErr == nil {
		t.Fatalf("expected an error for status %d, got nil", status)
	}
	return doErr
}

// TestHTTPErrorParsesEnvelopeShapes pins the four error envelopes the API can
// answer with, and the raw fallback. The backend has no single envelope. A
// client that parses only the dominant shape mis-reads the other three. It then
// surfaces raw JSON in a Terraform diagnostic.
func TestHTTPErrorParsesEnvelopeShapes(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		body           string
		headers        map[string]string
		wantCode       string
		wantMessage    string
		wantRetryAfter time.Duration
		wantDetailsKey string
	}{
		{
			name:   "shape A zod 422 with details.issues",
			status: http.StatusUnprocessableEntity,
			body: `{"success":false,"message":"Invalid Parameters: name: Name is required",` +
				`"error":{"code":"Validation Error","statusCode":422,` +
				`"details":{"issues":[{"path":"name","message":"Name is required"}]}}}`,
			wantCode:       "Validation Error",
			wantMessage:    "Invalid Parameters: name: Name is required",
			wantDetailsKey: "issues",
		},
		{
			name:   "shape A errorResponse 403 with details null",
			status: http.StatusForbidden,
			body: `{"success":false,"message":"You do not have permission to perform this action.",` +
				`"error":{"code":"Insufficient Permissions","statusCode":403,"details":null}}`,
			wantCode:    "Insufficient Permissions",
			wantMessage: "You do not have permission to perform this action.",
		},
		{
			name:   "shape C 429 with retryAfter and header",
			status: http.StatusTooManyRequests,
			body: `{"success":false,"error":{"code":"Rate Limited",` +
				`"message":"Too many requests. Please try again in 14 minutes.","retryAfter":840}}`,
			headers:        map[string]string{"Retry-After": "840"},
			wantCode:       "Rate Limited",
			wantMessage:    "Too many requests. Please try again in 14 minutes.",
			wantRetryAfter: 840 * time.Second,
		},
		{
			name:           "shape C 429 with the header alone",
			status:         http.StatusTooManyRequests,
			body:           `{"success":false,"error":{"code":"Rate Limited","message":"Too many requests."}}`,
			headers:        map[string]string{"Retry-After": "30"},
			wantCode:       "Rate Limited",
			wantMessage:    "Too many requests.",
			wantRetryAfter: 30 * time.Second,
		},
		{
			name:        "shape D route not found",
			status:      http.StatusNotFound,
			body:        `{"message":"Route Not Found"}`,
			wantMessage: "Route Not Found",
		},
		{
			name:   "unparseable HTML",
			status: http.StatusBadGateway,
			body:   `<html><body><h1>502 Bad Gateway</h1></body></html>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errorFromStatusBody(t, tt.status, tt.body, tt.headers)

			var httpErr *client.HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected a *client.HTTPError, got %T: %v", err, err)
			}
			if httpErr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", httpErr.StatusCode, tt.status)
			}
			if httpErr.Body != tt.body {
				t.Errorf("Body = %q, want %q", httpErr.Body, tt.body)
			}
			if httpErr.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", httpErr.Code, tt.wantCode)
			}
			if httpErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", httpErr.Message, tt.wantMessage)
			}
			if httpErr.RetryAfter != tt.wantRetryAfter {
				t.Errorf("RetryAfter = %s, want %s", httpErr.RetryAfter, tt.wantRetryAfter)
			}
			if tt.wantDetailsKey == "" {
				if httpErr.Details != nil {
					t.Errorf("Details = %v, want nil", httpErr.Details)
				}
				return
			}
			if _, ok := httpErr.Details[tt.wantDetailsKey]; !ok {
				t.Errorf("Details = %v, want key %q", httpErr.Details, tt.wantDetailsKey)
			}
		})
	}
}

// TestHTTPErrorRendersCodeAndMessage pins the three render forms. The raw form
// must survive for an unparseable body, because a caller matches on it.
func TestHTTPErrorRendersCodeAndMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *client.HTTPError
		want string
	}{
		{
			name: "code and message",
			err: &client.HTTPError{
				StatusCode: 403,
				Body:       `{"message":"This account has moved to VPC networking."}`,
				Code:       "LEGACY_NETWORKING_CUT_OVER",
				Message:    "This account has moved to VPC networking.",
			},
			want: "HTTP 403 (LEGACY_NETWORKING_CUT_OVER): This account has moved to VPC networking.",
		},
		{
			name: "message alone",
			err: &client.HTTPError{
				StatusCode: 404,
				Body:       `{"message":"Route Not Found"}`,
				Message:    "Route Not Found",
			},
			want: "HTTP 404: Route Not Found",
		},
		{
			name: "unparseable body keeps the raw form",
			err:  &client.HTTPError{StatusCode: 502, Body: "<html>502</html>"},
			want: "HTTP error 502: <html>502</html>",
		},
		{
			name: "empty body keeps the raw form",
			err:  &client.HTTPError{StatusCode: 500},
			want: "HTTP error 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
