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

func PerformLongPolling(client *http.Client, ctx context.Context, action, jobId string) (*JobStatusMultiResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingPerformLongPollingWithAction, action))
	var jobResp *JobStatusMultiResponse
	secondsElapsed := 0
	longPollIteration := 1
	var errString string
	for {
		tflog.Info(ctx, fmt.Sprintf(LogStartingLongPollingIteration, longPollIteration, action, secondsElapsed))
		jobResponse, completed, err := poll(client, jobId)
		if err != nil {
			errString = err.Error()
		}
		if completed {
			jobResp = jobResponse
			tflog.Info(ctx, fmt.Sprintf(LogLongPollingCompletedSuccessfully, action))
			break
		}
		// Three second long-polling. Crude but effective
		time.Sleep(time.Second * 3)
		secondsElapsed += 3
		longPollIteration += 1

		// Give 10 minutes max timeout. If the individual job isn't completed by then, high chance it failed
		if secondsElapsed > 600 {
			errString = ErrLongPollingTimeout
			break
		}
	}

	// If there was an error, surface it
	if jobResp == nil || len(jobResp.Data.Jobs) == 0 {
		return nil, errors.New(errString)
	}

	return jobResp, nil
}

func poll(client *http.Client, jobId string) (*JobStatusMultiResponse, bool, error) {
	jobStatusRequestBody := map[string][]string{
		"jobIds": {jobId},
	}

	jsonJobStatusRequestBody, err := json.Marshal(jobStatusRequestBody)
	if err != nil {
		return nil, false, err
	}

	request, err := http.NewRequest("POST", JOBS_BASE_URL_V1, bytes.NewBuffer(jsonJobStatusRequestBody))
	if err != nil {
		return nil, false, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, false, err
	}

	var jobStatusMultiResponse JobStatusMultiResponse
	err = json.Unmarshal(body, &jobStatusMultiResponse)

	if err != nil {
		return nil, false, err
	}

	if jobStatusMultiResponse.Data.Jobs[0].HasFailed {
		return nil, true, errors.New("job operation failed! Please check parameters and retry operation")
	}

	return &jobStatusMultiResponse, jobStatusMultiResponse.Data.Jobs[0].IsCompleted && !jobStatusMultiResponse.Data.Jobs[0].HasFailed, nil
}
