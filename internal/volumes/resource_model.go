package volumes

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	DatacenterId   types.String `tfsdk:"datacenter_id"`
	VolumeType     types.String `tfsdk:"volume_type"`
	VolumeTypeCode types.String `tfsdk:"volume_type_code"`
	VolumeTypeId   types.Int64  `tfsdk:"volume_type_id"`
	SizeGb         types.Int64  `tfsdk:"size_gb"`
	CreatedTime    types.String `tfsdk:"created_time"`
	LastUpdated    types.String `tfsdk:"last_updated"`
	Location       types.Map    `tfsdk:"location"`
}

// Update the plan or state with new values from the GET response
func MapVolumeResponseToModel(ctx context.Context, response *readVolumesResponse, model ResourceModel) ResourceModel {
	// Construct most of the data object
	model.ID = types.StringValue(response.Data.ID)
	// The API identifies a volume type by code and has never sent an id.
	model.VolumeTypeId = types.Int64Null()
	if response.Data.VolumeType.Code == "" {
		model.VolumeTypeCode = types.StringNull()
	} else {
		model.VolumeTypeCode = types.StringValue(response.Data.VolumeType.Code)
	}

	// Construct time entries
	createdTime, err := time.Parse(time.RFC3339, response.Data.CreatedAt)
	if err != nil {
		model.CreatedTime = types.StringValue("unknown")
	} else {
		model.CreatedTime = types.StringValue(createdTime.Format(time.RFC850))
	}
	updatedTime, err := time.Parse(time.RFC3339, response.Data.UpdatedAt)
	if err != nil {
		model.LastUpdated = types.StringValue("unknown")
	} else {
		model.LastUpdated = types.StringValue(updatedTime.Format(time.RFC850))
	}

	// Construct the location object
	var diags diag.Diagnostics
	model.Location, diags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"country":    response.Data.Datacenter.Country,
		"region":     response.Data.Datacenter.Region,
		"datacenter": response.Data.Datacenter.Name,
	})
	if diags.HasError() {
		model.Location = types.MapNull(types.StringType)
	}

	// If model doesn't already have these populated, set them
	model = setModelValuesNotPresent(response, model)

	return model
}

// A refresh of name or size_gb from the API can destroy the volume, so this fills only null
// values. A drifted name and a drifted grow both reconcile by replacement.
func setModelValuesNotPresent(response *readVolumesResponse, model ResourceModel) ResourceModel {
	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	if model.Name.IsNull() && response.Data.Name != "" {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.SizeGb.IsNull() && response.Data.SizeGb != 0 {
		model.SizeGb = types.Int64Value(response.Data.SizeGb)
	}
	if model.VolumeType.IsNull() {
		model.VolumeType = importedVolumeType(response.Data.VolumeType)
	}
	return model
}

// A volume can have no code, and the API names such a volume "Unknown".
func importedVolumeType(volumeType volumeTypeResponse) types.String {
	if volumeType.Code != "" {
		return types.StringValue(volumeTypeAliasForCode(volumeType.Code))
	}
	if volumeType.Name != "" {
		return types.StringValue(volumeType.Name)
	}
	return types.StringNull()
}

// A configuration spells a built-in storage class by its alias. An import that
// writes the code instead plans a replacement.
func volumeTypeAliasForCode(code string) string {
	for alias, aliasCode := range volumeTypeMapping {
		if aliasCode == code {
			return alias
		}
	}
	return code
}

// An alias can arrive in any case. The mapping lookup takes the normalised key.
func canonicalVolumeType(name string) string {
	for alias := range volumeTypeMapping {
		if strings.EqualFold(alias, name) {
			return alias
		}
	}
	return name
}

// A display name becomes its code, and a code is already one.
func componentCodeForVolumeType(volumeType string) string {
	if code, known := volumeTypeMapping[canonicalVolumeType(volumeType)]; known {
		return code
	}
	return volumeType
}
