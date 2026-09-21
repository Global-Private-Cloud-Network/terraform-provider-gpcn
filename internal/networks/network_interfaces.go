package networks

import (
	"bytes"
	"cmp"
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

// Every string except the world discriminator is a pointer, because the API sends JSON
// null for a column that the interface's own world does not populate. A bare string
// would land in state as an empty string, which reads as a value GPCN never sent.
type ReadVirtualMachineNetworkDataResponse struct {
	ID               string  `json:"id"`
	NetworkInterface int64   `json:"networkInterface"`
	IsPrimary        int64   `json:"isPrimary"`
	MacAddress       *string `json:"macAddress"`
	PublicIP         *string `json:"publicIp"`
	PublicIPID       *string `json:"publicIpId"`
	PrivateIP        *string `json:"privateIp"`
	World            string  `json:"world"`
	NetworkName      *string `json:"networkName"`
	NetworkID        *string `json:"networkId"`
	CIDRBlock        *string `json:"cidrBlock"`
	GatewayIP        *string `json:"gatewayIp"`
	NetworkType      *string `json:"networkType"`
	VpcSubnetID      *string `json:"vpcSubnetId"`
	SubnetName       *string `json:"subnetName"`
	VpcID            *string `json:"vpcId"`
	VpcName          *string `json:"vpcName"`
	L2SegmentID      *string `json:"l2SegmentId"`
	L2SegmentName    *string `json:"l2SegmentName"`
}
type ReadVirtualMachineNetworkDataResponseTF struct {
	ID               types.String `tfsdk:"id"`
	NetworkInterface types.Int64  `tfsdk:"network_interface"`
	IsPrimary        types.Bool   `tfsdk:"is_primary"`
	MacAddress       types.String `tfsdk:"mac_address"`
	PublicIP         types.String `tfsdk:"public_ip"`
	PublicIPID       types.String `tfsdk:"public_ip_id"`
	PrivateIP        types.String `tfsdk:"private_ip"`
	World            types.String `tfsdk:"world"`
	NetworkName      types.String `tfsdk:"network_name"`
	NetworkID        types.String `tfsdk:"network_id"`
	CIDRBlock        types.String `tfsdk:"cidr_block"`
	GatewayIP        types.String `tfsdk:"gateway_ip"`
	NetworkType      types.String `tfsdk:"network_type"`
	VpcSubnetID      types.String `tfsdk:"vpc_subnet_id"`
	SubnetName       types.String `tfsdk:"subnet_name"`
	VpcID            types.String `tfsdk:"vpc_id"`
	VpcName          types.String `tfsdk:"vpc_name"`
	L2SegmentID      types.String `tfsdk:"l2_segment_id"`
	L2SegmentName    types.String `tfsdk:"l2_segment_name"`
}

func (o ReadVirtualMachineNetworkDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                types.StringType,
		"network_interface": types.Int64Type,
		"is_primary":        types.BoolType,
		"mac_address":       types.StringType,
		"public_ip":         types.StringType,
		"public_ip_id":      types.StringType,
		"private_ip":        types.StringType,
		"world":             types.StringType,
		"network_name":      types.StringType,
		"network_id":        types.StringType,
		"cidr_block":        types.StringType,
		"gateway_ip":        types.StringType,
		"network_type":      types.StringType,
		"vpc_subnet_id":     types.StringType,
		"subnet_name":       types.StringType,
		"vpc_id":            types.StringType,
		"vpc_name":          types.StringType,
		"l2_segment_id":     types.StringType,
		"l2_segment_name":   types.StringType,
	}
}

// nullableString keeps a JSON null out of state as a null, not an empty string.
func nullableString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
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

func GetVirtualMachinesAttachedToNetworks(gpcnClient *client.GpcnClient, ctx context.Context, networkId string) (*readNetworksToVMsResponse, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVirtualMachinesAttachedToNetworks, networkId))
	request, err := http.NewRequestWithContext(ctx, "GET", BASE_URL_V1+networkId+"/virtual-machines", nil)
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

	var ntwkToVMsResp readNetworksToVMsResponse
	err = json.Unmarshal(body, &ntwkToVMsResp)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVirtualMachinesAttachedToNetworks, networkId))
	return &ntwkToVMsResp, nil
}

