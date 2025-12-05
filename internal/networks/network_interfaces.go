package networks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/helpers"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type readVirtualMachineNetworkResponse struct {
	Success bool                                    `json:"success"`
	Message string                                  `json:"message"`
	Data    []ReadVirtualMachineNetworkDataResponse `json:"data"`
}
type ReadVirtualMachineNetworkDataResponse struct {
	ID               string `json:"id"`
	NetworkInterface int64  `json:"networkInterface"`
	IsPrimary        int64  `json:"isPrimary"`
	PublicIP         string `json:"publicIp"`
	PublicIPID       string `json:"publicIpId"`
	PrivateIP        string `json:"privateIp"`
	NetworkName      string `json:"networkName"`
	NetworkID        string `json:"networkId"`
	CIDRBlock        string `json:"cidrBlock"`
	GatewayIP        string `json:"gatewayIp"`
	NetworkType      string `json:"networkType"`
}
type ReadVirtualMachineNetworkDataResponseTF struct {
	ID               types.String `tfsdk:"id"`
	NetworkInterface types.Int64  `tfsdk:"network_interface"`
	IsPrimary        types.Int64  `tfsdk:"is_primary"`
	PublicIP         types.String `tfsdk:"public_ip"`
	PublicIPID       types.String `tfsdk:"public_ip_id"`
	PrivateIP        types.String `tfsdk:"private_ip"`
	NetworkName      types.String `tfsdk:"network_name"`
	NetworkID        types.String `tfsdk:"network_id"`
	CIDRBlock        types.String `tfsdk:"cidr_block"`
	GatewayIP        types.String `tfsdk:"gateway_ip"`
	NetworkType      types.String `tfsdk:"network_type"`
}

func (o ReadVirtualMachineNetworkDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                types.StringType,
		"network_interface": types.Int64Type,
		"is_primary":        types.Int64Type,
		"public_ip":         types.StringType,
		"public_ip_id":      types.StringType,
		"private_ip":        types.StringType,
		"network_name":      types.StringType,
		"network_id":        types.StringType,
		"cidr_block":        types.StringType,
		"gateway_ip":        types.StringType,
		"network_type":      types.StringType,
	}
}

type readNetworksToVMsResponse struct {
	Success bool                            `json:"success"`
	Message string                          `json:"message"`
	Data    []readNetworksToVMsDataResponse `json:"data"`
}
type readNetworksToVMsDataResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MachineId string `json:"machineId"`
	PublicIp  string `json:"publicIp"`
	PrivateIp string `json:"privateIp"`
	NetworkId string `json:"networkId"`
}

func GetVirtualMachinesAttachedToNetworks(httpClient *http.Client, ctx context.Context, networkId string) (*readNetworksToVMsResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVirtualMachinesAttachedToNetworks, networkId))
	request, err := http.NewRequest("GET", BASE_URL+networkId+"/virtual-machines", nil)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var readNetworksToVMsResponse readNetworksToVMsResponse
	err = json.Unmarshal(body, &readNetworksToVMsResponse)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVirtualMachinesAttachedToNetworks, networkId))
	return &readNetworksToVMsResponse, nil
}

// Fetch all network interfaces attached to the VM
func GetNetworkInterfaces(httpClient *http.Client, ctx context.Context, virtualMachineId string) ([]ReadVirtualMachineNetworkDataResponseTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetNetworkInterfacesWithID, virtualMachineId))
	request, err := http.NewRequest("GET", VIRTUAL_MACHINES_BASE_URL+virtualMachineId+"/network-interfaces", nil)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var readVirtualMachineNetworkResponse readVirtualMachineNetworkResponse
	err = json.Unmarshal(body, &readVirtualMachineNetworkResponse)

	if err != nil {
		return nil, err
	}

	var networkInterfaces []ReadVirtualMachineNetworkDataResponseTF
	for _, inter := range readVirtualMachineNetworkResponse.Data {
		networkInterfaces = append(networkInterfaces, ReadVirtualMachineNetworkDataResponseTF{
			ID:               types.StringValue(inter.ID),
			NetworkInterface: types.Int64Value(inter.NetworkInterface),
			IsPrimary:        types.Int64Value(inter.IsPrimary),
			PublicIP:         types.StringValue(inter.PublicIP),
			PublicIPID:       types.StringValue(inter.PublicIPID),
			PrivateIP:        types.StringValue(inter.PrivateIP),
			NetworkName:      types.StringValue(inter.NetworkName),
			NetworkID:        types.StringValue(inter.NetworkID),
			CIDRBlock:        types.StringValue(inter.CIDRBlock),
			GatewayIP:        types.StringValue(inter.GatewayIP),
			NetworkType:      types.StringValue(inter.NetworkType),
		})
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedAllNetworkInterfaces, virtualMachineId))
	return networkInterfaces, nil
}

