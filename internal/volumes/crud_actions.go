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
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		SizeGb        int64  `json:"sizeGb"`
		Configuration struct {
			SkuId string `json:"skuId"`
		} `json:"configuration"`
		VolumeType struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"volumeType"`
		Datacenter struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Region      string `json:"region"`
			CountryAbbr string `json:"countryAbbr"`
			Country     string `json:"country"`
		} `json:"datacenter"`
		VirtualMachineId   string `json:"virtualMachineId"`
		VirtualMachineName string `json:"virtualMachineName"`
		CreatedAt          string `json:"createdAt"`
		UpdatedAt          string `json:"updatedAt"`
	} `json:"data"`
}

func CreateVolume(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) (*readVolumesResponse, error) {
	tflog.Info(ctx, LogStartingCreateVolume)
	componentCode := volumeTypeMapping[model.VolumeType.ValueString()]

	tflog.Info(ctx, LogLookingUpVolumeSkuId)
	skuId, err := GetVolumeSkuId(gpcnClient, ctx, model.DatacenterId.ValueString(), componentCode, model.SizeGb.ValueInt64())
	if err != nil {
		return nil, err
	}

	createVolumeRequestBody := map[string]any{
		"datacenterId": model.DatacenterId.ValueString(),
		"name":         model.Name.ValueString(),
		"skuId":        skuId,
	}

	jsonCreateVolumeRequestBody, err := json.Marshal(createVolumeRequestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateVolumeRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateVolumeRequest)

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	tflog.Info(ctx, LogIssuedCreateVolumeJob)

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

	// The API accepted the create. After this point, an error can leave a volume
	// that Terraform does not record. Each error must give its ID.
	jobResp, err := client.PerformLongPolling(gpcnClient, ctx, "Create GPCN Volume", createVolumeResponse.Data.JobID)
	if err != nil {
		return nil, fmt.Errorf("create volume polling failed (job %s can still complete and make a volume): %w", createVolumeResponse.Data.JobID, err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVolume)

	// Extract resource ID with bounds checking
	resourceID, err := client.GetJobResourceID(jobResp)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource ID from create volume job response: %w", err)
	}

	// Perform a GET call to retrieve actual information about the Volume
	getVolumeResponse, err := GetVolume(gpcnClient, ctx, resourceID)
	if err != nil {
		return nil, client.NewPartialCreateError(resourceID, err)
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedVolumeCreate)
	return getVolumeResponse, nil
}

// Helper function to get a volume by ID. Shared between Read and the final action of Create and Update
func GetVolume(gpcnClient *client.GpcnClient, ctx context.Context, volumeId string) (*readVolumesResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVolumeWithID, volumeId))
	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+volumeId, nil)
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

	var volResp readVolumesResponse
	err = json.Unmarshal(body, &volResp)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVolumeWithID, volumeId))
	return &volResp, nil
}

func UpdateVolume(gpcnClient *client.GpcnClient, ctx context.Context, volumeId string, model ResourceModel) (*readVolumesResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVolumeWithID, volumeId))
	componentCode := volumeTypeMapping[model.VolumeType.ValueString()]

	tflog.Info(ctx, LogValidatingVolumeSkuIdForUpdate)

	// Verify that the new volume size exists in the datacenter
	_, err := GetVolumeSkuId(gpcnClient, ctx, model.DatacenterId.ValueString(), componentCode, model.SizeGb.ValueInt64())
	if err != nil {
		return nil, err
	}

	updateVolumeRequestBody := map[string]any{
		"newSizeGb": model.SizeGb.ValueInt64(),
	}

	jsonUpdateVolumeRequestBody, err := json.Marshal(updateVolumeRequestBody)
	if err != nil {
		return nil, err
	}

	// The only possible update is a resizing since everything else triggers a re-create
	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+volumeId+"/resize", bytes.NewBuffer(jsonUpdateVolumeRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateVolumeRequest)

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	tflog.Info(ctx, LogIssuedUpdateVolumeJob)

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

	jobResp, err := client.PerformLongPolling(gpcnClient, ctx, "Update GPCN Volume", updateVolumeResponse.Data.JobID)
	if err != nil {
		return nil, fmt.Errorf("update volume polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedUpdateVolume)

	// Extract resource ID with bounds checking
	updateResourceID, err := client.GetJobResourceID(jobResp)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource ID from update volume job response: %w", err)
	}

	// Perform a GET call to retrieve actual information about the Volume
	getVolumeResponse, err := GetVolume(gpcnClient, ctx, updateResourceID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedVolumeUpdate)
	return getVolumeResponse, nil
}

func DeleteVolume(gpcnClient *client.GpcnClient, ctx context.Context, volumeId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVolumeWithID, volumeId))
	request, err := http.NewRequestWithContext(ctx, "DELETE", BASE_URL_V1+volumeId, nil)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogConstructedDeleteVolumeRequest)

	// Detach this volume if possible
	// Verify the volume is attached to a virtual machine before attempting to detach it
	getVolumeResponse, err := GetVolume(gpcnClient, ctx, volumeId)
	if err != nil {
		return err
	}
	if getVolumeResponse.Data.VirtualMachineId != "" {
		err = RemoveVolumeFromVirtualMachine(gpcnClient, ctx, volumeId)
		if err != nil {
			return err
		}
	}
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	tflog.Info(ctx, LogIssuedDeleteVolumeJob)

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

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Delete GPCN Volume", deleteVolumeResponse.Data.JobID)

	if err != nil {
		return err
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyCompletedDeleteVolumeWithID, volumeId))
	return nil
}