// Fetch all network interfaces attached to the VM
func GetNetworkInterfaces(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string) ([]ReadVirtualMachineNetworkDataResponseTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetNetworkInterfacesWithID, virtualMachineId))
	request, err := http.NewRequestWithContext(ctx, "GET", VIRTUAL_MACHINES_BASE_URL_V1+virtualMachineId+"/network-interfaces", nil)
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

	var vmNetworkResp readVirtualMachineNetworkResponse
	err = json.Unmarshal(body, &vmNetworkResp)

	if err != nil {
		return nil, err
	}

	var networkInterfaces []ReadVirtualMachineNetworkDataResponseTF
	for _, inter := range vmNetworkResp.Data {
		networkInterfaces = append(networkInterfaces, ReadVirtualMachineNetworkDataResponseTF{
			ID:               types.StringValue(inter.ID),
			NetworkInterface: types.Int64Value(inter.NetworkInterface),
			IsPrimary:        types.BoolValue(inter.IsPrimary == 1),
			MacAddress:       nullableString(inter.MacAddress),
			PublicIP:         nullableString(inter.PublicIP),
			PublicIPID:       nullableString(inter.PublicIPID),
			PrivateIP:        nullableString(inter.PrivateIP),
			World:            types.StringValue(inter.World),
			NetworkName:      nullableString(inter.NetworkName),
			NetworkID:        nullableString(inter.NetworkID),
			CIDRBlock:        nullableString(inter.CIDRBlock),
			GatewayIP:        nullableString(inter.GatewayIP),
			NetworkType:      nullableString(inter.NetworkType),
			VpcSubnetID:      nullableString(inter.VpcSubnetID),
			SubnetName:       nullableString(inter.SubnetName),
			VpcID:            nullableString(inter.VpcID),
			VpcName:          nullableString(inter.VpcName),
			L2SegmentID:      nullableString(inter.L2SegmentID),
			L2SegmentName:    nullableString(inter.L2SegmentName),
		})
	}

	// Sort for a stable order
	slices.SortStableFunc(networkInterfaces, func(a, b ReadVirtualMachineNetworkDataResponseTF) int {
		if c := cmp.Compare(a.NetworkInterface.ValueInt64(), b.NetworkInterface.ValueInt64()); c != 0 {
			return c
		}
		return cmp.Compare(a.ID.ValueString(), b.ID.ValueString())
	})

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedAllNetworkInterfaces, virtualMachineId))
	return networkInterfaces, nil
}

// Attach a network interface to the virtual machine
func AddNetworkInterface(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, networkId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAddNetworkInterfaceWithIDs, virtualMachineId, networkId))
	attachNetworkInterfaceRequestBody := map[string]string{
		"networkId": networkId,
	}

	jsonAttachNetworkInterfaceRequestBody, err := json.Marshal(attachNetworkInterfaceRequestBody)
	if err != nil {
		return errors.New("error marshaling the json request body GPCN Virtual Machines - Attach Network")
	}
	request, err := http.NewRequestWithContext(ctx, "POST", VIRTUAL_MACHINES_BASE_URL_V1+virtualMachineId+"/network-interfaces", bytes.NewBuffer(jsonAttachNetworkInterfaceRequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
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

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Add GPCN Network Interface to Virtual Machine", addNetworkInterfaceResponse.Data.JobID)

	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyAttachedNetworkInterface)
	return nil
}

