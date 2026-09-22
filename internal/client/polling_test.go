package client_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
)

const (
	pollTestJobID  = "job-1"
	pollTestAction = "Create GPCN Network"
)

// The API and the provider both spell the stage the British way. The fixture
// and the expected sentence quote those bytes.
//
//nolint:misspell // Byte-exact quotes of the wire value and of ErrJobCancelled.
const (
	pollTestCancelStageJob   = `{"jobId":"job-1","stage":"cancelled","progressPercentage":40,` + `"message":"Stopped by an administrator","isCompleted":false,"isTerminal":true,"hasFailed":false}`
	pollTestCancelStageError = `job job-1 for action "Create GPCN Network" was cancelled by the platform`
)

// jobsServer answers every poll with the given body, and it counts the calls.
// A test can then prove the poller stopped instead of looping to its timeout.
func jobsServer(t *testing.T, body string) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/resource/jobs/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
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
	return gpcnClient, &calls
}

// fastPollingConfig keeps a test that does loop away from the ten minute
// production timeout.
func fastPollingConfig() *client.PollingConfig {
	return &client.PollingConfig{
		Timeout:         150 * time.Millisecond,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
	}
}

// TestPollStopsOnCancelledStage proves an operator cancel ends the poll. Such a
// job reports isCompleted false and hasFailed false. A poller that waits for
// either sits until its timeout, and it hides the cancel.
func TestPollStopsOnCancelledStage(t *testing.T) {
	gpcnClient, calls := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully",`+
		`"data":{"jobs":[`+pollTestCancelStageJob+`]}}`)

	_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
	if err == nil {
		t.Fatal("expected a job in the cancel stage to fail the poll, got nil")
	}

	if err.Error() != pollTestCancelStageError {
		t.Errorf("error = %q, want %q", err.Error(), pollTestCancelStageError)
	}
	if got := int(calls.Load()); got != 1 {
		t.Errorf("poll calls = %d, want exactly 1", got)
	}
}

// TestPollReportsJobErrorMessage proves the failure text comes from the job.
// The envelope message on the read path is always the same success sentence.
// A poller that reports it tells every user their job failed because progress
// was retrieved.
func TestPollReportsJobErrorMessage(t *testing.T) {
	t.Run("prefers the job errorMessage", func(t *testing.T) {
		gpcnClient, _ := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully",`+
			`"data":{"jobs":[{"jobId":"job-1","stage":"failed","progressPercentage":60,`+
			`"message":"Rolling back","errorMessage":"Quota exceeded",`+
			`"isCompleted":false,"isTerminal":true,"hasFailed":true}]}}`)

		_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
		if err == nil {
			t.Fatal("expected a failed job to fail the poll, got nil")
		}
		if !strings.Contains(err.Error(), "Quota exceeded") {
			t.Errorf("error = %q, want it to carry the job errorMessage", err.Error())
		}
		if strings.Contains(err.Error(), "Job progress retrieved successfully") {
			t.Errorf("error = %q, want the envelope message kept out of it", err.Error())
		}
	})

	t.Run("falls back to the job message", func(t *testing.T) {
		gpcnClient, _ := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully",`+
			`"data":{"jobs":[{"jobId":"job-1","stage":"failed","progressPercentage":60,`+
			`"message":"Provider refused the request",`+
			`"isCompleted":false,"isTerminal":true,"hasFailed":true}]}}`)

		_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
		if err == nil {
			t.Fatal("expected a failed job to fail the poll, got nil")
		}
		if !strings.Contains(err.Error(), "Provider refused the request") {
			t.Errorf("error = %q, want it to carry the job message", err.Error())
		}
	})

	t.Run("falls back to the envelope message", func(t *testing.T) {
		gpcnClient, _ := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully",`+
			`"data":{"jobs":[{"jobId":"job-1","stage":"failed","isCompleted":false,`+
			`"isTerminal":true,"hasFailed":true}]}}`)

		_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
		if err == nil {
			t.Fatal("expected a failed job to fail the poll, got nil")
		}
		if !strings.Contains(err.Error(), "Job progress retrieved successfully") {
			t.Errorf("error = %q, want the envelope message as the last resort", err.Error())
		}
	})

	t.Run("stops on a terminal job that never reported a failure", func(t *testing.T) {
		gpcnClient, calls := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully",`+
			`"data":{"jobs":[{"jobId":"job-1","stage":"failed","errorMessage":"Image not available",`+
			`"isCompleted":false,"isTerminal":true,"hasFailed":false}]}}`)

		_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
		if err == nil {
			t.Fatal("expected a terminal job to fail the poll, got nil")
		}
		if !strings.Contains(err.Error(), "Image not available") {
			t.Errorf("error = %q, want it to carry the job errorMessage", err.Error())
		}
		if got := int(calls.Load()); got != 1 {
			t.Errorf("poll calls = %d, want exactly 1", got)
		}
	})
}

// TestPollFailsFastOnInvisibleJob proves an empty jobs array ends the poll.
// The array stays empty for a job the caller may never see. A poller that keeps
// asking only burns the timeout.
func TestPollFailsFastOnInvisibleJob(t *testing.T) {
	gpcnClient, calls := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[]}}`)

	_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
	if err == nil {
		t.Fatal("expected an invisible job to fail the poll, got nil")
	}

	want := "job job-1 is not visible to this API key (it does not exist, belongs to another tenant, or was deleted)"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if got := int(calls.Load()); got != 1 {
		t.Errorf("poll calls = %d, want exactly 1", got)
	}
}

// TestPollDecodesFractionalProgress proves a fractional percentage still
// decodes. An integer field rejects the whole envelope, and the poll then fails
// on a job that is healthy.
func TestPollDecodesFractionalProgress(t *testing.T) {
	gpcnClient, _ := jobsServer(t, `{"success":true,"message":"Job progress retrieved successfully",`+
		`"data":{"jobs":[{"jobId":"job-1","stage":"completed","progressPercentage":12.5,`+
		`"message":"Working","isCompleted":true,"isTerminal":true,"hasFailed":false}]}}`)

	response, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, fastPollingConfig())
	if err != nil {
		t.Fatalf("expected a fractional progress to decode, got: %v", err)
	}
	if got := response.Data.Jobs[0].ProgressPercentage; got != 12.5 {
		t.Errorf("ProgressPercentage = %v, want 12.5", got)
	}
}
