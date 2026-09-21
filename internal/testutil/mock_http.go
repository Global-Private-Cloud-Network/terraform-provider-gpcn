package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/client"
)

// MockTransport is a custom RoundTripper that rewrites URLs to point to a test server.
// This allows tests to intercept HTTP calls and return mock responses without actually
// making network requests.
type MockTransport struct {
	BaseURL string
}

// RoundTrip implements the http.RoundTripper interface.
// It rewrites the request URL to point to the mock server while preserving the path.
func (t *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point to our mock server
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.BaseURL, "http://")

	// Use default transport to actually make the request to the mock server
	return http.DefaultTransport.RoundTrip(req)
}

// MockServerConfig defines the configuration for setting up a mock HTTP server
type MockServerConfig struct {
	// T is the testing instance for logging and errors
	T *testing.T

	// Handler is a custom handler function that will be invoked for all requests
	Handler func(w http.ResponseWriter, r *http.Request)
}

// SetupMockServer creates a mock HTTP server and returns the server and configured HTTP client
func SetupMockServer(config MockServerConfig) (*httptest.Server, *http.Client) {
	server := httptest.NewServer(http.HandlerFunc(config.Handler))

	httpClient := &http.Client{
		Transport: &MockTransport{
			BaseURL: server.URL,
		},
	}

	return server, httpClient
}

// SetupMockServerWithGpcnClient creates a mock HTTP server and returns the server and configured GpcnClient
func SetupMockServerWithGpcnClient(config MockServerConfig) (*httptest.Server, *client.GpcnClient) {
	server, httpClient := SetupMockServer(config)
	gpcnClient := client.NewGpcnClientFromHTTPClient(httpClient)
	return server, gpcnClient
}

// HandleJobResponse is a helper function to handle job status polling requests
func HandleJobResponse(w http.ResponseWriter, jobID, resourceID string, completed bool) {
	response := client.JobStatusMultiResponse{
		Success: true,
		Message: "Job status retrieved",
		Data: client.JobStatusDataResponse{
			Jobs: []client.JobResponse{
				{
					JobID:       jobID,
					ResourceId:  resourceID,
					IsCompleted: completed,
					HasFailed:   false,
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// HandleCreateJobResponse is a helper function to handle responses that start a job
func HandleCreateJobResponse(w http.ResponseWriter, jobID string, message string) {
	response := client.JobStatusSingularResponse{
		Success: true,
		Message: message,
		Data: client.JobResponse{
			JobID: jobID,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// ReadRequestBody reads and unmarshals the request body into a map
func ReadRequestBody(r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	var requestBody map[string]any
	_ = json.Unmarshal(body, &requestBody)
	return requestBody
}

// WriteJSONResponse writes a JSON response with the given data
func WriteJSONResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// LogUnexpectedRequest logs an unexpected request and returns 404
func LogUnexpectedRequest(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNotFound)
}

// SetupMockServerWithRealTransport builds the client through NewGpcnClient. The real
// authTransport stays in the stack, so a mocked 404 arrives as *client.HTTPError.
// SetupMockServerWithGpcnClient bypasses that transport, so not-found paths cannot be
// tested through it.
func SetupMockServerWithRealTransport(config MockServerConfig) (*httptest.Server, *client.GpcnClient) {
	config.T.Helper()

	server := httptest.NewServer(http.HandlerFunc(config.Handler))
	// A failure below leaves the listener open, so the cleanup closes it.
	config.T.Cleanup(server.Close)

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 0
	cfg.InitialRetryDelay = 0

	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		config.T.Fatalf("failed to create GPCN client: %v", err)
	}

	return server, gpcnClient
}

// Provider Configure calls this endpoint before any resource work. Every mock
// server a provider test drives needs an arm for it.
func HandleAuthCheck(w http.ResponseWriter) {
	WriteJSONResponse(w, map[string]any{
		"success": true,
		"message": "",
		"data": map[string]any{
			"authenticated": true,
			"credential": map[string]any{
				"kind":      "api_key",
				"id":        "key-1",
				"name":      "terraform",
				"keyStart":  "gpcn_test",
				"entityId":  "entity-1",
				"expiresAt": nil,
			},
			"grants": map[string]any{
				"source":      "api_key_role",
				"entityId":    "entity-1",
				"permissions": []string{},
			},
		},
		"meta": nil,
	})
}
