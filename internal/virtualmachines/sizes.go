package virtualmachines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type sizesResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		DatacenterId string `json:"datacenterId"`
		Categories   []struct {
			Code  string `json:"code"`
			Name  string `json:"name"`
			Sizes []struct {
				SkuId   string `json:"skuId"`
				Name    string `json:"name"`
				SkuCode string `json:"skuCode"`
				CPU     int64  `json:"cpu"`
				Memory  int64  `json:"ram"`
				Disk    int64  `json:"disk"`
			} `json:"sizes"`
		} `json:"categories"`
	} `json:"data"`
}

type VirtualMachineConfigurationsTF struct {
	SkuId        types.String `tfsdk:"skuId"`
	SkuCode      types.String `tfsdk:"skuCode"`
	Category     types.String `tfsdk:"category"`
	Name         types.String `tfsdk:"name"`
	CPUCores     types.Int64  `tfsdk:"cpu"`
	MemorySizeGB types.Int64  `tfsdk:"memory"`
	DiskSizeGB   types.Int64  `tfsdk:"disk"`
}

func (o VirtualMachineConfigurationsTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"skuId":    types.StringType,
		"skuCode":  types.StringType,
		"category": types.StringType,
		"name":     types.StringType,
		"cpu":      types.Int64Type,
		"memory":   types.Int64Type,
		"disk":     types.Int64Type,
	}
}

// Get virtual machine SKU ID for a given datacenterId
func GetVirtualMachineSizeSkuId(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, virtualMachineSizeName string) (string, []VirtualMachineConfigurationsTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMSizeIDWithName, virtualMachineSizeName))
	request, err := http.NewRequestWithContext(ctx, "GET", DATA_CENTERS_BASE_URL_V1+datacenterId+"/virtual-machine-sizes", nil)
	var sizes []VirtualMachineConfigurationsTF
	if err != nil {
		return "", sizes, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return "", sizes, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", sizes, err
	}

	var sizesResp sizesResponse
	err = json.Unmarshal(body, &sizesResp)

	if err != nil {
		return "", sizes, err
	}

	// Collect all names for error handling later
	var names []string
	for _, category := range sizesResp.Data.Categories {
		for _, size := range category.Sizes {
			sizes = append(sizes, VirtualMachineConfigurationsTF{
				Category:     types.StringValue(category.Name),
				SkuId:        types.StringValue(size.SkuId),
				SkuCode:      types.StringValue(size.SkuCode),
				Name:         types.StringValue(size.Name),
				CPUCores:     types.Int64Value(size.CPU),
				MemorySizeGB: types.Int64Value(size.Memory),
				DiskSizeGB:   types.Int64Value(size.Disk),
			})
			names = append(names, category.Name+" - "+size.Name)
		}
	}
	sizesFormatted := strings.Join(names, ", ")

	// Verify the size specified is available
	sizeIdx := slices.IndexFunc(sizes, func(size VirtualMachineConfigurationsTF) bool {
		return strings.EqualFold(size.Name.ValueString(), virtualMachineSizeName)
	})

	if sizeIdx < 0 {
		return "", sizes, fmt.Errorf(ErrDetailSizeNotAvailableForDatacenterNoCategory, virtualMachineSizeName, sizesFormatted)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMSizeIDWithName, virtualMachineSizeName))
	return sizes[sizeIdx].SkuId.ValueString(), sizes, nil
}

// Helper function to update a VM by ID
func UpdateVirtualMachineSize(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, skuId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMSizeWithID, virtualMachineId))
	// Create a new request from the plan
	updateVMRequestBody := map[string]any{
		"skuId": skuId,
	}

	jsonUpdateVMRequestBody, err := json.Marshal(updateVMRequestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+virtualMachineId+"/size", bytes.NewBuffer(jsonUpdateVMRequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Read the response body and process it as updateVirtualMachineSizeResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var updateVirtualMachineSizeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &updateVirtualMachineSizeResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Update Virtual Machine Size", updateVirtualMachineSizeResponse.Data.JobID)

	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyUpdatedVMSize)
	return nil
}
