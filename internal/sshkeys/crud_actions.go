package sshkeys

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Response structures for API calls
type createSSHKeyResponse struct {
	Data struct {
		ID         string `json:"id"`
		PrivateKey string `json:"privateKey"`
	} `json:"data"`
}

type readSSHKeyResponse struct {
	Data struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		CreatedAt  string `json:"createdAt"`
		UpdatedAt  string `json:"updatedAt"`
		Algorithm  string `json:"algorithm"`
		PublicKey  string `json:"publicKey"`
		PrivateKey string `json:"privateKey"`
	} `json:"data"`
}

type algorithmEntry struct {
	Code        string `json:"code"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type algorithmsResponse struct {
	Data []algorithmEntry `json:"data"`
}

// CheckAlgorithms verifies that the requested algorithm is supported by the API.
func CheckAlgorithms(gpcnClient *client.GpcnClient, ctx context.Context, algorithm string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingCheckAlgorithms, algorithm))

	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+"algorithms", nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return fmt.Errorf("%s: %w", ErrDetailAlgorithmsCheckFailed, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var algResp algorithmsResponse
	if err = json.Unmarshal(body, &algResp); err != nil {
		return err
	}

	tflog.Info(ctx, LogAlgorithmsRetrievedFromAPI)

	if len(algResp.Data) == 0 {
		return errors.New(ErrDetailMalformedAlgorithmsResponse)
	}

	if slices.ContainsFunc(algResp.Data, func(e algorithmEntry) bool { return e.Code == algorithm }) {
		tflog.Info(ctx, fmt.Sprintf(LogAlgorithmCheckPassed, algorithm))
		return nil
	}

	codes := make([]string, len(algResp.Data))
	for i, e := range algResp.Data {
		codes[i] = e.Code
	}
	return fmt.Errorf(ErrDetailAlgorithmNotSupported, algorithm, codes)
}

// CreateSSHKey creates a new SSH key and returns the resource details.
func CreateSSHKey(gpcnClient *client.GpcnClient, ctx context.Context, plan ResourceModel) (*readSSHKeyResponse, error) {
	tflog.Info(ctx, LogStartingCreateSSHKey)

	requestBody := map[string]any{
		"name": plan.Name.ValueString(),
	}

	if !plan.PublicKey.IsNull() && plan.PublicKey.ValueString() != "" {
		requestBody["type"] = "upload"
		requestBody["publicKey"] = plan.PublicKey.ValueString()
	} else {
		requestBody["type"] = "generate"
		requestBody["algorithm"] = plan.Algorithm.ValueString()
		if !plan.Passphrase.IsNull() && plan.Passphrase.ValueString() != "" {
			requestBody["passphrase"] = plan.Passphrase.ValueString()
		}
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

	// Fetch the full resource details using the returned ID.
	sshKeyResponse, err := GetSSHKey(gpcnClient, ctx, createResp.Data.ID)
	if err != nil {
		return nil, err
	}

	// The private key is only present in the create response; preserve it for state.
	sshKeyResponse.Data.PrivateKey = createResp.Data.PrivateKey

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
