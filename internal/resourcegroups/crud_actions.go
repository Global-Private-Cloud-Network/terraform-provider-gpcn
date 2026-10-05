package resourcegroups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		IsDefault   bool   `json:"isDefault"`
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

// ResourceGroupSummary is one row of the list endpoint.
type ResourceGroupSummary struct {
	ID          string
	Name        string
	Description string
	IsDefault   bool
}

type listResourceGroupsResponse struct {
	Data []struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Description *string `json:"description"`
		IsDefault   bool    `json:"isDefault"`
	} `json:"data"`
	Meta struct {
		TotalPages int `json:"totalPages"`
	} `json:"meta"`
}

// FetchResourceGroups reads every page of the list. The first answer bounds the
// loop, so a later page cannot extend it.
func FetchResourceGroups(gpcnClient *client.GpcnClient, ctx context.Context) ([]ResourceGroupSummary, error) {
	var groups []ResourceGroupSummary

	for page, totalPages := 1, 1; page <= totalPages; page++ {
		response, err := fetchResourceGroupPage(gpcnClient, ctx, page)
		if err != nil {
			return nil, err
		}
		if page == 1 {
			totalPages = response.Meta.TotalPages
			if totalPages > listPageCap {
				return nil, fmt.Errorf(ErrDetailResourceGroupListTooLong, totalPages, listPageCap)
			}
		}
		for _, row := range response.Data {
			group := ResourceGroupSummary{ID: row.ID, Name: row.Name, IsDefault: row.IsDefault}
			if row.Description != nil {
				group.Description = *row.Description
			}
			groups = append(groups, group)
		}
	}

	return groups, nil
}

func fetchResourceGroupPage(gpcnClient *client.GpcnClient, ctx context.Context, page int) (*listResourceGroupsResponse, error) {
	listURL := fmt.Sprintf("%s?page=%d&limit=%d", BASE_URL_V1, page, listLimit)
	request, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
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

	var parsed listResourceGroupsResponse
	if err = json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}
