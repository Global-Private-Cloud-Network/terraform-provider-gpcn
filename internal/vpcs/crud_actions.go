package vpcs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// vpcPayload is the VPC object every VPC endpoint answers with. Only the detail
// read and the create 202 carry dnsNameservers.
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
	Status          string         `json:"status"`
	FailureReason   *string        `json:"failureReason"`
	EgressIp        *string        `json:"egressIp"`
	ActiveJobId     *string        `json:"activeJobId"`
	HeldBy          *client.HeldBy `json:"heldBy"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
	DnsNameservers  []string       `json:"dnsNameservers"`
	ResourceGroupID string         `json:"resourceGroupId"`
	VpnCapable      *bool          `json:"vpnCapable"`
}

type readVpcResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    vpcPayload `json:"data"`
}

// The create 202 carries the new row beside the job. The row exists already, so
// its id reaches state before the poll starts. CreateVpc fills both fields from
// either shape of the answer.
type createVpcResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		JobID string     `json:"jobId"`
		Vpc   vpcPayload `json:"vpc"`
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
	if isKnown(model.ResourceGroupId) {
		createVpcRequestBody["resourceGroupId"] = model.ResourceGroupId.ValueString()
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

	var accepted client.JobStatusSingularResponse
	if err := json.Unmarshal(response, &accepted); err != nil {
		return nil, err
	}
	var createdVpc createVpcResponse
	if err := json.Unmarshal(response, &createdVpc); err != nil {
		return nil, err
	}
	// The answer names the job as data.job.id or data.jobId, and carries the
	// row as data.resource or data.vpc.
	createdVpc.Data.JobID = accepted.Data.JobID
	if accepted.Data.Resource != nil {
		var row vpcPayload
		if err := json.Unmarshal(accepted.Data.Resource, &row); err != nil {
			return nil, err
		}
		createdVpc.Data.Vpc = row
	}
	// The ID is what Create writes to state before the poll. Without one the
	// row exists and Terraform cannot name it, so the create stops here.
	if createdVpc.Data.Vpc.ID == "" {
		return nil, errors.New(ErrDetailNoVpcIDInCreate)
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
	if isKnown(plan.ResourceGroupId) && !plan.ResourceGroupId.Equal(state.ResourceGroupId) {
		updateVpcRequestBody["resourceGroupId"] = plan.ResourceGroupId.ValueString()
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
// that refusal. A VPC already being torn down answers the same refusal. The row
// itself says whether a job drives that teardown.
func DeleteVpc(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVpcWithID, vpcID))

	start := time.Now()
	timeout := client.DefaultPollingConfig().Timeout
	if config := gpcnClient.Config(); config != nil && config.PollingTimeout > 0 {
		timeout = config.PollingTimeout
	}
	deadline := start.Add(timeout)

	// A 409 RESOURCE_BUSY names the job that holds the VPC. The delete goes out
	// again once that job ends, however it ends.
	jobID, err := issueVpcDelete(gpcnClient, ctx, vpcID)
	for holder, busy := client.BusyHolder(err); busy; holder, busy = client.BusyHolder(err) {
		if err := client.AwaitHolder(gpcnClient, ctx, holder, deadline); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(BusyRetryInterval):
		}
		jobID, err = issueVpcDelete(gpcnClient, ctx, vpcID)
	}
	if err != nil {
		if !client.HasErrorCode(err, ERROR_CODE_VPC_NOT_ACTIVE) {
			return err
		}
		vpcResponse, readErr := GetVpc(gpcnClient, ctx, vpcID)
		// The teardown can end between the refusal and this read. The row is
		// then gone, which is what the delete asked for.
		if client.IsNotFound(readErr) {
			tflog.Info(ctx, fmt.Sprintf(LogVpcTeardownFinished, vpcID))
			return nil
		}
		// A read that fails says nothing about the row. The reader needs the
		// failure the provider met, not the refusal that sent it to the row.
		if readErr != nil {
			return readErr
		}
		activeJobIDRef := client.ActiveJobID(vpcResponse.Data.HeldBy, vpcResponse.Data.ActiveJobId)
		switch {
		// A teardown that parked writes a failure reason, and no job then
		// drives the row. The platform re-admits a delete on it, so a wait
		// would never end.
		case vpcResponse.Data.Status == VPC_STATUS_DELETING && vpcResponse.Data.FailureReason == nil:
			tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpcTeardown, vpcID))
			return awaitVpcGone(gpcnClient, ctx, vpcID)
		case vpcResponse.Data.Status == VPC_STATUS_DELETING:
			tflog.Info(ctx, fmt.Sprintf(LogReclaimingParkedVpcTeardown, vpcID, *vpcResponse.Data.FailureReason))
		case vpcResponse.Data.Status == VPC_STATUS_CREATING && activeJobIDRef != nil:
			activeJobID := *activeJobIDRef
			tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpcActiveJob, vpcID, activeJobID))
			if pollErr := AwaitVpcJob(gpcnClient, ctx, ACTION_CREATE_VPC, activeJobID); pollErr != nil {
				tflog.Warn(ctx, fmt.Sprintf(LogVpcActiveJobPollFailed, vpcID, pollErr.Error()))
			}
		default:
			return err
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

	var deletedVpc client.JobStatusSingularResponse
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
