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

// The helpers below write literal JSON, never the client's own types, so a
// change to the client's decoder cannot make its fixtures agree with it.

// jsonString renders s as a JSON string, escaped as encoding/json escapes it.
func jsonString(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// jsonStringOrNull renders "" as null and any other value as a JSON string.
func jsonStringOrNull(s string) string {
	if s == "" {
		return "null"
	}
	return jsonString(s)
}

func writeLiteralJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// HandleJobResponse answers a job status read with one job, as the API reports
// it: an id, a stage and an outcome that stays null until the job ends.
func HandleJobResponse(w http.ResponseWriter, jobID, resourceID string, completed bool) {
	stage, outcome, progress, completedAt := `"running"`, "null", "50", "null"
	if completed {
		stage, outcome, progress, completedAt = `"completed"`, `"succeeded"`, "100", `"2026-11-02T00:00:01Z"`
	}
	writeLiteralJSON(w, `{"success":true,"message":"Job status retrieved","data":{"jobs":[{`+
		`"id":`+jsonString(jobID)+`,"resourceType":"virtual_machine","operation":"create","verb":"Creating",`+
		`"resourceId":`+jsonStringOrNull(resourceID)+`,"resourceName":null,"stage":`+stage+`,"outcome":`+outcome+`,`+
		`"progress":`+progress+`,"message":null,"failureReason":null,"retryable":false,`+
		`"createdAt":"2026-11-02T00:00:00Z","startedAt":"2026-11-02T00:00:00Z","lastProgressAt":"2026-11-02T00:00:00Z",`+
		`"completedAt":`+completedAt+`,"version":1}],"notFound":[]}}`)
}

// HandleCreateJobResponse answers a request that starts a job, as the API
// accepts it: the job under data.job, queued.
func HandleCreateJobResponse(w http.ResponseWriter, jobID string, message string) {
	writeLiteralJSON(w, `{"success":true,"message":`+jsonString(message)+`,"data":{"job":{`+
		`"id":`+jsonString(jobID)+`,"resourceType":"virtual_machine","operation":"create","verb":"Creating",`+
		`"resourceId":null,"resourceName":null,"stage":"queued","outcome":null,"progress":0,`+
		`"message":null,"failureReason":null,"retryable":false,`+
		`"createdAt":"2026-11-02T00:00:00Z","startedAt":null,"lastProgressAt":null,"completedAt":null,"version":1}}}`)
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
