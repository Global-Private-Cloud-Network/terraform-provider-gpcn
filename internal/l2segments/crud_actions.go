package l2segments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// l2SegmentData is the detail projection. The list and the detail carry the same
// key set, so one struct reads both.
type l2SegmentData struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Description      *string `json:"description"`
	DatacenterId     string  `json:"datacenterId"`
	DatacenterName   *string `json:"datacenterName"`
	State            string  `json:"state"`
	Offering         string  `json:"offering"`
	FailureReason    *string `json:"failureReason"`
	AttachedNicCount int64   `json:"attachedNicCount"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

type readL2SegmentResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Data    l2SegmentData `json:"data"`
}

// createRequestBody builds the create body. The wire schema is strict, so an
// empty description is omitted rather than written as an empty string.
func createRequestBody(model ResourceModel) map[string]any {
	body := map[string]any{
		"name":         model.Name.ValueString(),
		"datacenterId": model.DatacenterId.ValueString(),
	}
	if description := model.Description.ValueString(); description != "" {
		body["description"] = description
	}
	return body
}

// updateRequestBody names only what changed. The wire schema refuses a body that
// names neither the name nor the description.
func updateRequestBody(plan, state ResourceModel) map[string]any {
	body := map[string]any{}
	if !plan.Name.Equal(state.Name) {
		body["name"] = plan.Name.ValueString()
	}
	if !plan.Description.Equal(state.Description) {
		body["description"] = plan.Description.ValueString()
	}
	return body
}

// CreateL2Segment orders a segment and reads it back. The 202 names a job and
// nothing else, and the job names the segment only once it completes.
func CreateL2Segment(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) (*readL2SegmentResponse, error) {
	tflog.Info(ctx, LogStartingCreateL2Segment)

	jsonCreateRequestBody, err := json.Marshal(createRequestBody(model))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateL2SegmentRequestBody)

	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateL2SegmentRequest)

	createResponse, err := issueJob(gpcnClient, ctx, request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateL2SegmentJob)

	jobResponse, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateL2Segment, createResponse.Data.JobID)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogLongPollingCompletedCreateL2Segment)

	segmentID, err := client.GetJobResourceID(jobResponse)
	if err != nil {
		return nil, err
	}
	if segmentID == "" {
		return nil, fmt.Errorf("%s", ErrDetailNoSegmentIDInJob)
	}

	getResponse, err := GetL2Segment(gpcnClient, ctx, segmentID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedL2SegmentCreate)
	return getResponse, nil
}

// GetL2Segment reads one segment. Read, Create and Update all end here.
func GetL2Segment(gpcnClient *client.GpcnClient, ctx context.Context, segmentID string) (*readL2SegmentResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetL2SegmentWithID, segmentID))

	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+segmentID, nil)
	if err != nil {
		return nil, err
	}

	segmentResponse, err := readSegment(gpcnClient, request)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedL2SegmentWithID, segmentID))
	return segmentResponse, nil
}

// UpdateL2Segment writes the changed keys. The update is a database write that
// never reaches the cloud provider. It answers with the detail and no job.
func UpdateL2Segment(gpcnClient *client.GpcnClient, ctx context.Context, segmentID string, plan, state ResourceModel) (*readL2SegmentResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateL2SegmentWithID, segmentID))

	body := updateRequestBody(plan, state)
	if len(body) == 0 {
		tflog.Info(ctx, fmt.Sprintf(LogNothingToUpdateOnL2Segment, segmentID))
		return GetL2Segment(gpcnClient, ctx, segmentID)
	}

	jsonUpdateRequestBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateL2SegmentRequestBody)

	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+segmentID, bytes.NewBuffer(jsonUpdateRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateL2SegmentRequest)

	segmentResponse, err := readSegment(gpcnClient, request)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedL2SegmentUpdate)
	return segmentResponse, nil
}

// DeleteL2Segment tears the segment down. The refusals are synchronous, and only
// an accepted removal carries a job.
func DeleteL2Segment(gpcnClient *client.GpcnClient, ctx context.Context, segmentID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteL2SegmentWithID, segmentID))

	request, err := http.NewRequestWithContext(ctx, "DELETE", BASE_URL_V1+segmentID, nil)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogConstructedDeleteL2SegmentRequest)

	deleteResponse, err := issueJob(gpcnClient, ctx, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteL2SegmentJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionDeleteL2Segment, deleteResponse.Data.JobID); err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyCompletedDeleteL2SegmentWithID, segmentID))
	return nil
}

// issueJob sends a request the platform answers with a job id.
func issueJob(gpcnClient *client.GpcnClient, ctx context.Context, request *http.Request) (*client.JobStatusSingularResponse, error) {
	body, err := sendAndRead(gpcnClient, ctx, request)
	if err != nil {
		return nil, err
	}

	var jobResponse client.JobStatusSingularResponse
	if err := json.Unmarshal(body, &jobResponse); err != nil {
		return nil, err
	}
	return &jobResponse, nil
}

// readSegment sends a request the platform answers with the detail projection.
func readSegment(gpcnClient *client.GpcnClient, request *http.Request) (*readL2SegmentResponse, error) {
	body, err := sendAndRead(gpcnClient, request.Context(), request)
	if err != nil {
		return nil, err
	}

	var segmentResponse readL2SegmentResponse
	if err := json.Unmarshal(body, &segmentResponse); err != nil {
		return nil, err
	}
	return &segmentResponse, nil
}

func sendAndRead(gpcnClient *client.GpcnClient, ctx context.Context, request *http.Request) ([]byte, error) {
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			tflog.Warn(ctx, "failed to close response body", map[string]any{"error": closeErr.Error()})
		}
	}()

	return io.ReadAll(response.Body)
}
