package client

import "errors"

// Polling errors
var (
	ErrLongPollingTimeout = errors.New("after polling timeout, the job status was still not completed")
	ErrJobFailed          = errors.New("job operation failed, please check parameters and retry operation")
	ErrEmptyJobsResponse  = errors.New("API returned empty jobs array in response")
	ErrNoJobID            = errors.New("the API accepted the request but its answer carried no job ID, so the provider cannot follow the job; the change may still be running")

	// ErrJobCancelledByPlatform matches, through errors.Is, the error of a job
	// that the platform stopped.
	ErrJobCancelledByPlatform = errors.New("job stopped by the platform")
	// ErrJobNotVisibleTarget matches, through errors.Is, the error of a job
	// that the batch read does not return.
	ErrJobNotVisibleTarget = errors.New("job not visible")
	// ErrHolderStillRunning matches, through errors.Is, the error of a holder
	// job that outlived the wait.
	ErrHolderStillRunning = errors.New("a job still holds the resource")
	// ErrBusyWaitExhausted matches every error of a busy wait that ran out of
	// time, so the caller knows the API never processed the request.
	ErrBusyWaitExhausted = errors.New("the busy wait ran out")
)

// jobCancelledError keeps the stopped-job sentence and matches
// ErrJobCancelledByPlatform.
type jobCancelledError struct {
	message string
}

func (e *jobCancelledError) Error() string { return e.message }

func (e *jobCancelledError) Is(target error) bool { return target == ErrJobCancelledByPlatform }

type jobNotVisibleError struct {
	message string
}

func (e *jobNotVisibleError) Error() string { return e.message }

func (e *jobNotVisibleError) Is(target error) bool { return target == ErrJobNotVisibleTarget }

type holderStillRunningError struct {
	message string
}

func (e *holderStillRunningError) Error() string { return e.message }

func (e *holderStillRunningError) Is(target error) bool {
	return target == ErrHolderStillRunning || target == ErrBusyWaitExhausted
}

type holderEndedAtDeadlineError struct {
	message string
}

func (e *holderEndedAtDeadlineError) Error() string { return e.message }

func (e *holderEndedAtDeadlineError) Is(target error) bool { return target == ErrBusyWaitExhausted }

// Configuration errors
var (
	ErrMissingHost   = errors.New("missing required configuration: host")
	ErrMissingAPIKey = errors.New("missing required configuration: api_key")
)

// HTTP errors
var (
	ErrMaxRetriesExceeded = errors.New("maximum retry attempts exceeded")
)

// Each polling format renders the whole sentence. The job id and the action
// tell one stopped job from another.
const (
	//nolint:misspell // The platform spells the cancelled stage this way, and the sentence quotes it.
	ErrJobCancelled  = "job %s for action %q was cancelled by the platform"
	ErrJobAbandoned  = "job %s for action %q was abandoned by the platform"
	ErrJobNotVisible = "job %s is not visible to this API key (it does not exist, belongs to another tenant, or was deleted)"

	ErrDetailUnknownOutcome     = "the job ended with outcome %q, which this provider version does not recognize"
	ErrFmtHolderStillRunning    = "job %s still holds this resource after %s; run the apply or destroy again once that job ends"
	ErrFmtHolderEndedAtDeadline = "job %s ended as the wait for it ran out after %s, so the provider did not send the request; run the apply or destroy again"
)
