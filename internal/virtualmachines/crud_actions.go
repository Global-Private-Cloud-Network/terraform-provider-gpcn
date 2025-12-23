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
	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type ReadVirtualMachinesResponse struct {
	Success bool                            `json:"success"`
	Message string                          `json:"message"`
	Data    readVirtualMachinesDataResponse `json:"data"`
}
type readVirtualMachinesDataResponse struct {
	VirtualMachine readVirtualMachinesDataVMResponse `json:"virtualmachine"`
	Status         string                            `json:"status"`
}
type readVirtualMachinesDataVMResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	ConfigurationId int64  `json:"configurationId"`
	Configuration   string `json:"configuration"`
	CPU             int64  `json:"cpu"`
	RAM             int64  `json:"ram"`
	Disk            int64  `json:"disk"`
	Image           string `json:"image"`
	Username        string `json:"username"`
	DatacenterId    string `json:"datacenterId"`
	Datacenter      string `json:"datacenter"`
	RegionId        int64  `json:"regionId"`
	Region          string `json:"region"`
	Country         string `json:"country"`
}

func CreateVirtualMachine(httpClient *http.Client, ctx context.Context, imageId, sizeId int64, model ResourceModel) (*ReadVirtualMachinesResponse, error) {
	tflog.Info(ctx, LogStartingCreateVirtualMachine)

	// Allocate public Ip cannot be true if we are attaching a network of type custom
	tflog.Info(ctx, LogValidatingPublicIPConfiguration)
	err := ValidatePublicIpValue(httpClient, ctx, model)
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
	request, err := http.NewRequest("POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateVMRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateVMRequest)

	// Perform API request
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateVMJob)
	defer response.Body.Close()

	// Read the response body and process it as createVirtualMachineResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var createVirtualMachineResponse client.JobStatusMultiResponse
	err = json.Unmarshal(body, &createVirtualMachineResponse)

	if err != nil {
		return nil, err
	}

	jobResp, err := client.PerformLongPolling(httpClient, ctx, "Create GPCN Virtual Machine", createVirtualMachineResponse.Data.Jobs[0].JobID)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVM)
	// Wait for the VM to actually be spun up before doing anything more
	getVirtualMachineResponse, err := PollForVirtualMachineStatus(httpClient, ctx, jobResp.Data.Jobs[0].ResourceId, []string{Running, Shutoff}, DEFAULT_NETWORK_TIMEOUT_SECONDS)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyProcessedVMCreate)
	return getVirtualMachineResponse, nil
}

// Gets a Virtual Machine by its Id
func GetVirtualMachine(httpClient *http.Client, ctx context.Context, virtualMachineId string) (*ReadVirtualMachinesResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMWithID, virtualMachineId))
	request, err := http.NewRequest("GET", BASE_URL_V1+virtualMachineId, nil)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.Do(request)
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

// Updates a Virtual Machine by its Id
func UpdateVirtualMachine(httpClient *http.Client, ctx context.Context, virtualMachineId, name string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMWithID, virtualMachineId))
	// Create a new request from the plan
	updateVMRequestBody := map[string]any{
		"name": name,
	}

	jsonUpdateVMRequestBody, err := json.Marshal(updateVMRequestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequest("PUT", BASE_URL_V1+virtualMachineId, bytes.NewBuffer(jsonUpdateVMRequestBody))
	if err != nil {
		return err
	}

	_, err = httpClient.Do(request)
	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyUpdatedVMWithID, virtualMachineId))
	return nil
}

// Iteratively calls getVirtualMachine until the machine is in a target status, or it times out
func PollForVirtualMachineStatus(httpClient *http.Client, ctx context.Context, virtualMachineId string, targetStatuses []string, timeoutMaxSec int) (*ReadVirtualMachinesResponse, error) {
	// Make all statuses lowercase for ease of comparison
	targetStatusesLower := make([]string, len(targetStatuses))
	for _, status := range targetStatuses {
		targetStatusesLower = append(targetStatusesLower, strings.ToLower(status))
	}
	tflog.Info(ctx, fmt.Sprintf(LogStartingPollForVMStatusWithID, virtualMachineId))
	var getVirtualMachineResponse *ReadVirtualMachinesResponse
	secondsElapsed := 0
	longPollIteration := 1
	var errString string
	for {
		tflog.Info(ctx, fmt.Sprintf(LogStartingLongPollingIteration, longPollIteration, secondsElapsed))

		getResp, err := GetVirtualMachine(httpClient, ctx, virtualMachineId)
		if err != nil {
			errString = err.Error()
			break
		}
		getVirtualMachineResponse = getResp
		tflog.Info(ctx, fmt.Sprintf(LogVMResponseStatus, getVirtualMachineResponse.Data.Status))

		if slices.Contains(targetStatusesLower, strings.ToLower(getVirtualMachineResponse.Data.Status)) {
			tflog.Info(ctx, fmt.Sprintf(LogVMStatusProceedingToAttach, getVirtualMachineResponse.Data.VirtualMachine.ID, getVirtualMachineResponse.Data.Status))
			// Don't trust the API and do actions too quick. Wait an additional 5 seconds to verify it's actually in the status we want
			time.Sleep(time.Second * 5)
			break
		}
		time.Sleep(time.Second * 5)
		secondsElapsed += 5
		longPollIteration += 1

		if secondsElapsed > timeoutMaxSec {
			errString = ErrVirtualMachineStatusTimeout
			break
		}
	}
	if errString != "" {
		return nil, errors.New(errString)
	}
	return getVirtualMachineResponse, nil
}

// Verify if public IP is set to true, the first network cannot be of type custom
func ValidatePublicIpValue(httpClient *http.Client, ctx context.Context, model ResourceModel) error {
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
	getNetworkResponse, err := networks.GetNetwork(httpClient, ctx, networkIds[0])
	if err != nil {
		return err
	}

	if getNetworkResponse.Data.NetworkType == networks.NETWORK_TYPE_CUSTOM {
		return errors.New(ErrDetailNetworkTypeMustBeStandard)
	}

	tflog.Info(ctx, LogPublicIPValidationPassed)
	return nil
}
