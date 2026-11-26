package virtualmachines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type ConfigurationResponse struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Code         string `json:"code"`
	CategoryCode string `json:"categoryCode"`
	SkuId        string `json:"skuId"`
	SkuCode      string `json:"skuCode"`
	CPU          int64  `json:"cpu"`
	RAM          int64  `json:"ram"`
	Disk         int64  `json:"disk"`
}

type ReadVirtualMachinesResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Status          string                `json:"status"`
		ID              string                `json:"id"`
		Name            string                `json:"name"`
		CreatedAt       string                `json:"createdAt"`
		UpdatedAt       string                `json:"updatedAt"`
		Configuration   ConfigurationResponse `json:"configuration"`
		Image           string                `json:"image"`
		Username        string                `json:"username"`
		SshKeyId        string                `json:"sshKeyId"`
		SshKeyName      string                `json:"sshKeyName"`
		NetworkHotplug  flexBool              `json:"networkHotplug"`
		ResourceGroupId string                `json:"resourceGroupId"`
		FailureReason   *string               `json:"failureReason"`
		Datacenter      struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Region      string `json:"region"`
			CountryAbbr string `json:"countryAbbr"`
			Country     string `json:"country"`
		} `json:"datacenter"`
	} `json:"data"`
}

// CreateVirtualMachine also returns the machine ID it knows when it fails: the
// ID the 202 carried, else the ID the completed job names. A row can outlive a
// failed create, so the caller records that ID and Terraform taints the machine.
func CreateVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, imageId string, skuId string, model ResourceModel) (*ReadVirtualMachinesResponse, string, error) {
	tflog.Info(ctx, LogStartingCreateVirtualMachine)

	// Create a new request from the model. The body is .strict() at GPCN, so it carries
	// only the keys this create needs. subnetId is the fork that names the VPC world.
	createVMRequestBody := map[string]any{
		"skuId":        skuId,
		"datacenterId": model.DatacenterId.ValueString(),
		"imageId":      imageId,
		"name":         model.Name.ValueString(),
		"subnetId":     model.SubnetId.ValueString(),
	}

	if err := addFirstBootKeys(ctx, model, createVMRequestBody); err != nil {
		return nil, "", err
	}

	if !model.ResourceGroupId.IsNull() && !model.ResourceGroupId.IsUnknown() {
		createVMRequestBody["resourceGroupId"] = model.ResourceGroupId.ValueString()
	}

	// GPCN reads acquirePublicIp as a request for a NEW address, and refuses it beside a
	// held one. A false flag would compete with publicIpId, so it is never sent.
	if model.AllocatePublicIp.ValueBool() {
		createVMRequestBody["acquirePublicIp"] = true
	}
	if !model.PublicIpId.IsNull() && model.PublicIpId.ValueString() != "" {
		createVMRequestBody["publicIpId"] = model.PublicIpId.ValueString()
	}

	jsonCreateVMRequestBody, err := json.Marshal(createVMRequestBody)
	if err != nil {
		return nil, "", err
	}

	// Create API request
	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateVMRequestBody))
	if err != nil {
		return nil, "", err
	}
	tflog.Info(ctx, LogConstructedCreateVMRequest)

	// Perform API request
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, "", err
	}
	tflog.Info(ctx, LogIssuedCreateVMJob)

	// Read the response body and process it as createVirtualMachineResponse
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}

	var createVirtualMachineResponse client.JobStatusMultiResponse
	err = json.Unmarshal(body, &createVirtualMachineResponse)

	if err != nil {
		return nil, "", err
	}

	// Extract job ID with bounds checking
	jobID, err := client.GetJobID(&createVirtualMachineResponse)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get job ID from create VM response: %w", err)
	}
	virtualMachineID, err := client.GetJobResourceID(&createVirtualMachineResponse)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get resource ID from create VM response: %w", err)
	}

	jobResp, err := client.PerformLongPolling(gpcnClient, ctx, "Create GPCN Virtual Machine", jobID)
	if err != nil {
		return nil, virtualMachineID, fmt.Errorf("create VM polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVM)

	if virtualMachineID == "" {
		virtualMachineID, err = client.GetJobResourceID(jobResp)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get resource ID from create VM job response: %w", err)
		}
	}
	if virtualMachineID == "" {
		return nil, "", errors.New(ErrDetailNoResourceIDInJob)
	}

	// Wait for the VM to actually be spun up before doing anything more
	getVirtualMachineResponse, err := PollForVirtualMachineStatus(gpcnClient, ctx, virtualMachineID, []string{VMStatusRunning.String(), VMStatusShutoff.String(), VMStatusStopped.String()}, DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS, DEFAULT_INITIAL_POLL_DELAY_SECONDS)
	if err != nil {
		return nil, virtualMachineID, err
	}

	tflog.Info(ctx, LogSuccessfullyProcessedVMCreate)
	return getVirtualMachineResponse, virtualMachineID, nil
}

