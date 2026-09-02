package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type JobStatusSingularResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    JobResponse `json:"data"`
}
type JobResponse struct {
	JobID        string `json:"jobId"`
	IsCompleted  bool   `json:"isCompleted"`
	HasFailed    bool   `json:"hasFailed"`
	ResourceId   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	ResourceType string `json:"resourceType"`
}
type JobStatusMultiResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    JobStatusDataResponse `json:"data"`
}
type JobStatusDataResponse struct {
	Jobs []JobResponse `json:"jobs"`
}

// PollingConfig holds configuration for long polling operations
type PollingConfig struct {
	// Timeout is the maximum time to wait for job completion
	Timeout time.Duration
	// InitialInterval is the initial polling interval
	InitialInterval time.Duration
	// MaxInterval is the maximum polling interval
	MaxInterval time.Duration
}

// DefaultPollingConfig returns sensible defaults for polling
func DefaultPollingConfig() *PollingConfig {
	return &PollingConfig{
		Timeout:         10 * time.Minute,
		InitialInterval: 1 * time.Second,
		MaxInterval:     10 * time.Second,
	}
}

// PerformLongPolling polls until job completes or times out, using exponential backoff.
// If gpcnClient has a PollingTimeout configured, it will be used; otherwise DefaultPollingConfig is used.
func PerformLongPolling(gpcnClient *GpcnClient, ctx context.Context, action, jobId string) (*JobStatusMultiResponse, error) {
	config := DefaultPollingConfig()
	if gpcnClient.config != nil && gpcnClient.config.PollingTimeout > 0 {
		config.Timeout = gpcnClient.config.PollingTimeout
	}
	return PerformLongPollingWithConfig(gpcnClient, ctx, action, jobId, config)
}

// PerformLongPollingWithConfig polls with custom configuration.
//
// On an error the returned response is the last job status that the poll read,
// or nil if no poll completed. Use it only to identify a resource that the
// operation may have created; see PartialCreateFromPoll.
func PerformLongPollingWithConfig(gpcnClient *GpcnClient, ctx context.Context, action, jobId string, config *PollingConfig) (*JobStatusMultiResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingPerformLongPollingWithAction, action),
		map[string]any{"job_id": jobId})

	startTime := time.Now()
	interval := config.InitialInterval
	iteration := 1

	// The API assigns a resource ID before the job completes. Keep the last
	// status that named one: if the run is interrupted, that ID is the only
	// record of a resource the API already created.
	var lastResponse *JobStatusMultiResponse

	// A check on the elapsed time limits only the interval between polls. A poll
	// that is in progress continues for its request timeout and all its retries.
	// With the default configuration this adds approximately 4 minutes.
	pollCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	// asTimeout changes an error that the deadline of pollCtx caused into
	// ErrLongPollingTimeout. All three conditions are necessary:
	//
	//   - the caller did not cancel, so an interrupted run keeps context.Canceled;
	//   - pollCtx expired, so a per-request timeout, which also wraps
	//     context.DeadlineExceeded, stays a request timeout;
	//   - the error came from a deadline, so an unrelated failure that arrives
	//     after the deadline keeps its own cause.
	asTimeout := func(err error) error {
		if ctx.Err() == nil && errors.Is(pollCtx.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("job %s for action %q: %w (elapsed: %s)", jobId, action, ErrLongPollingTimeout, time.Since(startTime))
		}
		return err
	}

	for {
		elapsed := time.Since(startTime)
		tflog.Info(ctx, fmt.Sprintf(LogStartingLongPollingIteration, iteration, action, int(elapsed.Seconds())),
			map[string]any{"job_id": jobId, "iteration": iteration})

		jobResponse, err := poll(gpcnClient, pollCtx, jobId)
		if err != nil {
			return lastResponse, asTimeout(fmt.Errorf("polling for job %s failed: %w", jobId, err))
		}

		// Bounds check before accessing Jobs array
		if len(jobResponse.Data.Jobs) == 0 {
			return lastResponse, fmt.Errorf("polling for job %s: %w", jobId, ErrEmptyJobsResponse)
		}

		job := jobResponse.Data.Jobs[0]
		if job.ResourceId != "" {
			lastResponse = jobResponse
		}

		if job.HasFailed {
			return lastResponse, fmt.Errorf("job %s for action %q: %w", jobId, action, ErrJobFailed)
		}

		if job.IsCompleted {
			tflog.Info(ctx, fmt.Sprintf(LogLongPollingCompletedSuccessfully, action),
				map[string]any{"job_id": jobId, "elapsed_seconds": int(elapsed.Seconds())})
			return jobResponse, nil
		}

		// pollCtx has the deadline. A limit on the interval is not necessary.
		if err := SleepWithContext(pollCtx, interval); err != nil {
			return lastResponse, asTimeout(fmt.Errorf("polling for job %s for action %q: %w", jobId, action, err))
		}

		interval *= 2
		if interval > config.MaxInterval {
			interval = config.MaxInterval
		}
		iteration++
	}
}

func poll(gpcnClient *GpcnClient, ctx context.Context, jobId string) (*JobStatusMultiResponse, error) {
	jobStatusRequestBody := map[string][]string{
		"jobIds": {jobId},
	}

	jsonJobStatusRequestBody, err := json.Marshal(jobStatusRequestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job status request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, "POST", JOBS_BASE_URL_V1, bytes.NewBuffer(jsonJobStatusRequestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create job status request: %w", err)
	}

	// Use DoWithRetry to propagate correlation IDs and benefit from retry logic
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, fmt.Errorf("job status request failed: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			tflog.Warn(ctx, "failed to close response body", map[string]any{"error": closeErr.Error()})
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read job status response: %w", err)
	}

	var jobStatusMultiResponse JobStatusMultiResponse
	if err := json.Unmarshal(body, &jobStatusMultiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse job status response: %w", err)
	}

	return &jobStatusMultiResponse, nil
}

// GetJobResourceID safely extracts the resource ID from a job response with bounds checking.
//
// An empty ID is an error. A caller builds a URL from this value, and the base
// URL alone addresses the collection, so an empty ID reads every resource
// instead of one. A caller also puts this value in a PartialCreateError, where
// an empty ID gives the operator no way to find the resource.
func GetJobResourceID(resp *JobStatusMultiResponse) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("job response is nil")
	}
	if len(resp.Data.Jobs) == 0 {
		return "", ErrEmptyJobsResponse
	}
	if resp.Data.Jobs[0].ResourceId == "" {
		return "", fmt.Errorf("job %s: %w", resp.Data.Jobs[0].JobID, ErrEmptyResourceID)
	}
	return resp.Data.Jobs[0].ResourceId, nil
}

// GetJobID safely extracts the job ID from a multi-response with bounds checking
func GetJobID(resp *JobStatusMultiResponse) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("job response is nil")
	}
	if len(resp.Data.Jobs) == 0 {
		return "", ErrEmptyJobsResponse
	}
	return resp.Data.Jobs[0].JobID, nil
}
