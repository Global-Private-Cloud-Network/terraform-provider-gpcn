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

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type readVirtualMachineNetworkResponse struct {
	Success bool                                    `json:"success"`
	Message string                                  `json:"message"`
	Data    []ReadVirtualMachineNetworkDataResponse `json:"data"`
}

// The world discriminator the interface projection carries. It names which linkage kind
// an interface has, and therefore which identity attributes are live.
const (
	NicWorldLegacy = "legacy"
	NicWorldVpc    = "vpc"
	NicWorldL2     = "l2"
)

// Every string except the world discriminator is a pointer. The API sends JSON null for
// a column that the interface's own world does not populate. A bare string would land in
// state as an empty string, which reads as a value GPCN never sent.
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

// AddL2SegmentInterface attaches an L2 segment to the virtual machine as a new
// interface. GPCN accepts exactly one target key, and l2SegmentId names a segment.
func AddL2SegmentInterface(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, l2SegmentId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAddL2SegmentInterfaceWithIDs, virtualMachineId, l2SegmentId))
	attachNetworkInterfaceRequestBody := map[string]string{
		"l2SegmentId": l2SegmentId,
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
