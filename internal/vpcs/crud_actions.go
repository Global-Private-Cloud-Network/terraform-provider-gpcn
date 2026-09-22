package vpcs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// vpcPayload is the VPC object every VPC endpoint answers with
// (src/components/vpc/vpc.controller.ts:43-59). Only the detail read and the
// create 202 carry dnsNameservers.
type vpcPayload struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CIDR        string  `json:"cidr"`
	Datacenter  struct {
		ID   string  `json:"id"`
		Code *string `json:"code"`
		Name string  `json:"name"`
	} `json:"datacenter"`
	Status         string   `json:"status"`
	FailureReason  *string  `json:"failureReason"`
	EgressIp       *string  `json:"egressIp"`
	ActiveJobId    *string  `json:"activeJobId"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
	DnsNameservers []string `json:"dnsNameservers"`
}

type readVpcResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    vpcPayload `json:"data"`
}

// The create 202 carries the new row beside the job. The row exists already, so
// its id reaches state before the poll starts.
type createVpcResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		JobID string     `json:"jobId"`
		Vpc   vpcPayload `json:"vpc"`
	} `json:"data"`
}

type deleteVpcResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		JobID string `json:"jobId"`
	} `json:"data"`
}

func CreateVpc(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) (*createVpcResponse, error) {
	tflog.Info(ctx, LogStartingCreateVpc)

	createVpcRequestBody := map[string]any{
		"name":         model.Name.ValueString(),
		"cidr":         model.CIDR.ValueString(),
		"datacenterId": model.DatacenterId.ValueString(),
	}
	if isKnown(model.Description) {
		createVpcRequestBody["description"] = model.Description.ValueString()
	}
	if !model.DNSNameservers.IsNull() && !model.DNSNameservers.IsUnknown() {
		createVpcRequestBody["dnsNameservers"] = stringsFromList(model.DNSNameservers)
	}
	// The 409 this key answers is a confirm gate. A request that always carries
	// the key defeats the gate.
	if model.AcknowledgeOverlap.ValueBool() {
		createVpcRequestBody["acknowledgeOverlap"] = true
	}

	response, err := sendVpcRequest(gpcnClient, ctx, http.MethodPost, BASE_URL_V1, createVpcRequestBody)
	if err != nil {
		return nil, err
	}

	var createdVpc createVpcResponse
	if err := json.Unmarshal(response, &createdVpc); err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateVpcJob)
	return &createdVpc, nil
}

func AwaitVpcJob(gpcnClient *client.GpcnClient, ctx context.Context, action, jobID string) error {
	if _, err := client.PerformLongPolling(gpcnClient, ctx, action, jobID); err != nil {
		return fmt.Errorf("%s polling failed: %w", action, err)
	}
	return nil
}

func GetVpc(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) (*readVpcResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVpcWithID, vpcID))

	response, err := sendVpcRequest(gpcnClient, ctx, http.MethodGet, BASE_URL_V1+vpcID, nil)
	if err != nil {
		return nil, err
	}

	var vpcResponse readVpcResponse
	if err := json.Unmarshal(response, &vpcResponse); err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVpcWithID, vpcID))
	return &vpcResponse, nil
}

// UpdateVpc sends the changed keys alone. The API refuses a body that carries
// an immutable key, and it refuses an empty body.
func UpdateVpc(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, plan, state ResourceModel) (*readVpcResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVpcWithID, vpcID))

	updateVpcRequestBody := map[string]any{}
	if plan.Name.ValueString() != state.Name.ValueString() {
		updateVpcRequestBody["name"] = plan.Name.ValueString()
	}
	if isKnown(plan.Description) && plan.Description.ValueString() != state.Description.ValueString() {
		updateVpcRequestBody["description"] = plan.Description.ValueString()
	}

	if len(updateVpcRequestBody) == 0 {
		tflog.Info(ctx, fmt.Sprintf(LogVpcUpdateNothingToSend, vpcID))
		return GetVpc(gpcnClient, ctx, vpcID)
	}

	if _, err := sendVpcRequest(gpcnClient, ctx, http.MethodPut, BASE_URL_V1+vpcID, updateVpcRequestBody); err != nil {
		return nil, err
	}
	return GetVpc(gpcnClient, ctx, vpcID)
}

// DeleteVpc claims the VPC and waits for the teardown job. A VPC that is still
// being created answers VPC_NOT_ACTIVE. Only the end of its create job clears
// that refusal. A VPC already being torn down answers the same refusal, and the
// teardown it names is the work the caller asked for.
func DeleteVpc(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVpcWithID, vpcID))

	jobID, err := issueVpcDelete(gpcnClient, ctx, vpcID)
	if err != nil {
		if !client.HasErrorCode(err, ERROR_CODE_VPC_NOT_ACTIVE) {
			return err
		}
		vpcResponse, readErr := GetVpc(gpcnClient, ctx, vpcID)
		if readErr != nil {
			return err
		}
		if vpcResponse.Data.Status == VPC_STATUS_DELETING {
			tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpcTeardown, vpcID))
			return awaitVpcGone(gpcnClient, ctx, vpcID)
		}
		if vpcResponse.Data.Status != VPC_STATUS_CREATING || vpcResponse.Data.ActiveJobId == nil {
			return err
		}
		activeJobID := *vpcResponse.Data.ActiveJobId
		tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpcActiveJob, vpcID, activeJobID))
		if pollErr := AwaitVpcJob(gpcnClient, ctx, ACTION_CREATE_VPC, activeJobID); pollErr != nil {
			tflog.Warn(ctx, fmt.Sprintf(LogVpcActiveJobPollFailed, vpcID, pollErr.Error()))
		}
		jobID, err = issueVpcDelete(gpcnClient, ctx, vpcID)
		if err != nil {
			return err
		}
	}

	if err := AwaitVpcJob(gpcnClient, ctx, ACTION_DELETE_VPC, jobID); err != nil {
		return err
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyDeletedVpcID, vpcID))
	return nil
}

func issueVpcDelete(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) (string, error) {
	response, err := sendVpcRequest(gpcnClient, ctx, http.MethodDelete, BASE_URL_V1+vpcID, nil)
	if err != nil {
		return "", err
	}

	var deletedVpc deleteVpcResponse
	if err := json.Unmarshal(response, &deletedVpc); err != nil {
		return "", err
	}
	tflog.Info(ctx, LogIssuedDeleteVpcJob)
	return deletedVpc.Data.JobID, nil
}

// awaitVpcGone waits out a teardown the caller did not start. The row answers
// 404 when the teardown ends, which is the state the delete asked for. The
// teardown job belongs to the other caller, so only the row reports progress.
func awaitVpcGone(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	config := client.DefaultPollingConfig()
	if clientConfig := gpcnClient.Config(); clientConfig != nil && clientConfig.PollingTimeout > 0 {
		config.Timeout = clientConfig.PollingTimeout
	}

	startTime := time.Now()
	interval := config.InitialInterval
	for {
		_, err := GetVpc(gpcnClient, ctx, vpcID)
		if client.IsNotFound(err) {
			tflog.Info(ctx, fmt.Sprintf(LogVpcTeardownFinished, vpcID))
			return nil
		}
		if err != nil {
			return err
		}

		elapsed := time.Since(startTime)
		if elapsed >= config.Timeout {
			return fmt.Errorf(ErrDetailVpcTeardownTimeout, elapsed)
		}

		time.Sleep(interval)
		interval *= 2
		if interval > config.MaxInterval {
			interval = config.MaxInterval
		}
	}
}

func sendVpcRequest(gpcnClient *client.GpcnClient, ctx context.Context, method, url string, requestBody map[string]any) ([]byte, error) {
	var payload io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewBuffer(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return nil, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return io.ReadAll(response.Body)
}

func stringsFromList(list types.List) []string {
	values := make([]string, 0, len(list.Elements()))
	for _, element := range list.Elements() {
		value, ok := element.(types.String)
		if !ok {
			continue
		}
		values = append(values, value.ValueString())
	}
	return values
}

func isKnown(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown()
}
