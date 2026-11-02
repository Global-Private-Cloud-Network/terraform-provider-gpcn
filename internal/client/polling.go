package client

import (
	"bytes"
	"context"
	"encoding/json"
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
	JobID       string `json:"jobId"`
	Stage       string `json:"stage"`
	IsCompleted bool   `json:"isCompleted"`
	IsTerminal  bool   `json:"isTerminal"`
	HasFailed   bool   `json:"hasFailed"`
	// A job percentage decodes as a float, so a platform that starts sending
	// fractions does not break the poll. An integer field rejects the whole
	// envelope.
	ProgressPercentage float64 `json:"progressPercentage"`
	Message            string  `json:"message"`
	ErrorMessage       string  `json:"errorMessage"`
	ResourceId         string  `json:"resourceId"`
	ResourceName       string  `json:"resourceName"`
	ResourceType       string  `json:"resourceType"`

	// The fields below have no legacy key, so an encoded JobResponse keeps
	// the legacy keys only. UnmarshalJSON fills them.

	// Outcome stays nil while the job runs. A terminal job sets it.
	Outcome       *string `json:"-"`
	FailureReason string  `json:"-"`
	// Resource is the row an accepted answer carries beside its job.
	Resource json.RawMessage `json:"-"`
}

// jobResponseFields has no methods, so a decode into it does not call
// UnmarshalJSON again.
type jobResponseFields JobResponse

// A pointer tells an absent or null key from a zero value.
type jobResponseKeys struct {
	ID            *string  `json:"id"`
	Outcome       *string  `json:"outcome"`
	FailureReason *string  `json:"failureReason"`
	Progress      *float64 `json:"progress"`
}

// UnmarshalJSON reads both job shapes the API sends. The id is "id" or
// "jobId", and the progress is "progress" or "progressPercentage". An accepted
// answer can wrap the job as {"job": <job>, "resource": <row>}. ResourceId
// then falls back to the row's id.
func (j *JobResponse) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		return nil
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err == nil {
		if inner, ok := keys["job"]; ok && isJSONObject(inner) {
			if err := j.decodeFlatJob(inner); err != nil {
				return err
			}
			if resource, ok := keys["resource"]; ok && !isJSONNull(resource) {
				j.Resource = resource
			}
			j.fillResourceIDFromResource()
			return nil
		}
	}

	if err := j.decodeFlatJob(data); err != nil {
		return err
	}
	j.fillResourceIDFromResource()
	return nil
}

// The keys "id" and "progress" win over "jobId" and "progressPercentage".
func (j *JobResponse) decodeFlatJob(data []byte) error {
	if err := json.Unmarshal(data, (*jobResponseFields)(j)); err != nil {
		return err
	}

	var extra jobResponseKeys
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	if extra.ID != nil && *extra.ID != "" {
		j.JobID = *extra.ID
	}
	if extra.Progress != nil {
		j.ProgressPercentage = *extra.Progress
	}
	j.Outcome = extra.Outcome
	j.FailureReason = derefString(extra.FailureReason)
	return nil
}

// The job's own resourceId wins. A row id that is not a string is ignored.
func (j *JobResponse) fillResourceIDFromResource() {
	if j.ResourceId != "" || j.Resource == nil {
		return
	}
	var row map[string]any
	if err := json.Unmarshal(j.Resource, &row); err != nil {
		return
	}
	if id, ok := row["id"].(string); ok {
		j.ResourceId = id
	}
}

