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
)

// jobCancelledError keeps the stopped-job sentence and matches
// ErrJobCancelledByPlatform.
type jobCancelledError struct {
	message string
}

func (e *jobCancelledError) Error() string { return e.message }

func (e *jobCancelledError) Is(target error) bool { return target == ErrJobCancelledByPlatform }

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

	ErrDetailUnknownOutcome = "the job ended with outcome %q, which this provider version does not recognize"
)
