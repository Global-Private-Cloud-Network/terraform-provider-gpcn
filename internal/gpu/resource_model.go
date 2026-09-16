package gpu

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	DatacenterId types.String `tfsdk:"datacenter_id"`
	SeriesName   types.String `tfsdk:"series_name"`
	SeriesCode   types.String `tfsdk:"series_code"`
	SkuCode      types.String `tfsdk:"sku_code"`
	GPUCount     types.Int64  `tfsdk:"gpu_count"`
	ImageName    types.String `tfsdk:"image_name"`
	InitialAuth  types.Object `tfsdk:"initial_auth"`
	CreatedTime  types.String `tfsdk:"created_time"`
	LastUpdated  types.String `tfsdk:"last_updated"`
	Location     types.Map    `tfsdk:"location"`
}

type ResourceModelInitialAuth struct {
	SshKeyId types.String `tfsdk:"ssh_key_id"`
}

func (o ResourceModelInitialAuth) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ssh_key_id": types.StringType,
	}
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
	model = setModelValuesNotPresent(ctx, response, model)

	return model
}

func setModelValuesNotPresent(ctx context.Context, response *readGPUResponse, model ResourceModel) ResourceModel {
	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.SeriesName.IsNull() || model.SeriesName.ValueString() == "" {
		model.SeriesName = types.StringValue(response.Data.Configuration.Name)
	}
	if model.SeriesCode.IsNull() || model.SeriesCode.ValueString() == "" {
		model.SeriesCode = types.StringValue(response.Data.Configuration.Code)
	}
	if model.SkuCode.IsNull() || model.SkuCode.ValueString() == "" {
		model.SkuCode = types.StringValue(response.Data.Configuration.SkuCode)
	}
	if model.GPUCount.IsNull() {
		model.GPUCount = types.Int64Value(response.Data.Configuration.GPUCount)
	}

	// The API returns a longer image name than the user configures. A refresh here
	// shows permanent drift.
	if model.ImageName.IsNull() || model.ImageName.ValueString() == "" {
		model.ImageName = types.StringValue(response.Data.Image)
	}

	// initial_auth changes the state only, so the API value must not win.
	if model.InitialAuth.IsNull() && response.Data.SshKeyId != "" {
		authObj, diags := types.ObjectValueFrom(ctx, ResourceModelInitialAuth{}.AttrTypes(), ResourceModelInitialAuth{
			SshKeyId: types.StringValue(response.Data.SshKeyId),
		})
		if !diags.HasError() {
			model.InitialAuth = authObj
		}
	}
	return model
}

// RefreshGPUModelFromResponse reports the drift that Read must show. An empty
// response field leaves the model value alone, because an omitted field must not
// blank a required attribute. sku_code stays out: it requires replacement, and a
// substituted SKU would plan a replacement on every refresh. The fill-if-null path
// in setModelValuesNotPresent still populates it.
func RefreshGPUModelFromResponse(response *readGPUResponse, model ResourceModel) ResourceModel {
	if response.Data.Datacenter.ID != "" {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	if response.Data.Name != "" {
		model.Name = types.StringValue(response.Data.Name)
	}
	if response.Data.Configuration.Name != "" {
		model.SeriesName = types.StringValue(response.Data.Configuration.Name)
	}
	if response.Data.Configuration.Code != "" {
		model.SeriesCode = types.StringValue(response.Data.Configuration.Code)
	}
	if response.Data.Configuration.GPUCount != 0 {
		model.GPUCount = types.Int64Value(response.Data.Configuration.GPUCount)
	}
	return model
}
