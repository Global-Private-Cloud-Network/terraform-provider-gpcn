package client_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	if !errors.Is(err, client.ErrJobCancelledByPlatform) {
		t.Errorf("errors.Is(err, ErrJobCancelledByPlatform) = false, want true")
	}
	if errors.Is(err, client.ErrJobFailed) {
		t.Errorf("a platform-stopped job must not match ErrJobFailed")
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

// v3JobsBody wraps one job in the batch read answer that carries notFound.
func v3JobsBody(job string) string {
	return `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[` + job + `],"notFound":[]}}`
}

// jobsSequenceServer answers the polls with the bodies in order, and it repeats
// the last body after that.
func jobsSequenceServer(t *testing.T, bodies ...string) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/resource/jobs/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		call := int(calls.Add(1))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[min(call, len(bodies))-1]))
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

func millisecondPollingConfig() *client.PollingConfig {
	return &client.PollingConfig{
		Timeout:         5 * time.Second,
		InitialInterval: time.Millisecond,
		MaxInterval:     time.Millisecond,
	}
}

func decodeJob(t *testing.T, body string) client.JobResponse {
	t.Helper()

	var job client.JobResponse
	if err := json.Unmarshal([]byte(body), &job); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return job
}

func stringPointer(value string) *string { return &value }

func TestJobResponseDecodesLegacyFlatJob(t *testing.T) {
	job := decodeJob(t, `{"jobId":"job-1","stage":"in_progress","isCompleted":false,"isTerminal":false,`+
		`"hasFailed":false,"progressPercentage":40,"message":"Creating the machine",`+
		`"errorMessage":"Retrying a step","resourceId":"vm-1"}`)

	want := client.JobResponse{
		JobID:              "job-1",
		Stage:              "in_progress",
		ProgressPercentage: 40,
		Message:            "Creating the machine",
		ErrorMessage:       "Retrying a step",
		ResourceId:         "vm-1",
	}
	if !reflect.DeepEqual(job, want) {
		t.Errorf("decoded job = %+v, want %+v", job, want)
	}
}

func TestJobResponseDecodesV3FlatJob(t *testing.T) {
	tests := []struct {
		name string
		body string
		want client.JobResponse
	}{
		{
			name: "a running job",
			body: `{"id":"job-1","stage":"running","outcome":null,"progress":40,"message":"Creating the machine",` +
				`"failureReason":null,"resourceId":"vm-1"}`,
			want: client.JobResponse{
				JobID:              "job-1",
				Stage:              "running",
				ProgressPercentage: 40,
				Message:            "Creating the machine",
				ResourceId:         "vm-1",
			},
		},
		{
			name: "a failed job",
			body: `{"id":"job-1","stage":"failed","outcome":"failed","progress":60,"message":"Rolling back",` +
				`"failureReason":"capacity","resourceId":"vm-1"}`,
			want: client.JobResponse{
				JobID:              "job-1",
				Stage:              "failed",
				ProgressPercentage: 60,
				Message:            "Rolling back",
				ResourceId:         "vm-1",
				Outcome:            stringPointer("failed"),
				FailureReason:      "capacity",
			},
		},
		{
			name: "an empty id falls back to jobId",
			body: `{"jobId":"j1","id":""}`,
			want: client.JobResponse{JobID: "j1"},
		},
		{
			name: "progress wins over progressPercentage",
			body: `{"jobId":"j1","progress":40,"progressPercentage":10}`,
			want: client.JobResponse{JobID: "j1", ProgressPercentage: 40},
		},
		{
			name: "a null progress falls back to progressPercentage",
			body: `{"jobId":"j1","progress":null,"progressPercentage":10}`,
			want: client.JobResponse{JobID: "j1", ProgressPercentage: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if job := decodeJob(t, tt.body); !reflect.DeepEqual(job, tt.want) {
				t.Errorf("decoded job = %+v, want %+v", job, tt.want)
			}
		})
	}
}

