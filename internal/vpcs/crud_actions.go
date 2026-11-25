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
// its id reaches state before the poll starts.
type createVpcResponse struct {
	Success bool
	Message string
	Data    struct {
		JobID string
		Vpc   vpcPayload
	}
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
	createdVpc := createVpcResponse{Success: accepted.Success, Message: accepted.Message}
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
	waitedBusy := false
	for holder, busy := client.BusyHolder(err); busy; holder, busy = client.BusyHolder(err) {
		waitedBusy = true
		outcome, waitErr := client.AwaitHolder(gpcnClient, ctx, holder, start, deadline)
		if waitErr != nil {
			return waitErr
		}
		select {
		case <-ctx.Done():
			return client.BusyWaitEnded(ctx.Err())
		case <-time.After(BusyRetryInterval):
		}
		// No DELETE goes out after the deadline.
		if !time.Now().Before(deadline) {
			return client.HolderDeadline(outcome, holder, start)
		}
		jobID, err = issueVpcDelete(gpcnClient, ctx, vpcID)
	}
	if err != nil {
		return err
	}

	// After a busy wait the job poll shares the deadline of that wait.
	if waitedBusy {
		pollConfig := client.DefaultPollingConfig()
		pollConfig.Timeout = max(timeout-time.Since(start), 0)
		if _, err := client.PerformLongPollingWithConfig(gpcnClient, ctx, ACTION_DELETE_VPC, jobID, pollConfig); err != nil {
			return fmt.Errorf("%s polling failed: %w", ACTION_DELETE_VPC, err)
		}
	} else if err := AwaitVpcJob(gpcnClient, ctx, ACTION_DELETE_VPC, jobID); err != nil {
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
