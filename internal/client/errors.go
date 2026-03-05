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