func TestJobResponseDecodesV3DispatchElement(t *testing.T) {
	const row = `{"id":"row-1","name":"x"}`

	tests := []struct {
		name           string
		jobResourceID  string
		wantResourceID string
	}{
		{name: "the job names no resource", jobResourceID: `null`, wantResourceID: "row-1"},
		{name: "the job names its resource", jobResourceID: `"job-row"`, wantResourceID: "job-row"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := decodeJob(t, `{"job":{"id":"job-1","stage":"queued","outcome":null,"progress":0,`+
				`"message":null,"failureReason":null,"resourceId":`+tt.jobResourceID+`},"resource":`+row+`}`)

			if job.JobID != "job-1" {
				t.Errorf("JobID = %q, want %q", job.JobID, "job-1")
			}
			if string(job.Resource) != row {
				t.Errorf("Resource = %s, want %s", job.Resource, row)
			}
			if job.ResourceId != tt.wantResourceID {
				t.Errorf("ResourceId = %q, want %q", job.ResourceId, tt.wantResourceID)
			}
		})
	}

	edges := []struct {
		name string
		body string
	}{
		{name: "a null resource", body: `{"job":{"id":"j1"},"resource":null}`},
		{name: "a null job reads as a flat job", body: `{"jobId":"j1","job":null}`},
		{name: "a string job reads as a flat job", body: `{"jobId":"j1","job":"str"}`},
	}

	for _, tt := range edges {
		t.Run(tt.name, func(t *testing.T) {
			want := client.JobResponse{JobID: "j1"}
			if job := decodeJob(t, tt.body); !reflect.DeepEqual(job, want) {
				t.Errorf("decoded job = %+v, want %+v", job, want)
			}
		})
	}
}

func TestSingularResponseReadsBothDispatchShapes(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantResourceID string
	}{
		{
			name: "legacy",
			body: `{"success":true,"data":{"jobId":"j1"}}`,
		},
		{
			name: "v3",
			body: `{"success":true,"data":{"job":{"id":"j1","stage":"queued","outcome":null,"progress":0,` +
				`"resourceId":"r1"},"resource":{"id":"r1"}}}`,
			wantResourceID: "r1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response client.JobStatusSingularResponse
			if err := json.Unmarshal([]byte(tt.body), &response); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if response.Data.JobID != "j1" {
				t.Errorf("Data.JobID = %q, want %q", response.Data.JobID, "j1")
			}
			if response.Data.ResourceId != tt.wantResourceID {
				t.Errorf("Data.ResourceId = %q, want %q", response.Data.ResourceId, tt.wantResourceID)
			}
		})
	}
}

func TestBatchDispatchReadsBothShapes(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantResourceID string
	}{
		{
			name: "legacy",
			body: `{"data":{"jobs":[{"jobId":"j1"}]}}`,
		},
		{
			name:           "v3",
			body:           `{"data":{"jobs":[{"job":{"id":"j1","resourceId":"r1"},"resource":{"id":"r1"}}]}}`,
			wantResourceID: "r1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response client.JobStatusMultiResponse
			if err := json.Unmarshal([]byte(tt.body), &response); err != nil {
				t.Fatalf("decode: %v", err)
			}

			jobID, err := client.GetJobID(&response)
			if err != nil {
				t.Fatalf("GetJobID: %v", err)
			}
			if jobID != "j1" {
				t.Errorf("GetJobID = %q, want %q", jobID, "j1")
			}

			resourceID, err := client.GetJobResourceID(&response)
			if err != nil {
				t.Fatalf("GetJobResourceID: %v", err)
			}
			if resourceID != tt.wantResourceID {
				t.Errorf("GetJobResourceID = %q, want %q", resourceID, tt.wantResourceID)
			}
		})
	}
}

// The outcome fixtures quote the bytes the API sends.
//
//nolint:misspell // Byte-exact quotes of the wire value.
const (
	pollTestV3CancelledJob = `{"id":"job-1","stage":"cancelled","outcome":"cancelled","progress":40,` +
		`"message":"Stopped by an administrator","failureReason":null,"resourceId":"vm-1"}`
	pollTestV3AbandonedCancelStageJob = `{"id":"job-1","stage":"cancelled","outcome":"abandoned","progress":40,` +
		`"message":null,"failureReason":null,"resourceId":"vm-1"}`
)

