package resourcegroups

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
type createResourceGroupResponse struct {
	Data struct {
		ID string `json:"resourceGroupId"`
	} `json:"data"`
}

type readResourceGroupResponse struct {
	Data struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		CreatedAt   string `json:"createdAt"`
		UpdatedAt   string `json:"updatedAt"`
	} `json:"data"`
}

// CreateResourceGroup creates a new resource group and returns the resource details.
func CreateResourceGroup(gpcnClient *client.GpcnClient, ctx context.Context, plan ResourceModel) (*readResourceGroupResponse, error) {
	tflog.Info(ctx, LogStartingCreateResourceGroup)

	requestBody := map[string]any{
		"name": plan.Name.ValueString(),
	}

	if !plan.Description.IsNull() {
		requestBody["description"] = plan.Description.ValueString()
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonBody))
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

	var createResp createResourceGroupResponse
	if err = json.Unmarshal(body, &createResp); err != nil {
		return nil, err
	}

	resourceGroupResponse, err := GetResourceGroup(gpcnClient, ctx, createResp.Data.ID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedCreateResourceGroup)
	return resourceGroupResponse, nil
}

// GetResourceGroup retrieves a resource group by ID.
func GetResourceGroup(gpcnClient *client.GpcnClient, ctx context.Context, id string) (*readResourceGroupResponse, error) {
	tflog.Info(ctx, LogStartingReadResourceGroup)

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

	var resourceGroupResp readResourceGroupResponse
	if err = json.Unmarshal(body, &resourceGroupResp); err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedReadResourceGroup)
	return &resourceGroupResp, nil
}

// UpdateResourceGroup updates the name and/or description of a resource group.
func UpdateResourceGroup(gpcnClient *client.GpcnClient, ctx context.Context, id string, plan ResourceModel) error {
	tflog.Info(ctx, LogStartingUpdateResourceGroup)

	updateBody := map[string]any{
		"name": plan.Name.ValueString(),
	}

	if !plan.Description.IsNull() {
		updateBody["description"] = plan.Description.ValueString()
	} else {
		updateBody["description"] = ""
	}

	jsonBody, err := json.Marshal(updateBody)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+id, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	tflog.Info(ctx, LogSuccessfullyFinishedUpdateResourceGroup)
	return nil
}

// DeleteResourceGroup deletes a resource group by ID.
func DeleteResourceGroup(gpcnClient *client.GpcnClient, ctx context.Context, id string) error {
	tflog.Info(ctx, LogStartingDeleteResourceGroup)

	request, err := http.NewRequestWithContext(ctx, "DELETE", BASE_URL_V1+id, nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	tflog.Info(ctx, LogSuccessfullyFinishedDeleteResourceGroup)
	return nil
}
