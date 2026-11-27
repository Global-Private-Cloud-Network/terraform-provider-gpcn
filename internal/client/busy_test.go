package client

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func TestActiveJobIDReadsTheHolder(t *testing.T) {
	held := "held-job"

	tests := []struct {
		name   string
		heldBy *HeldBy
		want   *string
	}{
		{name: "a holder with a job id", heldBy: &HeldBy{JobID: held}, want: &held},
		{name: "a holder with an empty job id", heldBy: &HeldBy{JobID: ""}},
		{name: "no holder"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ActiveJobID(tt.heldBy)
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
		want HolderOutcome
	}{
		{
			name: "succeeded",
			body: holderJobsBody(`{"id":"h1","stage":"completed","outcome":"succeeded","progress":100,"message":null,"failureReason":null}`),
			want: HolderEnded,
		},
		{
			name: "failed",
			body: holderJobsBody(`{"id":"h1","stage":"failed","outcome":"failed","progress":40,"message":null,"failureReason":"capacity"}`),
			want: HolderEnded,
		},
		{
			name: "stopped by the platform",
			body: holderJobsBody(holderTestCancelledJob),
			want: HolderEnded,
		},
		{
			name: "abandoned",
			body: holderJobsBody(`{"id":"h1","stage":"failed","outcome":"abandoned","progress":40,"message":null,"failureReason":null}`),
			want: HolderEnded,
		},
		{
			name: "a job the read does not return",
			body: `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[],"notFound":["h1"]}}`,
			want: HolderNotVisible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpcnClient, calls := holderServer(t, http.StatusOK, tt.body)

			outcome, err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(5*time.Second))
			if err != nil {
				t.Errorf("AwaitHolder = %v, want nil", err)
			}
			if outcome != tt.want {
				t.Errorf("outcome = %d, want %d", outcome, tt.want)
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

		_, err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(-time.Second))
		if !errors.Is(err, ErrHolderStillRunning) {
			t.Errorf("AwaitHolder = %v, want ErrHolderStillRunning", err)
		}
		if got := int(calls.Load()); got != 0 {
			t.Errorf("poll calls = %d, want 0", got)
		}
	})

	t.Run("the holder outlives the deadline", func(t *testing.T) {
		gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

		_, err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(50*time.Millisecond))
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

	_, err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(5*time.Second))
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
	if !errors.Is(err, ErrBusyWaitExhausted) {
		t.Errorf("errors.Is(err, ErrBusyWaitExhausted) = false, want true")
	}
}

// The elapsed time in the deadline error counts from the start of the
// caller's wait, not from the call to AwaitHolder.
func TestAwaitHolderReportsElapsedFromWaitStart(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

	start := time.Now().Add(-90 * time.Second)
	_, err := AwaitHolder(gpcnClient, t.Context(), "h1", start, time.Now().Add(-time.Second))
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
	_, err := AwaitHolder(gpcnClient, t.Context(), "h1", start, time.Now().Add(50*time.Millisecond))
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

func TestHolderEndedAtDeadlineNamesTheEndedJob(t *testing.T) {
	err := HolderEndedAtDeadline("h1", time.Now().Add(-90*time.Second))
	want := "job h1 ended as the wait for it ran out after 1m30s, so the provider did not send the request again; run the apply or destroy again"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrBusyWaitExhausted) {
		t.Errorf("errors.Is(err, ErrBusyWaitExhausted) = false, want true")
	}
	if errors.Is(err, ErrHolderStillRunning) {
		t.Errorf("errors.Is(err, ErrHolderStillRunning) = true, want false")
	}
}

func TestHolderNotVisibleAtDeadlineNamesTheJob(t *testing.T) {
	err := HolderNotVisibleAtDeadline("h1", time.Now().Add(-90*time.Second))
	want := "the wait for job h1 ran out after 1m30s, and the job read does not return that job to this API key; run the apply or destroy again later"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrHolderNotVisible) {
		t.Errorf("errors.Is(err, ErrHolderNotVisible) = false, want true")
	}
	if !errors.Is(err, ErrBusyWaitExhausted) {
		t.Errorf("errors.Is(err, ErrBusyWaitExhausted) = false, want true")
	}
	if errors.Is(err, ErrHolderStillRunning) {
		t.Errorf("errors.Is(err, ErrHolderStillRunning) = true, want false")
	}
}