// Attach a network interface to the virtual machine
func AddNetworkInterface(httpClient *http.Client, ctx context.Context, virtualMachineId, networkId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAddNetworkInterfaceWithIDs, virtualMachineId, networkId))
	attachNetworkInterfaceRequestBody := map[string]string{
		"networkId": networkId,
	}

	jsonAttachNetworkInterfaceRequestBody, err := json.Marshal(attachNetworkInterfaceRequestBody)
	if err != nil {
		return errors.New("error marshaling the json request body GPCN Virtual Machines - Attach Network")
	}
	request, err := http.NewRequest("POST", VIRTUAL_MACHINES_BASE_URL+virtualMachineId+"/network-interfaces", bytes.NewBuffer(jsonAttachNetworkInterfaceRequestBody))
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var addNetworkInterfaceResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &addNetworkInterfaceResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(httpClient, ctx, "Add GPCN Network Interface to Virtual Machine", addNetworkInterfaceResponse.Data.JobID)

	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyAttachedNetworkInterface)
	return nil
}

// Attach a network interface to the virtual machine
func SetNextNetworkInterfaceToPrimary(httpClient *http.Client, ctx context.Context, virtualMachineId string, allNetworkInterfaces []ReadVirtualMachineNetworkDataResponseTF) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingSetNextNetworkInterfaceToPrimary, virtualMachineId))
	updateNetworkInterfaceRequestBody := map[string]bool{
		"setPrimary": true,
	}

	jsonUpdateNetworkInterfaceRequestBody, err := json.Marshal(updateNetworkInterfaceRequestBody)
	if err != nil {
		return errors.New("error marshaling the json request body GPCN Virtual Machines - Update Primary Interface")
	}
	// Find the next interface in the list that is not the previous primary
	networkInterfaceIdx := slices.IndexFunc(allNetworkInterfaces, func(networkInterface ReadVirtualMachineNetworkDataResponseTF) bool {
		return networkInterface.IsPrimary.ValueInt64() != 1
	})
	if networkInterfaceIdx < -1 {
		return errors.New("no network interfaces found that were not marked as primary")
	}
	nextPrimaryNetworkInterfaceId := allNetworkInterfaces[networkInterfaceIdx].ID.ValueString()
	tflog.Info(ctx, fmt.Sprintf(LogSettingNetworkInterfaceAsPrimary, nextPrimaryNetworkInterfaceId))
	request, err := http.NewRequest("PUT", VIRTUAL_MACHINES_BASE_URL+virtualMachineId+"/network-interfaces/"+nextPrimaryNetworkInterfaceId, bytes.NewBuffer(jsonUpdateNetworkInterfaceRequestBody))
	if err != nil {
		return err
	}

	_, err = httpClient.Do(request)
	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullySetNetworkInterfaceAsPrimary, nextPrimaryNetworkInterfaceId))
	return nil
}

// Remove a network interface from the virtual machine
func RemoveNetworkInterface(httpClient *http.Client, ctx context.Context, virtualMachineId, networkInterfaceId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingRemoveNetworkInterfaceWithIDs, virtualMachineId, networkInterfaceId))
	request, err := http.NewRequest("DELETE", VIRTUAL_MACHINES_BASE_URL+virtualMachineId+"/network-interfaces/"+networkInterfaceId, nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	// Read the response body and process it as removeNetworkInterfaceResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var removeNetworkInterfaceResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &removeNetworkInterfaceResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(httpClient, ctx, "Remove GPCN Network Interface from Virtual Machine", removeNetworkInterfaceResponse.Data.JobID)

	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRemovedNetworkInterface, networkInterfaceId))
	// If we got no errors, this should be a successful detach
	return nil
}

