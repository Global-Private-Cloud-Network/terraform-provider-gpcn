package client

import "errors"

// Polling errors
var (
	ErrLongPollingTimeout = errors.New("after polling timeout, the job status was still not completed")
	ErrJobFailed          = errors.New("job operation failed, please check parameters and retry operation")
	ErrEmptyJobsResponse  = errors.New("API returned empty jobs array in response")
)

// Configuration errors
var (
	ErrMissingHost   = errors.New("missing required configuration: host")
	ErrMissingAPIKey = errors.New("missing required configuration: api_key")
)

// HTTP errors
var (
	ErrMaxRetriesExceeded = errors.New("maximum retry attempts exceeded")
)

// Polling error formats. Each renders the whole sentence, because the job id
// and the action are what tell one stopped job from another.
const (
	//nolint:misspell // The platform spells the cancelled stage this way, and the sentence quotes it.
	ErrJobCancelled  = "job %s for action %q was cancelled by the platform"
	ErrJobNotVisible = "job %s is not visible to this API key (it does not exist, belongs to another tenant, or was deleted)"
)