func TestHolderStillRunningIsABusyWaitExhaustion(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, _ := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))

	_, err := AwaitHolder(gpcnClient, t.Context(), "h1", time.Now(), time.Now().Add(-time.Second))
	if !errors.Is(err, ErrHolderStillRunning) {
		t.Fatalf("AwaitHolder = %v, want ErrHolderStillRunning", err)
	}
	if !errors.Is(err, ErrBusyWaitExhausted) {
		t.Errorf("errors.Is(err, ErrBusyWaitExhausted) = false, want true")
	}
}

const holderTestSucceededJob = `{"id":"h1","stage":"completed","outcome":"succeeded","progress":100,"message":null,"failureReason":null}`

const holderTestNotFoundBody = `{"success":true,"message":"Job progress retrieved successfully","data":{"jobs":[],"notFound":["h1"]}}`

// busyThenAccepted answers busy to the first send and accepts the rest.
func busyThenAccepted(sends *int) func() (string, error) {
	return func() (string, error) {
		*sends++
		if *sends == 1 {
			return "", busyTestError(http.StatusConflict, busyTestBody)
		}
		return "accepted", nil
	}
}

func TestSendWaitingForHolderSendsAgainAfterTheHolder(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(holderTestSucceededJob))

	sends := 0
	result, err := SendWaitingForHolder(gpcnClient, t.Context(), time.Millisecond, busyThenAccepted(&sends))
	if err != nil {
		t.Fatalf("SendWaitingForHolder = %v, want nil", err)
	}
	if result != "accepted" {
		t.Errorf("result = %q, want %q", result, "accepted")
	}
	if sends != 2 {
		t.Errorf("sends = %d, want 2", sends)
	}
	if got := int(calls.Load()); got != 1 {
		t.Errorf("poll calls = %d, want 1", got)
	}
}

func TestSendWaitingForHolderReportsAHolderThatOutlivesTheDeadline(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, _ := holderServer(t, http.StatusOK, holderJobsBody(`{"id":"h1","stage":"running","outcome":null}`))
	gpcnClient.config.PollingTimeout = 50 * time.Millisecond

	sends := 0
	_, err := SendWaitingForHolder(gpcnClient, t.Context(), time.Millisecond, busyThenAccepted(&sends))
	if !errors.Is(err, ErrHolderStillRunning) {
		t.Errorf("SendWaitingForHolder = %v, want ErrHolderStillRunning", err)
	}
	if sends != 1 {
		t.Errorf("sends = %d, want 1", sends)
	}
}

// The holder answer arrives after the deadline, so the deadline always passes
// between the two sends.
func TestSendWaitingForHolderStopsAtADeadlineThatPassedAfterTheHolder(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantPrefix     string
		wantNotVisible bool
	}{
		{
			name:       "the holder ended",
			body:       holderJobsBody(holderTestSucceededJob),
			wantPrefix: "job h1 ended as the wait for it ran out after ",
		},
		{
			name:           "the holder is not visible",
			body:           holderTestNotFoundBody,
			wantPrefix:     "the wait for job h1 ran out after ",
			wantNotVisible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fastHolderPolling(t)

			const pollingTimeout = 500 * time.Millisecond
			sentAt := make(chan time.Time, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(time.Until((<-sentAt).Add(pollingTimeout)))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			cfg := DefaultConfig(server.URL, "test-key")
			cfg.MaxRetries = 0
			cfg.PollingTimeout = pollingTimeout
			gpcnClient, err := NewGpcnClient(cfg)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			sends := 0
			send := busyThenAccepted(&sends)
			_, err = SendWaitingForHolder(gpcnClient, t.Context(), time.Millisecond, func() (string, error) {
				if sends == 0 {
					sentAt <- time.Now()
				}
				return send()
			})
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantPrefix) {
				t.Fatalf("SendWaitingForHolder = %v, want it to start with %q", err, tt.wantPrefix)
			}
			if got := errors.Is(err, ErrHolderNotVisible); got != tt.wantNotVisible {
				t.Errorf("errors.Is(err, ErrHolderNotVisible) = %t, want %t", got, tt.wantNotVisible)
			}
			if sends != 1 {
				t.Errorf("sends = %d, want 1", sends)
			}
		})
	}
}

