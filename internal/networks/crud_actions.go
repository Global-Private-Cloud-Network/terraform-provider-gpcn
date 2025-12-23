package networks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"terraform-provider-gpcn/internal/client"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type readNetworkResponse struct {
	Success bool                    `json:"success"`
	Message string                  `json:"message"`
	Data    readNetworkDataResponse `json:"data"`
}
type readNetworkDataResponse struct {
	ID              string                                    `json:"id"`
	Name            string                                    `json:"name"`
	Description     string                                    `json:"description"`
	CreatedAt       string                                    `json:"createdAt"`
	UpdatedAt       string                                    `json:"updatedAt"`
	SNAT            string                                    `json:"snat"`
	CIDRBlock       string                                    `json:"cidrBlock"`
	Gateway         string                                    `json:"gatewayIp"`
	ConnectedVMs    string                                    `json:"connectedVms"`
	NetworkType     string                                    `json:"networkType"`
	Country         readNetworkDataLocationResponse           `json:"country"`
	Region          readNetworkDataLocationResponse           `json:"region"`
	Datacenter      readNetworkDataLocationDatacenterResponse `json:"datacenter"`
	DNSServers      string                                    `json:"dnsNameservers"`
	AllocationPools []readNetworkDataAllocationPoolResponse   `json:"allocationPools"`
}
type readNetworkDataLocationResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type readNetworkDataLocationDatacenterResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type readNetworkDataAllocationPoolResponse struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func CreateNetwork(httpClient *http.Client, ctx context.Context, model ResourceModel) (*readNetworkResponse, error) {
	tflog.Info(ctx, LogStartingCreateNetwork)
	isStandardNetwork := model.NetworkType == types.StringValue("standard")
	var defaultRoute string
	if isStandardNetwork {
		defaultRoute = "10.0.0.1"
	} else {
		defaultRoute = ""
	}

	// Create a new request from the model
	createNetworkRequestBody := map[string]any{
		"cidrBlock":              model.CIDRBlock.ValueString(),
		"defaultRoute":           defaultRoute,
		"defaultRouteEnabled":    isStandardNetwork,
		"datacenterId":           model.DatacenterId.ValueString(),
		"description":            model.Description.ValueString(),
		"dhcpStartAddress":       model.DHCPStartAddress.ValueString(),
		"dhcpEndAddress":         model.DHCPEndAddress.ValueString(),
		"dhcpServerEnabled":      isStandardNetwork,
		"dnsServers":             model.DNSServers.ValueString(),
		"name":                   model.Name.ValueString(),
		"networkType":            model.NetworkType.ValueString(),
		"serveDNSServersEnabled": isStandardNetwork,
		"snatEnabled":            isStandardNetwork,
	}

	jsonCreateNetworkRequestBody, err := json.Marshal(createNetworkRequestBody)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateNetworkRequestBody)

	request, err := http.NewRequest("POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateNetworkRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateNetworkRequest)

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateNetworkJob)
	defer response.Body.Close()

	// Read the response body and process it as createNetworkResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var createNetworkResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &createNetworkResponse)

	if err != nil {
		return nil, err
	}

	jobResp, err := client.PerformLongPolling(httpClient, ctx, "Create GPCN Network", createNetworkResponse.Data.JobID)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateNetwork)
	// Perform a GET call to retrieve actual information about the Network
	getNetworkResponse, err := GetNetwork(httpClient, ctx, jobResp.Data.Jobs[0].ResourceId)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedNetworkCreate)
	return getNetworkResponse, nil
}

// Helper function to get a network by Id. Shared between Read and the final action of Create and Delete
func GetNetwork(client *http.Client, ctx context.Context, networkId string) (*readNetworkResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetNetworkWithID, networkId))
	request, err := http.NewRequest("GET", BASE_URL_V1+networkId, nil)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var readNetworkResponse readNetworkResponse
	err = json.Unmarshal(body, &readNetworkResponse)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedNetworkWithID, networkId))
	return &readNetworkResponse, nil
}

