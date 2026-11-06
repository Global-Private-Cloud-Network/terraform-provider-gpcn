package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// HeldBy names the job that holds a resource, as a resource read reports it.
type HeldBy struct {
	JobID     string `json:"jobId"`
	Operation string `json:"operation"`
	Verb      string `json:"verb"`
}

const ActionAwaitHolder = "Wait for the job holding the resource"

// A test replaces holderPollConfig to shorten the poll intervals.
var holderPollConfig = DefaultPollingConfig

// A row names the job that holds it as heldBy or as activeJobId. heldBy wins.
func ActiveJobID(heldBy *HeldBy, activeJobID *string) *string {
	if heldBy != nil && heldBy.JobID != "" {
		return &heldBy.JobID
	}
	if activeJobID != nil && *activeJobID != "" {
		return activeJobID
	}
	return nil
}

// BusyHolder returns the holder's job id when err is a 409 RESOURCE_BUSY.
func BusyHolder(err error) (string, bool) {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return "", false
	}
	if httpErr.StatusCode != http.StatusConflict || httpErr.Code != CodeResourceBusy {
		return "", false
	}
	heldBy, ok := httpErr.Details["heldBy"].(map[string]any)
	if !ok {
		return "", false
	}
	jobID, ok := heldBy["jobId"].(string)
	if !ok || jobID == "" {
		return "", false
	}
	return jobID, true
}

// AwaitHolder returns nil on every end of the holder job, because the caller
// then sends its request again. Every error other than the deadline returns
// unchanged. start is the start of the caller's wait, the time its deadline
// counts from, so the deadline error reports how long the caller waited.
func AwaitHolder(gpcnClient *GpcnClient, ctx context.Context, jobID string, start, deadline time.Time) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return newHolderStillRunningError(jobID, time.Since(start))
	}

	config := holderPollConfig()
	config.Timeout = remaining
	_, err := PerformLongPollingWithConfig(gpcnClient, ctx, ActionAwaitHolder, jobID, config)
	switch {
	case err == nil,
		errors.Is(err, ErrJobFailed),
		errors.Is(err, ErrJobCancelledByPlatform),
		errors.Is(err, ErrJobNotVisibleTarget):
		return nil
	case errors.Is(err, ErrLongPollingTimeout):
		return newHolderStillRunningError(jobID, time.Since(start))
	default:
		return err
	}
}

// HolderEndedAtDeadline reports a holder job that ended, but only after the
// wait ran out, so the caller sent nothing.
func HolderEndedAtDeadline(jobID string, start time.Time) error {
	return &holderEndedAtDeadlineError{message: fmt.Sprintf(ErrFmtHolderEndedAtDeadline, jobID, time.Since(start).Round(time.Second))}
}

func newHolderStillRunningError(jobID string, elapsed time.Duration) error {
	return &holderStillRunningError{message: fmt.Sprintf(ErrFmtHolderStillRunning, jobID, elapsed.Round(time.Second))}
}
