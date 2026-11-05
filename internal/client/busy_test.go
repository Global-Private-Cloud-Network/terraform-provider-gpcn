package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const busyTestBody = `{"success":false,"message":"web-01 is busy: delete in progress.","error":{"code":"RESOURCE_BUSY",` +
	`"statusCode":409,"details":{"resourceType":"vpc_vpn_connection","resourceId":"c1",` +
	`"heldBy":{"jobId":"h1","operation":"delete","verb":"delete"}}}}`

func busyTestError(status int, body string) error {
	return newHTTPError(&http.Response{StatusCode: status, Header: http.Header{}}, []byte(body))
}

// holderServer answers every batch read with the given status and body, and it
// counts the calls.
func holderServer(t *testing.T, status int, body string) (*GpcnClient, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != JOBS_BASE_URL_V1 {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	cfg := DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 0
	cfg.InitialRetryDelay = 0
	gpcnClient, err := NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return gpcnClient, &calls
}

// fastHolderPolling replaces holderPollConfig for one test.
func fastHolderPolling(t *testing.T) {
	t.Helper()

	previous := holderPollConfig
	holderPollConfig = func() *PollingConfig {
		return &PollingConfig{InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}
	}
	t.Cleanup(func() { holderPollConfig = previous })
}

func holderJobsBody(job string) string {
	return `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[` + job + `],"notFound":[]}}`
}

func TestBusyHolderReadsResourceBusy(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		wantID string
		wantOK bool
	}{
		{
			name:   "the served RESOURCE_BUSY body",
			err:    busyTestError(http.StatusConflict, busyTestBody),
			wantID: "h1",
			wantOK: true,
		},
		{
			name: "the same body at 400",
			err:  busyTestError(http.StatusBadRequest, busyTestBody),
		},
		{
			name: "another 409 code",
			err:  busyTestError(http.StatusConflict, strings.Replace(busyTestBody, "RESOURCE_BUSY", "VPN_STATE_CONFLICT", 1)),
		},
		{
			name: "no heldBy",
			err: busyTestError(http.StatusConflict, `{"success":false,"message":"busy","error":{"code":"RESOURCE_BUSY",`+
				`"statusCode":409,"details":{"resourceType":"vpc_vpn_connection","resourceId":"c1"}}}`),
		},
		{
			name: "an empty holder job id",
			err:  busyTestError(http.StatusConflict, strings.Replace(busyTestBody, `"jobId":"h1"`, `"jobId":""`, 1)),
		},
		{
			name: "an error that is not an HTTP error",
			err:  errors.New("connection reset"),
		},
		{
			name:   "a wrapped RESOURCE_BUSY error",
			err:    fmt.Errorf("delete failed: %w", busyTestError(http.StatusConflict, busyTestBody)),
			wantID: "h1",
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := BusyHolder(tt.err)
			if id != tt.wantID || ok != tt.wantOK {
				t.Errorf("BusyHolder = (%q, %t), want (%q, %t)", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestActiveJobIDPrefersHeldBy(t *testing.T) {
	held := "held-job"
	active := "active-job"
	empty := ""

	tests := []struct {
		name        string
		heldBy      *HeldBy
		activeJobID *string
		want        *string
	}{
		{name: "heldBy and activeJobId", heldBy: &HeldBy{JobID: held}, activeJobID: &active, want: &held},
		{name: "activeJobId only", activeJobID: &active, want: &active},
		{name: "heldBy with an empty job id", heldBy: &HeldBy{JobID: ""}, activeJobID: &active, want: &active},
		{name: "neither"},
		{name: "an empty activeJobId", activeJobID: &empty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ActiveJobID(tt.heldBy, tt.activeJobID)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("ActiveJobID = %q, want nil", *got)
			case tt.want != nil && got == nil:
				t.Errorf("ActiveJobID = nil, want %q", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("ActiveJobID = %q, want %q", *got, *tt.want)
			}
		})
	}
}

// The holder fixtures quote the bytes the API sends.
//
//nolint:misspell // Byte-exact quotes of the wire value.
const holderTestCancelledJob = `{"id":"h1","stage":"cancelled","outcome":"cancelled","progress":40,"message":null,"failureReason":null}`

func TestAwaitHolderReturnsNilOnTerminalAnswers(t *testing.T) {
	fastHolderPolling(t)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "succeeded",
			body: holderJobsBody(`{"id":"h1","stage":"completed","outcome":"succeeded","progress":100,"message":null,"failureReason":null}`),
		},
		{
			name: "failed",
			body: holderJobsBody(`{"id":"h1","stage":"failed","outcome":"failed","progress":40,"message":null,"failureReason":"capacity"}`),
		},
		{
			name: "stopped by the platform",
			body: holderJobsBody(holderTestCancelledJob),
		},
		{
			name: "abandoned",
			body: holderJobsBody(`{"id":"h1","stage":"failed","outcome":"abandoned","progress":40,"message":null,"failureReason":null}`),
		},
		{
			name: "a legacy failed job",
			body: `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[{"jobId":"h1",` +
				`"stage":"failed","errorMessage":"capacity","isCompleted":false,"isTerminal":true,"hasFailed":true}]}}`,
		},
		{
			name: "a job the read does not return",
			body: `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[],"notFound":["h1"]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpcnClient, calls := holderServer(t, http.StatusOK, tt.body)

			if err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(5*time.Second)); err != nil {
				t.Errorf("AwaitHolder = %v, want nil", err)
			}
			if got := int(calls.Load()); got != 1 {
				t.Errorf("poll calls = %d, want 1", got)
			}
		})
	}
}

func TestAwaitHolderStopsAtDeadline(t *testing.T) {
	fastHolderPolling(t)

	t.Run("the deadline has passed", func(t *testing.T) {
		gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

		err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(-time.Second))
		if !errors.Is(err, ErrHolderStillRunning) {
			t.Errorf("AwaitHolder = %v, want ErrHolderStillRunning", err)
		}
		if got := int(calls.Load()); got != 0 {
			t.Errorf("poll calls = %d, want 0", got)
		}
	})

	t.Run("the holder outlives the deadline", func(t *testing.T) {
		gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

		err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(50*time.Millisecond))
		if !errors.Is(err, ErrHolderStillRunning) {
			t.Fatalf("AwaitHolder = %v, want ErrHolderStillRunning", err)
		}
		if want := "job h1 still holds this resource after "; !strings.HasPrefix(err.Error(), want) {
			t.Errorf("error = %q, want it to start with %q", err.Error(), want)
		}
		if got := int(calls.Load()); got < 2 {
			t.Errorf("poll calls = %d, want at least 2", got)
		}
	})
}

func TestAwaitHolderReturnsOtherErrors(t *testing.T) {
	fastHolderPolling(t)

	gpcnClient, _ := holderServer(t, http.StatusBadRequest,
		`{"success":false,"message":"jobIds must be uuids","error":{"code":"VALIDATION_ERROR","statusCode":400,"details":null}}`)

	err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(5*time.Second))
	if err == nil {
		t.Fatal("AwaitHolder = nil, want the read error")
	}
	if errors.Is(err, ErrHolderStillRunning) {
		t.Errorf("AwaitHolder = %v, want an error that is not ErrHolderStillRunning", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusBadRequest {
		t.Errorf("AwaitHolder = %v, want the 400 from the read", err)
	}
}

// The elapsed time in the deadline error counts from the start of the
// caller's wait, not from the call to AwaitHolder.
func TestAwaitHolderReportsElapsedFromWaitStart(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

	start := time.Now().Add(-90 * time.Second)
	err := AwaitHolder(gpcnClient, t.Context(), "h1", start, time.Now().Add(-time.Second))
	if !errors.Is(err, ErrHolderStillRunning) {
		t.Fatalf("AwaitHolder = %v, want ErrHolderStillRunning", err)
	}
	if want := "after 1m30s"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
	if got := int(calls.Load()); got != 0 {
		t.Errorf("poll calls = %d, want 0", got)
	}
}

// The poll reaches the deadline while the holder still runs. The elapsed time
// still counts from the start of the caller's wait.
func TestAwaitHolderReportsElapsedFromWaitStartMidPoll(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

	start := time.Now().Add(-90 * time.Second)
	err := AwaitHolder(gpcnClient, t.Context(), "h1", start, time.Now().Add(50*time.Millisecond))
	if !errors.Is(err, ErrHolderStillRunning) {
		t.Fatalf("AwaitHolder = %v, want ErrHolderStillRunning", err)
	}
	if want := "after 1m30s"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
	if got := int(calls.Load()); got < 2 {
		t.Errorf("poll calls = %d, want at least 2", got)
	}
}
