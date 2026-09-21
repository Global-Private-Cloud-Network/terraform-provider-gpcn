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

// The schema accepts a display name only for the aliases the provider knows. The
// API names every other storage class after its code, and calls a degraded one
// "Unknown", so the code is the only import value the schema always accepts.
func importedVolumeType(volumeType volumeTypeResponse) types.String {
	canonical := canonicalVolumeType(volumeType.Name)
	if _, known := volumeTypeMapping[canonical]; known {
		return types.StringValue(canonical)
	}
	if volumeType.Code != "" {
		return types.StringValue(volumeType.Code)
	}
	if canonical != "" {
		return types.StringValue(canonical)
	}
	return types.StringNull()
}

// The API can send the volume type in a different case. The canonical key keeps
// the configured value from planning a replacement. A component code is already
// canonical, because the API and the schema spell it the same way.
func canonicalVolumeType(name string) string {
	for alias := range volumeTypeMapping {
		if strings.EqualFold(alias, name) {
			return alias
		}
	}
	return name
}

// componentCodeForVolumeType answers with the code the API expects. A display name
// becomes its code, and a code is already one.
func componentCodeForVolumeType(volumeType string) string {
	if code, known := volumeTypeMapping[canonicalVolumeType(volumeType)]; known {
		return code
	}
	return volumeType
}