func RemoveNetworkInterfaceByNetworkId(httpClient *http.Client, ctx context.Context, virtualMachineId, networkId string) error {
	// Using the virtual machine id, find the corresponding networkId
	// No GET :id endpoint, use the list and find it
	networkInterfaces, err := GetNetworkInterfaces(httpClient, ctx, virtualMachineId)
	if err != nil {
		return err
	}

	var networkInterfaceId string
	for _, inter := range networkInterfaces {
		if inter.NetworkID.ValueString() == networkId {
			networkInterfaceId = inter.ID.ValueString()
		}
	}
	// If the networkId doesn't have a corresponding interface, something went wrong
	if networkInterfaceId == "" {
		return errors.New(ErrDetailRemoveNetworkInterfaceFailed)
	}

	// If it does, remove it
	err = RemoveNetworkInterface(httpClient, ctx, virtualMachineId, networkInterfaceId)
	if err != nil {
		return err
	}

	return nil
}

func AllocatePublicIp(httpClient *http.Client, ctx context.Context, virtualMachineId, networkInterfaceId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAllocatePublicIp, virtualMachineId, networkInterfaceId))
	request, err := http.NewRequest("POST", VIRTUAL_MACHINES_BASE_URL+virtualMachineId+"/network-interfaces/"+networkInterfaceId+"/public-ip", nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Read the response body and process it as allocatePublicIpResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var allocatePublicIpResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &allocatePublicIpResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(httpClient, ctx, "Allocate Public IP Address", allocatePublicIpResponse.Data.JobID)
	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyAllocatedPublicIp, virtualMachineId, networkInterfaceId))
	return nil
}

func ReleasePublicIp(httpClient *http.Client, ctx context.Context, virtualMachineId, networkInterfaceId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingReleasePublicIp, virtualMachineId, networkInterfaceId))
	request, err := http.NewRequest("DELETE", VIRTUAL_MACHINES_BASE_URL+virtualMachineId+"/network-interfaces/"+networkInterfaceId+"/public-ip", nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Read the response body and process it as allocatePublicIpResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var releasePublicIpResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &releasePublicIpResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(httpClient, ctx, "Release Public IP Address", releasePublicIpResponse.Data.JobID)
	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyReleasedPublicIp, virtualMachineId, networkInterfaceId))
	return nil
}

// Helper funtion to consolidate logic for adding and removing network interfaces for a virtual machine
func UpdateNetworkInterfaces(httpClient *http.Client, ctx context.Context, vmId string, oldNetworksList, newNetworksList []string, networkInterfaces []ReadVirtualMachineNetworkDataResponseTF) error {
	tflog.Info(ctx, "NetworkIds have changed, performing detaches and attaches in that order")

	addedValues, removedValues := helpers.CheckListForDifferences(oldNetworksList, newNetworksList)
	tflog.Info(ctx, fmt.Sprintf("NetworkIds to be removed are: [%s]", helpers.JoinStrings(removedValues)))
	tflog.Info(ctx, fmt.Sprintf("NetworkIds to be added are: [%s]", helpers.JoinStrings(addedValues)))

	// Check if any interfaces slated to be removed are the primary interface. If so, make the next interface available the primary
	for _, val := range removedValues {
		interfaceIdx := slices.IndexFunc(networkInterfaces, func(data ReadVirtualMachineNetworkDataResponseTF) bool {
			return strings.EqualFold(data.NetworkID.ValueString(), val)
		})
		if interfaceIdx > -1 && networkInterfaces[interfaceIdx].IsPrimary.ValueInt64() == 1 {
			// Issue a call to set the next interface to be the primary
			err := SetNextNetworkInterfaceToPrimary(httpClient, ctx, vmId, networkInterfaces)
			if err != nil {
				return fmt.Errorf("error replacing primary interface: %w", err)
			}
			break
		}
	}

	// Do removals first, since there is a cap of 5 networks
	for _, val := range removedValues {
		interfaceIdx := slices.IndexFunc(networkInterfaces, func(data ReadVirtualMachineNetworkDataResponseTF) bool {
			return strings.EqualFold(data.NetworkID.ValueString(), val)
		})
		if interfaceIdx < 0 {
			continue
		}
		tflog.Info(ctx, fmt.Sprintf("Removing network interface for ID: %s", val))
		err := RemoveNetworkInterface(httpClient, ctx, vmId, networkInterfaces[interfaceIdx].ID.ValueString())
		if err != nil {
			return fmt.Errorf("error removing network interface with ID %s: %w", val, err)
		}
	}

	// Add new network interfaces
	for _, val := range addedValues {
		tflog.Info(ctx, fmt.Sprintf("Adding network interface for ID: %s", val))
		err := AddNetworkInterface(httpClient, ctx, vmId, val)
		if err != nil {
			return fmt.Errorf("error adding network interface with ID %s: %w", val, err)
		}
	}

	return nil
}