// GPCN reads an omitted authMethod as its own request, not as custom, so a machine
// with no initial_auth and no user_data sends no authMethod.
func addFirstBootKeys(ctx context.Context, model ResourceModel, body map[string]any) error {
	if !model.UserData.IsNull() && !model.UserData.IsUnknown() {
		body["authMethod"] = "custom"
		body["userData"] = model.UserData.ValueString()
		return nil
	}

	if !model.InitialAuth.IsNull() {
		var auth ResourceModelInitialAuth
		authDiags := model.InitialAuth.As(ctx, &auth, basetypes.ObjectAsOptions{})
		if authDiags.HasError() {
			return fmt.Errorf("failed to read auth configuration")
		}

		if !auth.SshKeyId.IsNull() && auth.SshKeyId.ValueString() != "" {
			body["authMethod"] = "ssh-key"
			body["sshKeyId"] = auth.SshKeyId.ValueString()
		} else {
			body["authMethod"] = "password"
			body["password"] = auth.Password.ValueString()
		}
		if !auth.Username.IsNull() && auth.Username.ValueString() != "" {
			body["username"] = auth.Username.ValueString()
		}
	}

	if !model.FirstBootScript.IsNull() && !model.FirstBootScript.IsUnknown() {
		body["firstBootScript"] = model.FirstBootScript.ValueString()
	}
	return nil
}

// Gets a Virtual Machine by its ID
func GetVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string) (*ReadVirtualMachinesResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMWithID, virtualMachineId))
	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+virtualMachineId, nil)
	if err != nil {
		return nil, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var readVirtualMachinesResponse ReadVirtualMachinesResponse
	err = json.Unmarshal(body, &readVirtualMachinesResponse)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMWithID, virtualMachineId))
	return &readVirtualMachinesResponse, nil
}

// Updates a Virtual Machine by its ID
func UpdateVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, body map[string]any) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMWithID, virtualMachineId))

	jsonUpdateVMRequestBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+virtualMachineId, bytes.NewBuffer(jsonUpdateVMRequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyUpdatedVMWithID, virtualMachineId))
	return nil
}

// The Delete path must separate a dead virtual machine from every other poll
// failure. A typed error carries that fact, so no caller reads the message bytes.
type terminalStatusError struct {
	vmID    string
	status  string
	targets []string
}

func (e *terminalStatusError) Error() string {
	return fmt.Sprintf(ErrDetailVMTerminalStatus, e.vmID, e.status, strings.Join(e.targets, ", "))
}

func IsTerminalStatusError(err error) bool {
	var terminalErr *terminalStatusError
	return errors.As(err, &terminalErr)
}

func isTerminalFailureStatus(status string) bool {
	return slices.ContainsFunc(vmTerminalFailureStatuses, func(terminal VMStatus) bool {
		return strings.EqualFold(status, terminal.String())
	})
}

// Iteratively calls getVirtualMachine until the machine is in a target status, or it times out
func PollForVirtualMachineStatus(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, targetStatuses []string, timeoutMaxSec int, initialDelaySec int) (*ReadVirtualMachinesResponse, error) {
	// Make all statuses lowercase for ease of comparison
	targetStatusesLower := make([]string, 0, len(targetStatuses))
	for _, status := range targetStatuses {
		targetStatusesLower = append(targetStatusesLower, strings.ToLower(status))
	}
	tflog.Info(ctx, fmt.Sprintf(LogStartingPollForVMStatusWithID, virtualMachineId))

	// Wait for initial delay before starting polling
	if initialDelaySec > 0 {
		tflog.Info(ctx, fmt.Sprintf(LogInitialPollDelay, initialDelaySec))
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("polling virtual machine %s stopped: %w", virtualMachineId, ctx.Err())
		case <-time.After(time.Duration(initialDelaySec) * time.Second):
		}
	}
	var getResp *ReadVirtualMachinesResponse
	var err error
	longPollIteration := 1
	var pollErr error
	pollStart := time.Now()
	for {
		tflog.Info(ctx, fmt.Sprintf(LogStartingLongPollingIteration, longPollIteration, int(time.Since(pollStart).Seconds())))

		getResp, err = GetVirtualMachine(gpcnClient, ctx, virtualMachineId)
		if err != nil {
			pollErr = err
			break
		}
		tflog.Info(ctx, fmt.Sprintf(LogVMResponseStatus, getResp.Data.Status))

		if slices.Contains(targetStatusesLower, strings.ToLower(getResp.Data.Status)) {
			tflog.Info(ctx, fmt.Sprintf(LogVMStatusProceedingToAttach, getResp.Data.ID, getResp.Data.Status))
			// The API can report the target status before the change is complete.
			// The extra wait lowers that risk.
			// A cancel here still succeeds, because the machine has reached the target.
			select {
			case <-ctx.Done():
				return getResp, nil
			case <-time.After(VM_STATUS_SETTLE_WAIT):
			}
			break
		}
		if isTerminalFailureStatus(getResp.Data.Status) {
			pollErr = &terminalStatusError{vmID: virtualMachineId, status: getResp.Data.Status, targets: targetStatuses}
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("polling virtual machine %s stopped: %w", virtualMachineId, ctx.Err())
		case <-time.After(VM_STATUS_POLL_INTERVAL):
		}
		longPollIteration += 1

		if time.Since(pollStart) > time.Duration(timeoutMaxSec)*time.Second {
			//nolint:staticcheck // ST1005: the template is a user-facing Terraform diagnostic sentence
			pollErr = fmt.Errorf(ErrVirtualMachineStatusTimeoutTemplate, timeoutMaxSec)
			break
		}
	}
	if pollErr != nil {
		return nil, pollErr
	}
	return getResp, nil
}