func isJSONNull(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func isJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// JobStageCancelled is the stage an operator cancel leaves a job in. It is
// terminal, and it reports neither completion nor failure.
//
//nolint:misspell // The value is the byte the API sends, not prose.
const JobStageCancelled = "cancelled"

const (
	jobOutcomeSucceeded = "succeeded"
	jobOutcomeFailed    = "failed"
	//nolint:misspell // The value is the byte the API sends, not prose.
	jobOutcomeCancelled = "cancelled"
	jobOutcomeAbandoned = "abandoned"
)

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

// PerformLongPollingWithConfig polls with custom configuration
func PerformLongPollingWithConfig(gpcnClient *GpcnClient, ctx context.Context, action, jobId string, config *PollingConfig) (*JobStatusMultiResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingPerformLongPollingWithAction, action),
		map[string]any{"job_id": jobId})

	if jobId == "" {
		return nil, ErrNoJobID
	}

	startTime := time.Now()
	interval := config.InitialInterval
	iteration := 1

	for {
		elapsed := time.Since(startTime)
		tflog.Info(ctx, fmt.Sprintf(LogStartingLongPollingIteration, iteration, action, int(elapsed.Seconds())),
			map[string]any{"job_id": jobId, "iteration": iteration})

		jobResponse, err := poll(gpcnClient, ctx, jobId)
		if err != nil {
			return nil, fmt.Errorf("polling for job %s failed: %w", jobId, err)
		}

		// The API omits a job that does not exist or belongs to another tenant.
		// An empty array is therefore permanent, and waiting cannot change it.
		if len(jobResponse.Data.Jobs) == 0 {
			return nil, fmt.Errorf(ErrJobNotVisible, jobId)
		}

		job := jobResponse.Data.Jobs[0]

		tflog.Debug(ctx, fmt.Sprintf(LogLongPollingJobProgress, action),
			map[string]any{
				"job_id":              jobId,
				"iteration":           iteration,
				"stage":               job.Stage,
				"progress_percentage": job.ProgressPercentage,
				"message":             job.Message,
			})

		// The outcome alone says how such a job ended. Its stage can disagree,
		// and it carries no booleans.
		if job.Outcome != nil {
			switch *job.Outcome {
			case jobOutcomeSucceeded:
				tflog.Info(ctx, fmt.Sprintf(LogLongPollingCompletedSuccessfully, action),
					map[string]any{"job_id": jobId, "elapsed_seconds": int(elapsed.Seconds())})
				return jobResponse, nil
			case jobOutcomeFailed:
				if detail := jobFailureDetail(job, jobResponse.Message); detail != "" {
					return nil, fmt.Errorf("job %s for action %q: %w: %s", jobId, action, ErrJobFailed, detail)
				}
				return nil, fmt.Errorf("job %s for action %q: %w", jobId, action, ErrJobFailed)
			case jobOutcomeCancelled:
				return nil, &jobCancelledError{message: fmt.Sprintf(ErrJobCancelled, jobId, action)}
			case jobOutcomeAbandoned:
				return nil, &jobCancelledError{message: fmt.Sprintf(ErrJobAbandoned, jobId, action)}
			default:
				return nil, fmt.Errorf("job %s for action %q: %w: %s", jobId, action, ErrJobFailed, fmt.Sprintf(ErrDetailUnknownOutcome, *job.Outcome))
			}
		}

		if job.HasFailed || (job.IsTerminal && !job.IsCompleted) {
			if job.Stage == JobStageCancelled {
				return nil, &jobCancelledError{message: fmt.Sprintf(ErrJobCancelled, jobId, action)}
			}
			if detail := jobFailureDetail(job, jobResponse.Message); detail != "" {
				return nil, fmt.Errorf("job %s for action %q: %w: %s", jobId, action, ErrJobFailed, detail)
			}
			return nil, fmt.Errorf("job %s for action %q: %w", jobId, action, ErrJobFailed)
		}

		if job.IsCompleted {
			tflog.Info(ctx, fmt.Sprintf(LogLongPollingCompletedSuccessfully, action),
				map[string]any{"job_id": jobId, "elapsed_seconds": int(elapsed.Seconds())})
			return jobResponse, nil
		}

		// Check timeout
		if elapsed >= config.Timeout {
			return nil, fmt.Errorf("job %s for action %q: %w (elapsed: %s)", jobId, action, ErrLongPollingTimeout, elapsed)
		}

		// Sleep with exponential backoff
		time.Sleep(interval)
		interval *= 2
		if interval > config.MaxInterval {
			interval = config.MaxInterval
		}
		iteration++
	}
}

// jobFailureDetail returns the text that tells the user why the job stopped.
// The envelope message is the last resort, because the read path always sends
// the same success sentence there.
func jobFailureDetail(job JobResponse, envelopeMessage string) string {
	if job.FailureReason != "" {
		return job.FailureReason
	}
	if job.ErrorMessage != "" {
		return job.ErrorMessage
	}
	if job.Message != "" {
		return job.Message
	}
	return envelopeMessage
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

// GetJobResourceID safely extracts the resource ID from a job response with bounds checking
func GetJobResourceID(resp *JobStatusMultiResponse) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("job response is nil")
	}
	if len(resp.Data.Jobs) == 0 {
		return "", ErrEmptyJobsResponse
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
