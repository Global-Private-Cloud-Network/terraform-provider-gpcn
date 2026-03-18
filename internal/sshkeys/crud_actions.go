package sshkeys

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
type createSSHKeyResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

type readSSHKeyResponse struct {
	Data struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
		PublicKey string `json:"publicKey"`
	} `json:"data"`
}

// CreateSSHKey creates a new SSH key by uploading a public key and returns the resource details.
func CreateSSHKey(gpcnClient *client.GpcnClient, ctx context.Context, plan ResourceModel) (*readSSHKeyResponse, error) {
	tflog.Info(ctx, LogStartingCreateSSHKey)

	requestBody := map[string]any{
		"name":      plan.Name.ValueString(),
		"type":      "upload",
		"publicKey": plan.PublicKey.ValueString(),
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

	var createResp createSSHKeyResponse
	if err = json.Unmarshal(body, &createResp); err != nil {
		return nil, err
	}

	sshKeyResponse, err := GetSSHKey(gpcnClient, ctx, createResp.Data.ID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedCreateSSHKey)
	return sshKeyResponse, nil
}

// GetSSHKey retrieves an SSH key by ID.
func GetSSHKey(gpcnClient *client.GpcnClient, ctx context.Context, id string) (*readSSHKeyResponse, error) {
	tflog.Info(ctx, LogStartingReadSSHKey)

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

	var sshKeyResp readSSHKeyResponse
	if err = json.Unmarshal(body, &sshKeyResp); err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyFinishedReadSSHKey)
	return &sshKeyResp, nil
}

// UpdateSSHKey updates the name of an SSH key.
func UpdateSSHKey(gpcnClient *client.GpcnClient, ctx context.Context, id, name string) error {
	tflog.Info(ctx, LogStartingUpdateSSHKey)

	updateBody := map[string]any{
		"name": name,
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

	tflog.Info(ctx, LogSuccessfullyFinishedUpdateSSHKey)
	return nil
}

// DeleteSSHKey deletes an SSH key by ID.
func DeleteSSHKey(gpcnClient *client.GpcnClient, ctx context.Context, id string) error {
	tflog.Info(ctx, LogStartingDeleteSSHKey)

	request, err := http.NewRequestWithContext(ctx, "DELETE", BASE_URL_V1+id, nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	tflog.Info(ctx, LogSuccessfullyFinishedDeleteSSHKey)
	return nil
}
