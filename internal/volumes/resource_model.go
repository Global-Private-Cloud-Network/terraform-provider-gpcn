package volumes

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	DatacenterId types.String `tfsdk:"datacenter_id"`
	VolumeType   types.String `tfsdk:"volume_type"`
	VolumeTypeId types.Int64  `tfsdk:"volume_type_id"`
	SizeGb       types.Int64  `tfsdk:"size_gb"`
	CreatedTime  types.String `tfsdk:"created_time"`
	LastUpdated  types.String `tfsdk:"last_updated"`
	Location     types.Map    `tfsdk:"location"`
}

// Update the plan or state with new values from the GET response
func MapVolumeResponseToModel(ctx context.Context, response *readVolumesResponse, model ResourceModel) ResourceModel {
	// Construct most of the data object
	model.ID = types.StringValue(response.Data.ID)
	model.VolumeTypeId = types.Int64Value(response.Data.VolumeType.ID)

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

func setModelValuesNotPresent(response *readVolumesResponse, model ResourceModel) ResourceModel {
	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.SizeGb.IsNull() {
		model.SizeGb = types.Int64Value(response.Data.SizeGb)
	}
	// The schema accepts only the canonical keys. Import leaves the attribute null
	// when the API sends a name we do not know. Terraform then reports the missing
	// Required attribute instead of a value the validator rejects.
	if model.VolumeType.IsNull() {
		if canonical, found := canonicalVolumeType(response.Data.VolumeType.Name); found {
			model.VolumeType = types.StringValue(canonical)
		}
	}
	return model
}

// Read refreshes only the attributes that change out of band and that Terraform can
// reconcile. The attributes fixed at creation keep the configured value. A vocabulary
// mismatch there plans a destroy and create forever. Only Read calls this function.
func RefreshVolumeModelFromResponse(response *readVolumesResponse, model ResourceModel) ResourceModel {
	// An attribute the API omits must not blank a Required value.
	if response.Data.Name != "" {
		model.Name = types.StringValue(response.Data.Name)
	}
	if response.Data.SizeGb != 0 {
		model.SizeGb = types.Int64Value(response.Data.SizeGb)
	}
	return model
}

func canonicalVolumeType(name string) (string, bool) {
	for key := range volumeTypeMapping {
		if strings.EqualFold(key, name) {
			return key, true
		}
	}
	return "", false
}