// The caller passes only the candidates that survive the update.
// The preferred network ID names the primary that the practitioner configured.
func SetNextNetworkInterfaceToPrimary(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineID, preferredNetworkID string, candidateNetworkInterfaces []ReadVirtualMachineNetworkDataResponseTF) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingSetNextNetworkInterfaceToPrimary, virtualMachineID))

	networkInterfaceIdx := -1
	if preferredNetworkID != "" {
		networkInterfaceIdx = slices.IndexFunc(candidateNetworkInterfaces, func(networkInterface ReadVirtualMachineNetworkDataResponseTF) bool {
			return strings.EqualFold(networkInterface.NetworkID.ValueString(), preferredNetworkID)
		})
	}
	if networkInterfaceIdx < 0 {
		// The backend promotes a replacement primary on its own. Accept that choice, because
		// the practitioner named no interface of their own.
		if slices.ContainsFunc(candidateNetworkInterfaces, func(networkInterface ReadVirtualMachineNetworkDataResponseTF) bool {
			return networkInterface.IsPrimary.ValueBool()
		}) {
			tflog.Info(ctx, fmt.Sprintf(LogNetworkInterfaceAlreadyPrimary, virtualMachineID))
			return nil
		}
		networkInterfaceIdx = slices.IndexFunc(candidateNetworkInterfaces, func(networkInterface ReadVirtualMachineNetworkDataResponseTF) bool {
			return !networkInterface.IsPrimary.ValueBool()
		})
	}
	if networkInterfaceIdx < 0 {
		return errors.New(ErrDetailNoCandidateNetworkInterface)
	}
	if candidateNetworkInterfaces[networkInterfaceIdx].IsPrimary.ValueBool() {
		tflog.Info(ctx, fmt.Sprintf(LogNetworkInterfaceAlreadyPrimary, virtualMachineID))
		return nil
	}

	updateNetworkInterfaceRequestBody := map[string]bool{
		"setPrimary": true,
	}

	jsonUpdateNetworkInterfaceRequestBody, err := json.Marshal(updateNetworkInterfaceRequestBody)
	if err != nil {
		return errors.New("error marshaling the json request body GPCN Virtual Machines - Update Primary Interface")
	}

	nextPrimaryNetworkInterfaceID := candidateNetworkInterfaces[networkInterfaceIdx].ID.ValueString()
	tflog.Info(ctx, fmt.Sprintf(LogSettingNetworkInterfaceAsPrimary, nextPrimaryNetworkInterfaceID))
	request, err := http.NewRequestWithContext(ctx, "PUT", VIRTUAL_MACHINES_BASE_URL_V1+virtualMachineID+"/network-interfaces/"+nextPrimaryNetworkInterfaceID, bytes.NewBuffer(jsonUpdateNetworkInterfaceRequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullySetNetworkInterfaceAsPrimary, nextPrimaryNetworkInterfaceID))
	return nil
}

// Remove a network interface from the virtual machine
func RemoveNetworkInterface(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, networkInterfaceId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingRemoveNetworkInterfaceWithIDs, virtualMachineId, networkInterfaceId))
	request, err := http.NewRequestWithContext(ctx, "DELETE", VIRTUAL_MACHINES_BASE_URL_V1+virtualMachineId+"/network-interfaces/"+networkInterfaceId, nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
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

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Remove GPCN Network Interface from Virtual Machine", removeNetworkInterfaceResponse.Data.JobID)

	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRemovedNetworkInterface, networkInterfaceId))
	// If we got no errors, this should be a successful detach
	return nil
}

func RemoveNetworkInterfaceByNetworkId(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, networkId string) error {
	// Using the virtual machine id, find the corresponding networkId
	// No GET :id endpoint, use the list and find it
	networkInterfaces, err := GetNetworkInterfaces(gpcnClient, ctx, virtualMachineId)
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
		return fmt.Errorf(ErrDetailRemoveNetworkInterfaceFailed, networkId)
	}

	// If it does, remove it
	err = RemoveNetworkInterface(gpcnClient, ctx, virtualMachineId, networkInterfaceId)
	if err != nil {
		return err
	}

	return nil
}

func AllocatePublicIp(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, networkInterfaceId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAllocatePublicIp, virtualMachineId, networkInterfaceId))
	request, err := http.NewRequestWithContext(ctx, "POST", VIRTUAL_MACHINES_BASE_URL_V1+virtualMachineId+"/network-interfaces/"+networkInterfaceId+"/public-ip", nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
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

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Allocate Public IP Address", allocatePublicIpResponse.Data.JobID)
	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyAllocatedPublicIp, virtualMachineId, networkInterfaceId))
	return nil
}

func ReleasePublicIp(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, networkInterfaceId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingReleasePublicIp, virtualMachineId, networkInterfaceId))
	request, err := http.NewRequestWithContext(ctx, "DELETE", VIRTUAL_MACHINES_BASE_URL_V1+virtualMachineId+"/network-interfaces/"+networkInterfaceId+"/public-ip", nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Read the response body and process it as releasePublicIpResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var releasePublicIpResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &releasePublicIpResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Release Public IP Address", releasePublicIpResponse.Data.JobID)
	if err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyReleasedPublicIp, virtualMachineId, networkInterfaceId))
	return nil
}

