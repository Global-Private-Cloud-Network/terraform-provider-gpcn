package volumeattachments

import "github.com/hashicorp/terraform-plugin-framework/types"

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	VirtualMachineId types.String `tfsdk:"virtual_machine_id"`
	VolumeId         types.String `tfsdk:"volume_id"`
}
