package gpu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"terraform-provider-gpcn/internal/client"

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
	} `json:"data"`
}

func CreateGPU(httpClient *http.Client, ctx context.Context, seriesId string, plan ResourceModel) (*readGPUResponse, error) {
	tflog.Info(ctx, LogStartingCreateGPU)

	// Create a new request from the model
	createGPURequestBody := map[string]any{
		"datacenterId": plan.DatacenterId.ValueString(),
		"seriesId":     seriesId,
		"gpuCount":     plan.GPUCount.ValueInt64(),
		"name":         plan.Name.ValueString(),
		"imageName":    plan.ImageName.ValueString(),
	}

	jsonCreateGPURequestBody, err := json.Marshal(createGPURequestBody)
	if err != nil {
		return nil, err
	}

	// Create API request
	request, err := http.NewRequest("POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateGPURequestBody))
	if err != nil {
		return nil, err
	}

	// Perform API request
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateGPUJob)

	// Read the response body and process it as JobStatusMultiResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	_ = response.Body.Close()

	var createGPUResponse client.JobStatusMultiResponse
	err = json.Unmarshal(body, &createGPUResponse)
	if err != nil {
		return nil, err
	}

	// Perform long polling to wait for job completion
	jobResp, err := client.PerformLongPolling(httpClient, ctx, "Create GPCN GPU", createGPUResponse.Data.Jobs[0].JobID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateGPU)

	// Get the GPU details after creation
	getGPUResponse, err := GetGPU(httpClient, ctx, jobResp.Data.Jobs[0].ResourceId)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedCreateGPU)
	return getGPUResponse, nil
}

func GetGPU(httpClient *http.Client, ctx context.Context, id string) (*readGPUResponse, error) {
	tflog.Info(ctx, LogStartingReadGPU)

	request, err := http.NewRequest("GET", BASE_URL_V1+id, nil)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	_ = response.Body.Close()

	var readGPUResponse readGPUResponse
	err = json.Unmarshal(body, &readGPUResponse)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedReadGPU)
	return &readGPUResponse, nil
}

func UpdateGPU(httpClient *http.Client, ctx context.Context, id, name string) error {
	tflog.Info(ctx, LogStartingUpdateGPU)

	// Create a new request with the name
	updateGPURequestBody := map[string]any{
		"name": name,
	}

	jsonUpdateGPURequestBody, err := json.Marshal(updateGPURequestBody)
	if err != nil {
		return err
	}

	request, err := http.NewRequest("PUT", BASE_URL_V1+id, bytes.NewBuffer(jsonUpdateGPURequestBody))
	if err != nil {
		return err
	}

	_, err = httpClient.Do(request)
	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedUpdateGPU)
	return nil
}

func DeleteGPU(httpClient *http.Client, ctx context.Context, id string) error {
	tflog.Info(ctx, LogStartingDeleteGPU)

	request, err := http.NewRequest("DELETE", BASE_URL_V1+id, nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteGPUJob)

	// Read the response body and process it as JobStatusSingularResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	_ = response.Body.Close()

	var deleteGPUResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &deleteGPUResponse)
	if err != nil {
		return err
	}

	// Perform long polling to wait for job completion
	_, err = client.PerformLongPolling(httpClient, ctx, "Delete GPCN GPU", deleteGPUResponse.Data.JobID)
	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedDeleteGPU)
	return nil
}