// Helper function to consolidate logic for adding and removing network interfaces for a virtual machine
func UpdateNetworkInterfaces(gpcnClient *client.GpcnClient, ctx context.Context, vmId string, oldNetworksList, newNetworksList []string, networkInterfaces []ReadVirtualMachineNetworkDataResponseTF) error {
	tflog.Info(ctx, "NetworkIds have changed, performing detaches and attaches in that order")

	addedValues, removedValues := helpers.CheckListForDifferences(oldNetworksList, newNetworksList)
	tflog.Info(ctx, fmt.Sprintf("NetworkIds to be removed are: [%s]", strings.Join(removedValues, ", ")))
	tflog.Info(ctx, fmt.Sprintf("NetworkIds to be added are: [%s]", strings.Join(addedValues, ", ")))

	// The practitioner configures the first network ID as the primary.
	preferredNetworkID := ""
	if len(newNetworksList) > 0 {
		preferredNetworkID = newNetworksList[0]
	}

	isRemoved := func(data ReadVirtualMachineNetworkDataResponseTF) bool {
		return slices.ContainsFunc(removedValues, func(val string) bool {
			return namesNetwork(data, val)
		})
	}

	primaryIsRemoved := slices.ContainsFunc(networkInterfaces, func(data ReadVirtualMachineNetworkDataResponseTF) bool {
		return data.IsPrimary.ValueBool() && isRemoved(data)
	})

	// A promotion must not land on an interface that this call removes moments later.
	handoffIsComplete := false
	if primaryIsRemoved && len(newNetworksList) > 0 {
		var survivingInterfaces []ReadVirtualMachineNetworkDataResponseTF
		for _, data := range networkInterfaces {
			if !isRemoved(data) {
				survivingInterfaces = append(survivingInterfaces, data)
			}
		}
		// The configured primary keeps its interface only when the network survives.
		if slices.ContainsFunc(survivingInterfaces, func(data ReadVirtualMachineNetworkDataResponseTF) bool {
			return namesNetwork(data, preferredNetworkID)
		}) {
			err := SetNextNetworkInterfaceToPrimary(gpcnClient, ctx, vmId, preferredNetworkID, survivingInterfaces)
			if err != nil {
				return fmt.Errorf(ErrDetailReplacePrimaryInterfaceFailed, err)
			}
			handoffIsComplete = true
		}
	}

	// Do removals first, since there is a cap of 5 networks
	for _, val := range removedValues {
		interfaceIdx := slices.IndexFunc(networkInterfaces, func(data ReadVirtualMachineNetworkDataResponseTF) bool {
			return namesNetwork(data, val)
		})
		if interfaceIdx < 0 {
			continue
		}
		tflog.Info(ctx, fmt.Sprintf("Removing network interface for ID %s", val))
		err := RemoveNetworkInterface(gpcnClient, ctx, vmId, networkInterfaces[interfaceIdx].ID.ValueString())
		if err != nil {
			return fmt.Errorf("error removing network interface with ID %s: %w", val, err)
		}
	}

	// Add new network interfaces
	for _, val := range addedValues {
		tflog.Info(ctx, fmt.Sprintf("Adding network interface for ID %s", val))
		err := AddNetworkInterface(gpcnClient, ctx, vmId, val)
		if err != nil {
			return fmt.Errorf("error adding network interface with ID %s: %w", val, err)
		}
	}

	// The schema promises a handoff only when the removal takes the primary network away.
	if !primaryIsRemoved || handoffIsComplete || len(newNetworksList) == 0 {
		return nil
	}
	tflog.Info(ctx, fmt.Sprintf(LogPromotingAddedNetworkInterface, vmId))

	refreshedInterfaces, err := GetNetworkInterfaces(gpcnClient, ctx, vmId)
	if err != nil {
		return fmt.Errorf(ErrDetailRefreshNetworkInterfacesFailed, vmId, err)
	}
	// A delete that is still in flight keeps a removed interface in the refreshed list.
	var configuredInterfaces []ReadVirtualMachineNetworkDataResponseTF
	for _, data := range refreshedInterfaces {
		if slices.ContainsFunc(newNetworksList, func(val string) bool {
			return namesNetwork(data, val)
		}) {
			configuredInterfaces = append(configuredInterfaces, data)
		}
	}
	// An attach that the refresh misses leaves nothing to promote.
	// Nothing retries this until the network_ids attribute changes again.
	if len(configuredInterfaces) == 0 {
		tflog.Warn(ctx, fmt.Sprintf(LogNoConfiguredNetworkInterfaceAfterRefresh, vmId))
		return nil
	}

	err = SetNextNetworkInterfaceToPrimary(gpcnClient, ctx, vmId, preferredNetworkID, configuredInterfaces)
	if err != nil {
		return fmt.Errorf(ErrDetailReplacePrimaryInterfaceFailed, err)
	}

	return nil
}

func namesNetwork(data ReadVirtualMachineNetworkDataResponseTF, networkID string) bool {
	return strings.EqualFold(data.NetworkID.ValueString(), networkID)
}