func UpdateNetwork(httpClient *http.Client, ctx context.Context, networkId string, model ResourceModel) (*readNetworkResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateNetworkWithID, networkId))
	isStandardNetwork := model.NetworkType == types.StringValue("standard")
	var defaultRoute string
	if isStandardNetwork {
		defaultRoute = "10.0.0.1"
	} else {
		defaultRoute = ""
	}

	// Create a new request from the model
	updateNetworkRequestBody := map[string]any{
		"cidrBlock":              model.CIDRBlock.ValueString(),
		"defaultRoute":           defaultRoute,
		"defaultRouteEnabled":    isStandardNetwork,
		"description":            model.Description.ValueString(),
		"dhcpStartAddress":       model.DHCPStartAddress.ValueString(),
		"dhcpEndAddress":         model.DHCPEndAddress.ValueString(),
		"dhcpServerEnabled":      isStandardNetwork,
		"dnsServers":             model.DNSServers.ValueString(),
		"name":                   model.Name.ValueString(),
		"serveDNSServersEnabled": isStandardNetwork,
		"snatEnabled":            isStandardNetwork,
	}

	jsonUpdateNetworkRequestBody, err := json.Marshal(updateNetworkRequestBody)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateNetworkRequestBody)

	request, err := http.NewRequest("PUT", BASE_URL_V1+model.ID.ValueString(), bytes.NewBuffer(jsonUpdateNetworkRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateNetworkRequest)

	_, err = httpClient.Do(request)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogUpdateRequestSentSuccessfully)
	// Perform a GET call to retrieve actual information about the Network
	getNetworkResponse, err := GetNetwork(httpClient, ctx, model.ID.ValueString())
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedNetworkUpdate)
	return getNetworkResponse, nil
}

func DeleteNetwork(httpClient *http.Client, ctx context.Context, networkId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteNetworkWithID, networkId))
	request, err := http.NewRequest("DELETE", BASE_URL_V1+networkId, nil)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogConstructedDeleteNetworkRequest)

	// Detach this network from any virtual machines it may be attached to
	readNetworksToVMsResponse, err := GetVirtualMachinesAttachedToNetworks(httpClient, ctx, networkId)
	if err != nil {
		return err
	}

	for _, virtualmachine := range readNetworksToVMsResponse.Data {
		err := RemoveNetworkInterfaceByNetworkId(httpClient, ctx, virtualmachine.ID, networkId)
		if err != nil {
			return err
		}
	}

	// It's possible for the delete job to fail if we are deleting it and a virtual machine at the same time
	// If this happens, catch the error and don't process it until we've failed sufficiently enough
	errorCount := 1
	var errString string
	for {
		response, err := httpClient.Do(request)
		if err != nil {
			errorCount += 1
			tflog.Info(ctx, fmt.Sprintf(LogDeleteNetworkFailedRetrying, networkId, errorCount, DELETE_NETWORK_RETRY_COUNT))
			if errorCount > DELETE_NETWORK_RETRY_COUNT {
				errString = err.Error()
				// Wait a few seconds before retrying
				time.Sleep(time.Second * 5)
				break
			}
		}
		tflog.Info(ctx, LogIssuedDeleteNetworkJob)
		defer response.Body.Close()

		// Read the response body and process it as deleteNetworkResponse
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}

		var deleteNetworkResponse client.JobStatusSingularResponse
		err = json.Unmarshal(body, &deleteNetworkResponse)

		if err != nil {
			return err
		}

		_, err = client.PerformLongPolling(httpClient, ctx, "Delete GPCN Network", deleteNetworkResponse.Data.JobID)

		if err != nil {
			errorCount += 1
			tflog.Info(ctx, fmt.Sprintf(LogDeleteNetworkFailedRetrying, networkId, errorCount, DELETE_NETWORK_RETRY_COUNT))
			if errorCount > DELETE_NETWORK_RETRY_COUNT {
				errString = err.Error()
				// Wait a few seconds before retrying
				time.Sleep(time.Second * 5)
				break
			}
		} else {
			// If we got here safely, we can be done
			break
		}
	}

	if errString != "" {
		return errors.New(errString)
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyCompletedDeleteNetworkWithID, networkId))
	return nil
}