func TestSendWaitingForHolderStopsWhenTheContextEndsInTheSleep(t *testing.T) {
	fastHolderPolling(t)
	gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(holderTestSucceededJob))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for calls.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	sends := 0
	_, err := SendWaitingForHolder(gpcnClient, ctx, time.Hour, busyThenAccepted(&sends))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("SendWaitingForHolder = %v, want context.Canceled", err)
	}
	if !errors.Is(err, ErrBusyWaitExhausted) {
		t.Errorf("SendWaitingForHolder = %v, want ErrBusyWaitExhausted", err)
	}
	if sends != 1 {
		t.Errorf("sends = %d, want 1", sends)
	}
}

func TestSendWaitingForHolderReturnsOtherRefusalsAtOnce(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "another 409 code",
			err:  busyTestError(http.StatusConflict, strings.Replace(busyTestBody, "RESOURCE_BUSY", "L2_SEGMENT_IN_USE", 1)),
		},
		{
			name: "a busy 409 without a holder job id",
			err:  busyTestError(http.StatusConflict, strings.Replace(busyTestBody, `"jobId":"h1",`, "", 1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpcnClient, calls := holderServer(t, http.StatusOK, holderJobsBody(holderTestSucceededJob))

			sends := 0
			result, err := SendWaitingForHolder(gpcnClient, t.Context(), time.Millisecond, func() (string, error) {
				sends++
				return "refused", tt.err
			})
			//nolint:errorlint // The error must come back unchanged.
			if err != tt.err {
				t.Errorf("SendWaitingForHolder = %v, want the error of the send", err)
			}
			if result != "refused" {
				t.Errorf("result = %q, want the result of the send", result)
			}
			if sends != 1 {
				t.Errorf("sends = %d, want 1", sends)
			}
			if got := int(calls.Load()); got != 0 {
				t.Errorf("poll calls = %d, want 0", got)
			}
		})
	}
}

func TestSendWaitingForHolder(t *testing.T) {
	t.Run("ends a chain of holders at one deadline", func(t *testing.T) {
		fastHolderPolling(t)

		// A deadline that starts again for the second holder ends at about 4.5s.
		const pollingTimeout = 3 * time.Second
		const firstHolderEnds = 1500 * time.Millisecond

		start := time.Now()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(string(raw), "h2") {
				_, _ = w.Write([]byte(holderJobsBody(`{"id":"h2","stage":"running","outcome":null}`)))
				return
			}
			time.Sleep(time.Until(start.Add(firstHolderEnds)))
			_, _ = w.Write([]byte(holderJobsBody(holderTestSucceededJob)))
		}))
		t.Cleanup(server.Close)

		cfg := DefaultConfig(server.URL, "test-key")
		cfg.MaxRetries = 0
		cfg.PollingTimeout = pollingTimeout
		gpcnClient, err := NewGpcnClient(cfg)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		sends := 0
		_, err = SendWaitingForHolder(gpcnClient, t.Context(), time.Millisecond, func() (string, error) {
			sends++
			body := busyTestBody
			if sends > 1 {
				body = strings.Replace(body, `"jobId":"h1"`, `"jobId":"h2"`, 1)
			}
			return "", busyTestError(http.StatusConflict, body)
		})
		elapsed := time.Since(start)
		if !errors.Is(err, ErrHolderStillRunning) || !strings.HasPrefix(err.Error(), "job h2 still holds this resource after ") {
			t.Fatalf("SendWaitingForHolder = %v, want the still-running error of h2", err)
		}
		// The error counts from the first send, and it rounds to a second.
		if !strings.Contains(err.Error(), " after 3s;") && !strings.Contains(err.Error(), " after 4s;") {
			t.Errorf("error = %q, want an elapsed time of about 3s", err.Error())
		}
		if elapsed >= 4*time.Second {
			t.Errorf("elapsed = %s, want less than 4s", elapsed)
		}
		if sends != 2 {
			t.Errorf("sends = %d, want 2", sends)
		}
	})
}
