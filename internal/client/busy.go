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

func ActiveJobID(heldBy *HeldBy) *string {
	if heldBy != nil && heldBy.JobID != "" {
		return &heldBy.JobID
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

type HolderOutcome int

const (
	HolderEnded HolderOutcome = iota
	HolderNotVisible
)

// AwaitHolder returns a nil error when the holder job ends or is not visible.
// The caller then sends its request again. start is the start of the caller's
// wait, the time its deadline counts from. So the deadline error reports how
// long the caller waited.
func AwaitHolder(gpcnClient *GpcnClient, ctx context.Context, jobID string, start, deadline time.Time) (HolderOutcome, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return HolderEnded, newHolderStillRunningError(jobID, time.Since(start))
	}

	config := holderPollConfig()
	config.Timeout = remaining
	_, err := PerformLongPollingWithConfig(gpcnClient, ctx, ActionAwaitHolder, jobID, config)
	switch {
	case err == nil,
		errors.Is(err, ErrJobFailed),
		errors.Is(err, ErrJobCancelledByPlatform):
		return HolderEnded, nil
	case errors.Is(err, ErrJobNotVisibleTarget):
		return HolderNotVisible, nil
	case errors.Is(err, ErrLongPollingTimeout):
		return HolderEnded, newHolderStillRunningError(jobID, time.Since(start))
	default:
		return HolderEnded, BusyWaitEnded(err)
	}
}

// BusyWaitEnded marks cause as the end of a busy wait whose last send was
// refused.
func BusyWaitEnded(cause error) error {
	return &busyWaitError{cause: cause}
}

func HolderDeadline(outcome HolderOutcome, jobID string, start time.Time) error {
	if outcome == HolderNotVisible {
		return HolderNotVisibleAtDeadline(jobID, start)
	}
	return HolderEndedAtDeadline(jobID, start)
}

// HolderEndedAtDeadline reports a holder job that ended. The wait ran out before
// the caller could send the request again.
func HolderEndedAtDeadline(jobID string, start time.Time) error {
	return &holderEndedAtDeadlineError{message: fmt.Sprintf(ErrFmtHolderEndedAtDeadline, jobID, time.Since(start).Round(time.Second))}
}

func HolderNotVisibleAtDeadline(jobID string, start time.Time) error {
	return &holderNotVisibleAtDeadlineError{message: fmt.Sprintf(ErrFmtHolderNotVisibleAtDeadline, jobID, time.Since(start).Round(time.Second))}
}

func newHolderStillRunningError(jobID string, elapsed time.Duration) error {
	return &holderStillRunningError{message: fmt.Sprintf(ErrFmtHolderStillRunning, jobID, elapsed.Round(time.Second))}
}
