package networks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type readNetworkResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		CreatedAt    string `json:"createdAt"`
		UpdatedAt    string `json:"updatedAt"`
		SNAT         string `json:"snat"`
		CIDRBlock    string `json:"cidrBlock"`
		Gateway      string `json:"gatewayIp"`
		ConnectedVMs string `json:"connectedVms"`
		NetworkType  string `json:"networkType"`
		Datacenter   struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Region      string `json:"region"`
			CountryAbbr string `json:"countryAbbr"`
			Country     string `json:"country"`
		} `json:"datacenter"`
		DNSServers      string `json:"dnsNameservers"`
		AllocationPools []struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"allocationPools"`
	} `json:"data"`
}

// dnsServersToString joins the dns_servers list into the comma-separated string the API expects.
func dnsServersToString(dnsServers types.List) string {
	if dnsServers.IsNull() || dnsServers.IsUnknown() {
		return ""
	}
	parts := make([]string, len(dnsServers.Elements()))
	for i, el := range dnsServers.Elements() {
		if sv, ok := el.(types.String); ok {
			parts[i] = sv.ValueString()
		}
	}
	return strings.Join(parts, ", ")
}

func CreateNetwork(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) (*readNetworkResponse, error) {
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
		"defaultRoute":           defaultRoute,
		"defaultRouteEnabled":    isStandardNetwork,
		"datacenterId":           model.DatacenterId.ValueString(),
		"description":            model.Description.ValueString(),
		"dhcpStartAddress":       model.DHCPStartAddress.ValueString(),
		"dhcpEndAddress":         model.DHCPEndAddress.ValueString(),
		"dhcpServerEnabled":      isStandardNetwork,
		"dnsServers":             dnsServersToString(model.DNSServers),
		"name":                   model.Name.ValueString(),
		"networkType":            model.NetworkType.ValueString(),
		"serveDNSServersEnabled": isStandardNetwork,
		"snatEnabled":            isStandardNetwork,
	}

	if !model.CIDRBlock.IsNull() && !model.CIDRBlock.IsUnknown() {
		createNetworkRequestBody["cidrBlock"] = model.CIDRBlock.ValueString()
	}

	jsonCreateNetworkRequestBody, err := json.Marshal(createNetworkRequestBody)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateNetworkRequestBody)

	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1, bytes.NewBuffer(jsonCreateNetworkRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedCreateNetworkRequest)

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	tflog.Info(ctx, LogIssuedCreateNetworkJob)

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

	jobResp, err := client.PerformLongPolling(gpcnClient, ctx, "Create GPCN Network", createNetworkResponse.Data.JobID)
	if err != nil {
		return nil, fmt.Errorf("create network polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateNetwork)

	// Extract resource ID with bounds checking
	resourceID, err := client.GetJobResourceID(jobResp)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource ID from create network job response: %w", err)
	}

	// Perform a GET call to retrieve actual information about the Network
	getNetworkResponse, err := GetNetwork(gpcnClient, ctx, resourceID)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedNetworkCreate)
	return getNetworkResponse, nil
}

// Helper function to get a network by ID. Shared between Read and the final action of Create and Delete
func GetNetwork(gpcnClient *client.GpcnClient, ctx context.Context, networkID string) (*readNetworkResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetNetworkWithID, networkID))
	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+networkID, nil)
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

	var networkResp readNetworkResponse
	err = json.Unmarshal(body, &networkResp)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedNetworkWithID, networkID))
	return &networkResp, nil
}

func UpdateNetwork(gpcnClient *client.GpcnClient, ctx context.Context, networkId string, model ResourceModel) (*readNetworkResponse, error) {
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
		"dnsServers":             dnsServersToString(model.DNSServers),
		"name":                   model.Name.ValueString(),
		"serveDNSServersEnabled": isStandardNetwork,
	}

	jsonUpdateNetworkRequestBody, err := json.Marshal(updateNetworkRequestBody)
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateNetworkRequestBody)

	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+model.ID.ValueString(), bytes.NewBuffer(jsonUpdateNetworkRequestBody))
	if err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogConstructedUpdateNetworkRequest)

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	tflog.Info(ctx, LogUpdateRequestSentSuccessfully)
	// Perform a GET call to retrieve actual information about the Network
	getNetworkResponse, err := GetNetwork(gpcnClient, ctx, model.ID.ValueString())
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedNetworkUpdate)
	return getNetworkResponse, nil
}

func DeleteNetwork(gpcnClient *client.GpcnClient, ctx context.Context, networkId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteNetworkWithID, networkId))
	request, err := http.NewRequestWithContext(ctx, "DELETE", BASE_URL_V1+networkId, nil)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogConstructedDeleteNetworkRequest)

	// Detach this network from any virtual machines it may be attached to
	readNetworksToVMsResponse, err := GetVirtualMachinesAttachedToNetworks(gpcnClient, ctx, networkId)
	if err != nil {
		return err
	}

	// Nil check before iterating
	if readNetworksToVMsResponse != nil {
		for _, virtualmachine := range readNetworksToVMsResponse.Data {
			err := RemoveNetworkInterfaceByNetworkId(gpcnClient, ctx, virtualmachine.ID, networkId)
			if err != nil {
				return err
			}
		}
	}

	// It's possible for the delete job to fail if we are deleting it and a virtual machine at the same time
	// If this happens, catch the error and don't process it until we've failed sufficiently enough
	var lastErr error
	for errorCount := 1; errorCount <= DELETE_NETWORK_RETRY_COUNT; errorCount++ {
		// Wait before retry (skip on first attempt)
		if errorCount > 1 {
			time.Sleep(time.Second * 5)
		}

		response, err := gpcnClient.DoWithRetry(request)
		if err != nil {
			lastErr = err
			tflog.Info(ctx, fmt.Sprintf(LogDeleteNetworkFailedRetrying, networkId, errorCount, DELETE_NETWORK_RETRY_COUNT))
			continue
		}
		tflog.Info(ctx, LogIssuedDeleteNetworkJob)

		// Read the response body and process it as deleteNetworkResponse
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}

		var deleteNetworkResponse client.JobStatusSingularResponse
		err = json.Unmarshal(body, &deleteNetworkResponse)

		if err != nil {
			return err
		}

		_, err = client.PerformLongPolling(gpcnClient, ctx, "Delete GPCN Network", deleteNetworkResponse.Data.JobID)

		if err != nil {
			lastErr = err
			tflog.Info(ctx, fmt.Sprintf(LogDeleteNetworkFailedRetrying, networkId, errorCount, DELETE_NETWORK_RETRY_COUNT))
			continue
		}

		// Success - exit the loop
		lastErr = nil
		break
	}

	if lastErr != nil {
		return lastErr
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyCompletedDeleteNetworkWithID, networkId))
	return nil
}
