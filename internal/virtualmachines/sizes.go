package virtualmachines

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

// UpdateVirtualMachineSize resizes the VM to the given SKU ID.
func UpdateVirtualMachineSize(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, skuId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMSizeWithID, virtualMachineId))

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
