package gpu

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	DatacenterId types.String `tfsdk:"datacenter_id"`
	SeriesName   types.String `tfsdk:"series_name"`
	SeriesCode   types.String `tfsdk:"series_code"`
	GPUCount     types.Int64  `tfsdk:"gpu_count"`
	CreatedTime  types.String `tfsdk:"created_time"`
	LastUpdated  types.String `tfsdk:"last_updated"`
	Location     types.Map    `tfsdk:"location"`
}

// Update the plan or state with new values from the GET response
func MapGPUResponseToModel(ctx context.Context, response *readGPUResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)

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
	model.Location, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"country":    response.Data.Datacenter.Country,
		"region":     response.Data.Datacenter.Region,
		"datacenter": response.Data.Datacenter.Name,
	})

	return model
}
