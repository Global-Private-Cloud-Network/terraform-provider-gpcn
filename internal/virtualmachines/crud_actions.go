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
	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type ConfigurationResponse struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Code         string `json:"code"`
	CategoryCode string `json:"categoryCode"`
	CPU          int64  `json:"cpu"`
	RAM          int64  `json:"ram"`
	Disk         int64  `json:"disk"`
}

type ReadVirtualMachinesResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Status         string                `json:"status"`
		ID             string                `json:"id"`
		Name           string                `json:"name"`
		CreatedAt      string                `json:"createdAt"`
		UpdatedAt      string                `json:"updatedAt"`
		Configuration  ConfigurationResponse `json:"configuration"`
		Image          string                `json:"image"`
		Username       string                `json:"username"`
		NetworkHotplug int                   `json:"networkHotplug"`
		Datacenter     struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Region      string `json:"region"`
			CountryAbbr string `json:"countryAbbr"`
			Country     string `json:"country"`
		} `json:"datacenter"`
	} `json:"data"`
}

func CreateVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, imageId, sizeId int64, model ResourceModel) (*ReadVirtualMachinesResponse, error) {
	tflog.Info(ctx, LogStartingCreateVirtualMachine)

	// Allocate public Ip cannot be true if we are attaching a network of type custom
	tflog.Info(ctx, LogValidatingPublicIPConfiguration)
	err := ValidatePublicIpValue(gpcnClient, ctx, model)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogValidatedPublicIPConfigurationSuccessfully)

	// Create a new request from the model
	createVMRequestBody := map[string]any{
		"allocatePublicIp":  model.AllocatePublicIp.ValueBool(),
		"configurationId":   sizeId,
		"datacenterId":      model.DatacenterId.ValueString(),
		"imageId":           imageId,
		"name":              model.Name.ValueString(),
		"numberOfInstances": 1,
	}

	// If networkIds is populated, add it to the create request
	if !model.NetworkIds.IsNull() && len(model.NetworkIds.Elements()) > 0 {
		var networkIds []string
		model.NetworkIds.ElementsAs(ctx, &networkIds, true)

		tflog.Info(ctx, LogNetworkIdsNotNull)
		// Add all network interfaces, setting the first value entered as the primary
		var networkInterfaces []map[string]any
		for idx, networkId := range networkIds {
			networkInterfaces = append(networkInterfaces, map[string]any{
				"networkId": networkId,
				"primary":   idx == 0,
			})
		}
		createVMRequestBody["networkInterfaces"] = networkInterfaces
	} else {
		tflog.Info(ctx, LogNetworkIdsNullOrEmpty)
	}

	jsonCreateVMRequestBody, err := json.Marshal(createVMRequestBody)
	if err != nil {
		return nil, err
	}

	// Create API request
	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateVMRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateVMRequest)

	// Perform API request
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateVMJob)

	// Read the response body and process it as createVirtualMachineResponse
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var createVirtualMachineResponse client.JobStatusMultiResponse
	err = json.Unmarshal(body, &createVirtualMachineResponse)

	if err != nil {
		return nil, err
	}

	// Extract job ID with bounds checking
	jobID, err := client.GetJobID(&createVirtualMachineResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to get job ID from create VM response: %w", err)
	}

	jobResp, err := client.PerformLongPolling(gpcnClient, ctx, "Create GPCN Virtual Machine", jobID)
	if err != nil {
		return nil, fmt.Errorf("create VM polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVM)

	// Extract resource ID with bounds checking
	resourceID, err := client.GetJobResourceID(jobResp)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource ID from create VM job response: %w", err)
	}

	// Wait for the VM to actually be spun up before doing anything more
	getVirtualMachineResponse, err := PollForVirtualMachineStatus(gpcnClient, ctx, resourceID, []string{VMStatusRunning.String(), VMStatusShutoff.String()}, DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS, DEFAULT_INITIAL_POLL_DELAY_SECONDS)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyProcessedVMCreate)
	return getVirtualMachineResponse, nil
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
func UpdateVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, name string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMWithID, virtualMachineId))
	// Create a new request from the plan
	updateVMRequestBody := map[string]any{
		"name": name,
	}

	jsonUpdateVMRequestBody, err := json.Marshal(updateVMRequestBody)
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

// Iteratively calls getVirtualMachine until the machine is in a target status, or it times out
func PollForVirtualMachineStatus(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, targetStatuses []string, timeoutMaxSec int, initialDelaySec int) (*ReadVirtualMachinesResponse, error) {
	// Make all statuses lowercase for ease of comparison
	targetStatusesLower := make([]string, len(targetStatuses))
	for _, status := range targetStatuses {
		targetStatusesLower = append(targetStatusesLower, strings.ToLower(status))
	}
	tflog.Info(ctx, fmt.Sprintf(LogStartingPollForVMStatusWithID, virtualMachineId))

	// Wait for initial delay before starting polling
	if initialDelaySec > 0 {
		tflog.Info(ctx, fmt.Sprintf(LogInitialPollDelay, initialDelaySec))
		time.Sleep(time.Duration(initialDelaySec) * time.Second)
	}
	var getResp *ReadVirtualMachinesResponse
	var err error
	secondsElapsed := 0
	longPollIteration := 1
	var errString string
	for {
		tflog.Info(ctx, fmt.Sprintf(LogStartingLongPollingIteration, longPollIteration, secondsElapsed))

		getResp, err = GetVirtualMachine(gpcnClient, ctx, virtualMachineId)
		if err != nil {
			errString = err.Error()
			break
		}
		tflog.Info(ctx, fmt.Sprintf(LogVMResponseStatus, getResp.Data.Status))

		if slices.Contains(targetStatusesLower, strings.ToLower(getResp.Data.Status)) {
			tflog.Info(ctx, fmt.Sprintf(LogVMStatusProceedingToAttach, getResp.Data.ID, getResp.Data.Status))
			// Don't trust the API and do actions too quick. Wait an additional 5 seconds to verify it's actually in the status we want
			time.Sleep(time.Second * 5)
			break
		}
		time.Sleep(time.Second * 5)
		secondsElapsed += 5
		longPollIteration += 1

		if secondsElapsed > timeoutMaxSec {
			errString = fmt.Sprintf(ErrVirtualMachineStatusTimeoutTemplate, timeoutMaxSec)
			break
		}
	}
	if errString != "" {
		return nil, errors.New(errString)
	}
	return getResp, nil
}

// Verify if public IP is set to true, the first network cannot be of type custom
func ValidatePublicIpValue(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) error {
	tflog.Info(ctx, LogStartingValidatePublicIPValue)
	// If false, no error
	if !model.AllocatePublicIp.ValueBool() {
		tflog.Info(ctx, LogPublicIPNotAllocated)
		return nil
	}

	// If true, check if we have networks and check the primary (first) network type
	if model.NetworkIds.IsNull() || len(model.NetworkIds.Elements()) < 1 {
		tflog.Info(ctx, LogNoNetworksSpecified)
		return nil
	}
	var networkIds []string
	model.NetworkIds.ElementsAs(ctx, &networkIds, true)

	tflog.Info(ctx, LogValidatingPublicIPSettingByNetworkType)
	getNetworkResponse, err := networks.GetNetwork(gpcnClient, ctx, networkIds[0])
	if err != nil {
		return err
	}

	if getNetworkResponse.Data.NetworkType == networks.NETWORK_TYPE_CUSTOM {
		return errors.New(ErrDetailNetworkTypeMustBeStandard)
	}

	tflog.Info(ctx, LogPublicIPValidationPassed)
	return nil
}
