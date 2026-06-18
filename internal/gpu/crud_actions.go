package gpu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Response structures for API calls
type readGPUResponse struct {
	Data struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		CreatedAt     string `json:"createdAt"`
		UpdatedAt     string `json:"updatedAt"`
		Status        string `json:"status"`
		IP            string `json:"ip"`
		Configuration struct {
			Name     string `json:"name"`
			Code     string `json:"code"`
			GPUCount int64  `json:"gpuCount"`
			CPU      int64  `json:"cpu"`
			RAM      int64  `json:"ram"`
			Disk     int64  `json:"disk"`
		} `json:"configuration"`
		Datacenter struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Region      string `json:"region"`
			CountryAbbr string `json:"countryAbbr"`
			Country     string `json:"country"`
		} `json:"datacenter"`
		SshKeyId string `json:"sshKeyId"`
		Image    string `json:"image"`
	} `json:"data"`
}

func CreateGPU(gpcnClient *client.GpcnClient, ctx context.Context, seriesId string, plan ResourceModel) (*readGPUResponse, error) {
	tflog.Info(ctx, LogStartingCreateGPU)

	// Extract auth configuration
	var auth ResourceModelInitialAuth
	authDiags := plan.InitialAuth.As(ctx, &auth, basetypes.ObjectAsOptions{})
	if authDiags.HasError() {
		return nil, fmt.Errorf("failed to read auth configuration")
	}

	// Create a new request from the model
	createGPURequestBody := map[string]any{
		"datacenterId": plan.DatacenterId.ValueString(),
		"seriesId":     seriesId,
		"gpuCount":     plan.GPUCount.ValueInt64(),
		"name":         plan.Name.ValueString(),
		"imageName":    plan.ImageName.ValueString(),
		"sshKeyId":     auth.SshKeyId.ValueString(),
	}

	jsonCreateGPURequestBody, err := json.Marshal(createGPURequestBody)
	if err != nil {
		return nil, err
	}

	// Create API request
	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateGPURequestBody))
	if err != nil {
		return nil, err
	}

	// Perform API request
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateGPUJob)

	// Read the response body and process it as JobStatusMultiResponse
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var createGPUResponse client.JobStatusMultiResponse
	err = json.Unmarshal(body, &createGPUResponse)
	if err != nil {
		return nil, err
	}

	// Extract job ID with bounds checking
	jobID, err := client.GetJobID(&createGPUResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to get job ID from create GPU response: %w", err)
	}

	// Perform long polling to wait for job completion
	jobResp, err := client.PerformLongPolling(gpcnClient, ctx, "Create GPCN GPU", jobID)
	if err != nil {
		return nil, fmt.Errorf("create GPU polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateGPU)

	// Extract resource ID with bounds checking
	resourceID, err := client.GetJobResourceID(jobResp)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource ID from create GPU job response: %w", err)
	}

	// Get the GPU details after creation
	getGPUResponse, err := GetGPU(gpcnClient, ctx, resourceID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedCreateGPU)
	return getGPUResponse, nil
}

func GetGPU(gpcnClient *client.GpcnClient, ctx context.Context, id string) (*readGPUResponse, error) {
	tflog.Info(ctx, LogStartingReadGPU)

	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+id, nil)
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

	var gpuResp readGPUResponse
	err = json.Unmarshal(body, &gpuResp)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedReadGPU)
	return &gpuResp, nil
}

func UpdateGPU(gpcnClient *client.GpcnClient, ctx context.Context, id, name string) error {
	tflog.Info(ctx, LogStartingUpdateGPU)

	// Create a new request with the name
	updateGPURequestBody := map[string]any{
		"name": name,
	}

	jsonUpdateGPURequestBody, err := json.Marshal(updateGPURequestBody)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+id, bytes.NewBuffer(jsonUpdateGPURequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	tflog.Info(ctx, LogSuccessfullyFinishedUpdateGPU)
	return nil
}

func DeleteGPU(gpcnClient *client.GpcnClient, ctx context.Context, id string) error {
	tflog.Info(ctx, LogStartingDeleteGPU)

	request, err := http.NewRequestWithContext(ctx, "DELETE", BASE_URL_V1+id, nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	tflog.Info(ctx, LogIssuedDeleteGPUJob)

	// Read the response body and process it as JobStatusSingularResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var deleteGPUResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &deleteGPUResponse)
	if err != nil {
		return err
	}

	// Perform long polling to wait for job completion
	_, err = client.PerformLongPolling(gpcnClient, ctx, "Delete GPCN GPU", deleteGPUResponse.Data.JobID)
	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedDeleteGPU)
	return nil
}
