package virtualmachines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// GetSSHKeyResponse represents the response from the SSH key endpoint
type GetSSHKeyResponse struct {
	Data struct {
		PrivateKey string `json:"privateKey"`
	} `json:"data"`
}

// GetPasswordResponse represents the response from the password endpoint
type GetPasswordResponse struct {
	Data struct {
		SSHPassword string `json:"sshPassword"`
	} `json:"data"`
}

// GetSSHKey retrieves the SSH private key for a Virtual Machine by its ID
func GetSSHKey(httpClient *http.Client, ctx context.Context, virtualMachineId string) (*GetSSHKeyResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetSSHKeyWithID, virtualMachineId))

	request, err := http.NewRequest("GET", BASE_URL_V1+virtualMachineId+"/ssh-key", nil)
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

	var getSSHKeyResponse GetSSHKeyResponse
	err = json.Unmarshal(body, &getSSHKeyResponse)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedSSHKeyWithID, virtualMachineId))
	return &getSSHKeyResponse, nil
}

// GetPassword retrieves the SSH password for a Virtual Machine by its ID
func GetPassword(httpClient *http.Client, ctx context.Context, virtualMachineId string) (*GetPasswordResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetPasswordWithID, virtualMachineId))

	request, err := http.NewRequest("GET", BASE_URL_V1+virtualMachineId+"/password", nil)
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

	var getPasswordResponse GetPasswordResponse
	err = json.Unmarshal(body, &getPasswordResponse)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedPasswordWithID, virtualMachineId))
	return &getPasswordResponse, nil
}
