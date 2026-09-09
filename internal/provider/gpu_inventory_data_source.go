package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/gpu"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewGPUInventoryDataSource() datasource.DataSource {
	return &gpuInventoryDataSource{}
}

type gpuInventoryDataSource struct {
	client *client.GpcnClient
}

type gpuInventoryDataSourceModel struct {
	DatacenterId types.String `tfsdk:"datacenter_id"`
	SeriesName   types.String `tfsdk:"series_name"`
	SeriesCode   types.String `tfsdk:"series_code"`
	GPUCount     types.Int64  `tfsdk:"gpu_count"`
	Inventory    types.List   `tfsdk:"inventory"`
}

type gpuInventoryItemTF struct {
	Name      types.String `tfsdk:"name"`
	Code      types.String `tfsdk:"code"`
	GPUCount  types.Int64  `tfsdk:"gpu_count"`
	VCPU      types.Int64  `tfsdk:"vcpu"`
	MemoryGiB types.Int64  `tfsdk:"memory_gib"`
	StorageGB types.Int64  `tfsdk:"storage_gb"`
	SkuCode   types.String `tfsdk:"sku_code"`
}

func (o gpuInventoryItemTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":       types.StringType,
		"code":       types.StringType,
		"gpu_count":  types.Int64Type,
		"vcpu":       types.Int64Type,
		"memory_gib": types.Int64Type,
		"storage_gb": types.Int64Type,
		"sku_code":   types.StringType,
	}
}

func (d *gpuInventoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gpu_inventory"
}

func (d *gpuInventoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves the available GPU SKUs for a datacenter. Results can be filtered by GPU series and GPU count",
		Attributes: map[string]schema.Attribute{
			"datacenter_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the datacenter to retrieve GPU inventory for",
			},
			"series_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by human-readable GPU series name. Conflicts with series_code",
				Validators: []validator.String{
					stringvalidator.OneOf(gpu.GPUSeriesNames...),
					stringvalidator.ConflictsWith(path.MatchRoot("series_code")),
				},
			},
			"series_code": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by short GPU series code. Conflicts with series_name",
				Validators: []validator.String{
					stringvalidator.OneOf(gpu.GPUSeriesCodes...),
					stringvalidator.ConflictsWith(path.MatchRoot("series_name")),
				},
			},
			"gpu_count": schema.Int64Attribute{
				Optional:    true,
				Description: "Filter to SKUs with this GPU count. Must be 1, 2, 4, or 8",
				Validators: []validator.Int64{
					int64validator.OneOf(1, 2, 4, 8),
				},
			},
			"inventory": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of available GPU SKUs matching the specified filter criteria",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Human-readable name of the GPU series (e.g. \"NVIDIA RTX A6000 Series\")",
						},
						"code": schema.StringAttribute{
							Computed:    true,
							Description: "Short code of the GPU series (e.g. \"nvidia-rtx_a6000-series\")",
						},
						"gpu_count": schema.Int64Attribute{
							Computed:    true,
							Description: "Number of GPUs in this SKU",
						},
						"vcpu": schema.Int64Attribute{
							Computed:    true,
							Description: "Number of vCPUs in this SKU",
						},
						"memory_gib": schema.Int64Attribute{
							Computed:    true,
							Description: "Amount of RAM in GiB",
						},
						"storage_gb": schema.Int64Attribute{
							Computed:    true,
							Description: "Amount of storage in GB",
						},
						"sku_code": schema.StringAttribute{
							Computed:    true,
							Description: "Exact SKU code. Use this as the sku_code in a gpcn_gpu resource",
						},
					},
				},
			},
		},
	}
}

func (d *gpuInventoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(gpu.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	d.client = gpcnClient
}

func (d *gpuInventoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)

	var state gpuInventoryDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve a series name to its code so the API filter always uses the code.
	seriesCode := state.SeriesCode.ValueString()
	if seriesCode == "" && state.SeriesName.ValueString() != "" {
		seriesCode = gpu.GPUSeriesNameToCode[state.SeriesName.ValueString()]
	}

	var gpuCount int64
	if !state.GPUCount.IsNull() {
		gpuCount = state.GPUCount.ValueInt64()
	}

	items, err := gpu.FetchInventory(d.client, ctx, state.DatacenterId.ValueString(), seriesCode, gpuCount)
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToFetchInventory,
			err.Error(),
		)
		return
	}

	tfItems := make([]gpuInventoryItemTF, 0, len(items))
	for _, item := range items {
		tfItems = append(tfItems, gpuInventoryItemTF{
			Name:      types.StringValue(item.SeriesName),
			Code:      types.StringValue(item.SeriesCode),
			GPUCount:  types.Int64Value(item.GPUCount),
			VCPU:      types.Int64Value(item.VCPU),
			MemoryGiB: types.Int64Value(item.MemoryGiB),
			StorageGB: types.Int64Value(item.StorageGB),
			SkuCode:   types.StringValue(item.SkuCode),
		})
	}

	var listDiags diag.Diagnostics
	state.Inventory, listDiags = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: gpuInventoryItemTF{}.AttrTypes()}, tfItems)
	if listDiags.HasError() {
		resp.Diagnostics.Append(listDiags...)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
