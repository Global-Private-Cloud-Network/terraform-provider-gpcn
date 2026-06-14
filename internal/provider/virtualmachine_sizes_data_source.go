package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/virtualmachinesizes"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewVirtualMachineSizesDataSource() datasource.DataSource {
	return &virtualMachineSizesDataSource{}
}

type virtualMachineSizesDataSource struct {
	client *client.GpcnClient
}

type virtualMachineSizesDataSourceModel struct {
	DatacenterId types.String `tfsdk:"datacenter_id"`
	Category     types.String `tfsdk:"category"`
	MinCPU       types.Int64  `tfsdk:"min_cpu"`
	MinMemoryGB  types.Int64  `tfsdk:"min_memory_gb"`
	MinStorageGB types.Int64  `tfsdk:"min_storage_gb"`
	Sizes        types.List   `tfsdk:"sizes"`
}

type virtualMachineSizeTF struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	SkuCode   types.String `tfsdk:"sku_code"`
	Category  types.String `tfsdk:"category"`
	CPU       types.Int64  `tfsdk:"cpu"`
	MemoryGB  types.Int64  `tfsdk:"memory_gb"`
	StorageGB types.Int64  `tfsdk:"storage_gb"`
}

func (o virtualMachineSizeTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":         types.StringType,
		"name":       types.StringType,
		"sku_code":   types.StringType,
		"category":   types.StringType,
		"cpu":        types.Int64Type,
		"memory_gb":  types.Int64Type,
		"storage_gb": types.Int64Type,
	}
}

func (d *virtualMachineSizesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtualmachine_sizes"
}

func (d *virtualMachineSizesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves available virtual machine sizes for a given datacenter. Results can be filtered by category and minimum specifications",
		Attributes: map[string]schema.Attribute{
			"datacenter_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the datacenter to retrieve sizes for",
			},
			"category": schema.StringAttribute{
				Optional:    true,
				Description: "Filter sizes by category code. Must be one of: \"general-purpose\", \"memory-optimized\"",
				Validators: []validator.String{
					stringvalidator.OneOf("general-purpose", "memory-optimized"),
				},
			},
			"min_cpu": schema.Int64Attribute{
				Optional:    true,
				Description: "Filter to sizes with at least this many CPU cores",
			},
			"min_memory_gb": schema.Int64Attribute{
				Optional:    true,
				Description: "Filter to sizes with at least this much RAM in GB",
			},
			"min_storage_gb": schema.Int64Attribute{
				Optional:    true,
				Description: "Filter to sizes with at least this much base storage in GB",
			},
			"sizes": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of virtual machine sizes matching the specified filter criteria",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier (SKU ID) of the size. Use this as the size_id in a gpcn_virtualmachine resource",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Human-readable name of the size (e.g. \"G-Small-1\")",
						},
						"sku_code": schema.StringAttribute{
							Computed:    true,
							Description: "Short code identifying the size (e.g. \"vm-gen-small-1\")",
						},
						"category": schema.StringAttribute{
							Computed:    true,
							Description: "Category code this size belongs to (e.g. \"general-purpose\")",
						},
						"cpu": schema.Int64Attribute{
							Computed:    true,
							Description: "Number of CPU cores",
						},
						"memory_gb": schema.Int64Attribute{
							Computed:    true,
							Description: "Amount of RAM in GB",
						},
						"storage_gb": schema.Int64Attribute{
							Computed:    true,
							Description: "Amount of base storage in GB",
						},
					},
				},
			},
		},
	}
}

func (d *virtualMachineSizesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			virtualmachinesizes.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(virtualmachinesizes.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	d.client = gpcnClient
}

func (d *virtualMachineSizesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)

	var state virtualMachineSizesDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	allSizes, err := virtualmachinesizes.FetchSizes(d.client, ctx, state.DatacenterId.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachinesizes.ErrSummaryUnableToFetchSizes,
			err.Error(),
		)
		return
	}

	category := state.Category.ValueString()
	var minCPU, minMemoryGB, minStorageGB int64
	if !state.MinCPU.IsNull() {
		minCPU = state.MinCPU.ValueInt64()
	}
	if !state.MinMemoryGB.IsNull() {
		minMemoryGB = state.MinMemoryGB.ValueInt64()
	}
	if !state.MinStorageGB.IsNull() {
		minStorageGB = state.MinStorageGB.ValueInt64()
	}

	filtered := virtualmachinesizes.FilterSizes(ctx, allSizes, category, minCPU, minMemoryGB, minStorageGB)

	var tfSizes []virtualMachineSizeTF
	for _, s := range filtered {
		tfSizes = append(tfSizes, virtualMachineSizeTF{
			ID:        types.StringValue(s.ID),
			Name:      types.StringValue(s.Name),
			SkuCode:   types.StringValue(s.SkuCode),
			Category:  types.StringValue(s.Category),
			CPU:       types.Int64Value(s.CPU),
			MemoryGB:  types.Int64Value(s.MemoryGB),
			StorageGB: types.Int64Value(s.StorageGB),
		})
	}

	var listDiags diag.Diagnostics
	state.Sizes, listDiags = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: virtualMachineSizeTF{}.AttrTypes()}, tfSizes)
	if listDiags.HasError() {
		resp.Diagnostics.Append(listDiags...)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