func TestPollV3Outcomes(t *testing.T) {
	const (
		runningJob = `{"id":"job-1","stage":"running","outcome":null,"progress":40,"message":"Working",` +
			`"failureReason":null,"resourceId":"vm-1"}`
		succeededJob = `{"id":"job-1","stage":"completed","outcome":"succeeded","progress":100,"message":"Done",` +
			`"failureReason":null,"resourceId":"vm-1"}`
		compensatingJob = `{"id":"job-1","stage":"compensating","outcome":null,"progress":60,"message":"Rolling back",` +
			`"failureReason":null,"resourceId":"vm-1"}`
		failedJob = `{"id":"job-1","stage":"failed","outcome":"failed","progress":60,"message":"Rolled back",` +
			`"failureReason":"No capacity in the data center","resourceId":"vm-1"}`
		abandonedFailStageJob = `{"id":"job-1","stage":"failed","outcome":"abandoned","progress":40,` +
			`"message":null,"failureReason":null,"resourceId":"vm-1"}`
		exoticJob = `{"id":"job-1","stage":"failed","outcome":"exotic","progress":40,` +
			`"message":null,"failureReason":null,"resourceId":"vm-1"}`
	)

	abandoned := fmt.Sprintf(client.ErrJobAbandoned, pollTestJobID, pollTestAction)

	tests := []struct {
		name         string
		bodies       []string
		wantIs       error
		wantText     string
		wantSuffix   string
		wantContains string
		wantCalls    int
	}{
		{
			name:      "a live job that then succeeds",
			bodies:    []string{v3JobsBody(runningJob), v3JobsBody(succeededJob)},
			wantCalls: 2,
		},
		{
			name:       "a compensating job that then fails",
			bodies:     []string{v3JobsBody(compensatingJob), v3JobsBody(failedJob)},
			wantIs:     client.ErrJobFailed,
			wantSuffix: "No capacity in the data center",
			wantCalls:  2,
		},
		{
			name:      "a job the platform stopped",
			bodies:    []string{v3JobsBody(pollTestV3CancelledJob)},
			wantIs:    client.ErrJobCancelledByPlatform,
			wantText:  fmt.Sprintf(client.ErrJobCancelled, pollTestJobID, pollTestAction),
			wantCalls: 1,
		},
		{
			name:      "an abandoned job in the cancel stage",
			bodies:    []string{v3JobsBody(pollTestV3AbandonedCancelStageJob)},
			wantIs:    client.ErrJobCancelledByPlatform,
			wantText:  abandoned,
			wantCalls: 1,
		},
		{
			name:      "an abandoned job in the failed stage",
			bodies:    []string{v3JobsBody(abandonedFailStageJob)},
			wantIs:    client.ErrJobCancelledByPlatform,
			wantText:  abandoned,
			wantCalls: 1,
		},
		{
			name:         "an outcome this provider does not know",
			bodies:       []string{v3JobsBody(exoticJob)},
			wantIs:       client.ErrJobFailed,
			wantContains: fmt.Sprintf(client.ErrDetailUnknownOutcome, "exotic"),
			wantCalls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpcnClient, calls := jobsSequenceServer(t, tt.bodies...)

			response, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, millisecondPollingConfig())

			if got := int(calls.Load()); got != tt.wantCalls {
				t.Errorf("poll calls = %d, want %d", got, tt.wantCalls)
			}
			if tt.wantIs == nil {
				if err != nil {
					t.Fatalf("expected success, got: %v", err)
				}
				if response == nil || len(response.Data.Jobs) != 1 {
					t.Fatalf("response = %+v, want the job", response)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false, want true", err, tt.wantIs)
			}
			if tt.wantText != "" && err.Error() != tt.wantText {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantText)
			}
			if tt.wantSuffix != "" && !strings.HasSuffix(err.Error(), tt.wantSuffix) {
				t.Errorf("error = %q, want it to end with %q", err.Error(), tt.wantSuffix)
			}
			if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestPollFailureDetailPrefersFailureReason(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name: "a v3 job",
			body: v3JobsBody(`{"id":"job-1","stage":"failed","outcome":"failed","message":"C",` +
				`"failureReason":"A","errorMessage":"B"}`),
			wantDetail: "A",
		},
		{
			name: "a legacy job",
			body: `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[` +
				`{"jobId":"job-1","stage":"failed","message":"C","errorMessage":"B",` +
				`"isCompleted":false,"isTerminal":true,"hasFailed":true}]}}`,
			wantDetail: "B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpcnClient, _ := jobsSequenceServer(t, tt.body)

			_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, pollTestJobID, millisecondPollingConfig())
			if err == nil {
				t.Fatal("expected a failed job to fail the poll, got nil")
			}
			want := fmt.Sprintf("job %s for action %q: %v: %s", pollTestJobID, pollTestAction, client.ErrJobFailed, tt.wantDetail)
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err.Error(), want)
			}
		})
	}
}

func TestPollRefusesEmptyJobID(t *testing.T) {
	gpcnClient, calls := jobsServer(t, v3JobsBody(`{"id":"job-1","stage":"running","outcome":null}`))

	config := &client.PollingConfig{
		Timeout:         50 * time.Millisecond,
		InitialInterval: time.Millisecond,
		MaxInterval:     time.Millisecond,
	}
	_, err := client.PerformLongPollingWithConfig(gpcnClient, t.Context(), pollTestAction, "", config)
	if !errors.Is(err, client.ErrNoJobID) {
		t.Errorf("error = %v, want ErrNoJobID", err)
	}
	if got := int(calls.Load()); got != 0 {
		t.Errorf("poll calls = %d, want 0", got)
	}
}
