package volumes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type readVolumesResponse struct {
	Success bool                    `json:"success"`
	Message string                  `json:"message"`
	Data    readVolumesDataResponse `json:"data"`
}
type readVolumesDataResponse struct {
	ID                 string                            `json:"id"`
	Name               string                            `json:"name"`
	SizeGb             int64                             `json:"sizeGb"`
	VolumeSizeId       int64                             `json:"volumeSizeId"`
	VolumeType         readVolumesDataVolumeTypeResponse `json:"volumeType"`
	Datacenter         readVolumesDataDatacenterResponse `json:"datacenter"`
	VirtualMachineId   string                            `json:"virtualMachineId"`
	VirtualMachineName string                            `json:"virtualMachineName"`
	CreatedAt          string                            `json:"createdAt"`
	UpdatedAt          string                            `json:"updatedAt"`
}
type readVolumesDataVolumeTypeResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type readVolumesDataDatacenterResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Region  string `json:"region"`
	Country string `json:"countryAbbr"`
}

func CreateVolume(httpClient *http.Client, ctx context.Context, model ResourceModel) (*readVolumesResponse, error) {
	tflog.Info(ctx, LogStartingCreateVolume)
	// Find volumeTypeId based on volumeType. This will always be populated because of Schema validation
	volumeTypeId := volumeTypeMapping[model.VolumeType.ValueString()]

	tflog.Info(ctx, LogLookingUpVolumeSizeID)
	// Find volumeSizeId and do validation that sizeGb is valid
	volumeSizeId, err := GetVolumeSizeId(httpClient, ctx, model.DatacenterId.ValueString(), volumeTypeId, model.SizeGb.ValueInt64())

	if err != nil {
		return nil, err
	}

	// Create a new request from the model
	createVolumeRequestBody := map[string]any{
		"datacenterId": model.DatacenterId.ValueString(),
		"name":         model.Name.ValueString(),
		"volumeSizeId": volumeSizeId,
		"volumeTypeId": volumeTypeId,
		"sizeGb":       model.SizeGb.ValueInt64(),
	}

	jsonCreateVolumeRequestBody, err := json.Marshal(createVolumeRequestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("POST", BASE_URL, bytes.NewBuffer(jsonCreateVolumeRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateVolumeRequest)

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateVolumeJob)
	defer response.Body.Close()

	// Read the response body and process it as createVolumeResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var createVolumeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &createVolumeResponse)

	if err != nil {
		return nil, err
	}

	jobResp, err := client.PerformLongPolling(httpClient, ctx, "Create GPCN Volume", createVolumeResponse.Data.JobID)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVolume)
	// Perform a GET call to retrieve actual information about the Volume
	getVolumeResponse, err := GetVolume(httpClient, ctx, jobResp.Data.Jobs[0].ResourceId)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedVolumeCreate)
	return getVolumeResponse, nil
}

// Helper function to get a volume by Id. Shared between Read and the final action of Create and Update
func GetVolume(httpClient *http.Client, ctx context.Context, volumeId string) (*readVolumesResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVolumeWithID, volumeId))
	request, err := http.NewRequest("GET", BASE_URL+volumeId, nil)
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

	var readVolumesResponse readVolumesResponse
	err = json.Unmarshal(body, &readVolumesResponse)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVolumeWithID, volumeId))
	return &readVolumesResponse, nil
}

func UpdateVolume(httpClient *http.Client, ctx context.Context, volumeId string, model ResourceModel) (*readVolumesResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVolumeWithID, volumeId))
	// Find volumeTypeId based on volumeType. This will always be populated because of Schema validation
	volumeTypeId := volumeTypeMapping[model.VolumeType.ValueString()]

	tflog.Info(ctx, LogValidatingVolumeSizeForUpdate)
	// Do validation that sizeGb is valid
	_, err := GetVolumeSizeId(httpClient, ctx, model.DatacenterId.ValueString(), volumeTypeId, model.SizeGb.ValueInt64())

	if err != nil {
		return nil, err
	}

	// Create a new request from the plan
	// Validation was already done so we know the new sizeGb is larger than the previous state so this should be a valid request
	updateVolumeRequestBody := map[string]any{
		"newSizeGb": model.SizeGb.ValueInt64(),
	}

	jsonUpdateVolumeRequestBody, err := json.Marshal(updateVolumeRequestBody)
	if err != nil {
		return nil, err
	}

	// The only possible update is a resizing since everything else triggers a re-create
	request, err := http.NewRequest("PUT", BASE_URL+volumeId+"/resize", bytes.NewBuffer(jsonUpdateVolumeRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateVolumeRequest)

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedUpdateVolumeJob)
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var updateVolumeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &updateVolumeResponse)

	if err != nil {
		return nil, err
	}

	if !updateVolumeResponse.Success {
		return nil, errors.New("the job to update the GPCN Volume failed to start")
	}

	jobResp, err := client.PerformLongPolling(httpClient, ctx, "Update GPCN Volume", updateVolumeResponse.Data.JobID)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogLongPollingCompletedUpdateVolume)
	// Perform a GET call to retrieve actual information about the Volume
	getVolumeResponse, err := GetVolume(httpClient, ctx, jobResp.Data.Jobs[0].ResourceId)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedVolumeUpdate)
	return getVolumeResponse, nil
}

func DeleteVolume(httpClient *http.Client, ctx context.Context, volumeId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVolumeWithID, volumeId))
	request, err := http.NewRequest("DELETE", BASE_URL+volumeId, nil)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogConstructedDeleteVolumeRequest)

	// Detach this volume if possible
	// Verify the volume is attached to a virtual machine before attempting to detach it
	getVolumeResponse, err := GetVolume(httpClient, ctx, volumeId)
	if err != nil {
		return err
	}
	if getVolumeResponse.Data.VirtualMachineId != "" {
		err = RemoveVolumeFromVirtualMachine(httpClient, ctx, volumeId)
		if err != nil {
			return err
		}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteVolumeJob)
	defer response.Body.Close()

	// Read the response body and process it as deleteVolumeResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var deleteVolumeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &deleteVolumeResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(httpClient, ctx, "Delete GPCN Volume", deleteVolumeResponse.Data.JobID)

	if err != nil {
		return err
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyCompletedDeleteVolumeWithID, volumeId))
	return nil
}
