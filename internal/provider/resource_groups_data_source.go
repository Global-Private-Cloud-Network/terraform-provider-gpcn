package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/resourcegroups"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &resourceGroupsDataSource{}
	_ datasource.DataSourceWithConfigure = &resourceGroupsDataSource{}
)

func NewResourceGroupsDataSource() datasource.DataSource {
	return &resourceGroupsDataSource{}
}

type resourceGroupsDataSource struct {
	client *client.GpcnClient
}

type resourceGroupsDataSourceModel struct {
	Name                   types.String `tfsdk:"name"`
	ResourceGroups         types.List   `tfsdk:"resource_groups"`
	DefaultResourceGroupID types.String `tfsdk:"default_resource_group_id"`
}

type resourceGroupsRowTF struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

func (resourceGroupsRowTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":          types.StringType,
		"name":        types.StringType,
		"description": types.StringType,
		"is_default":  types.BoolType,
	}
}

func (d *resourceGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_groups"
}

func (d *resourceGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the resource groups the API key can see, and reports the tenant's Default resource group.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Return only the resource group with exactly this name. The data source fails when none matches.",
			},
			"resource_groups": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Resource groups that match the filter.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier of the resource group.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the resource group.",
						},
						"description": schema.StringAttribute{
							Computed:    true,
							Description: "Description of the resource group. Null when it has none.",
						},
						"is_default": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether this is the tenant's Default resource group.",
						},
					},
				},
			},
			"default_resource_group_id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the tenant's Default resource group. The name filter does not affect it. Null when the API key sees no Default group.",
			},
		},
	}
}

func (d *resourceGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnexpectedDataSourceConfigureType,
			fmt.Sprintf(resourcegroups.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	d.client = gpcnClient
}

func (d *resourceGroupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)

	var state resourceGroupsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groups, err := resourcegroups.FetchResourceGroups(d.client, ctx)
	if err != nil {
		resp.Diagnostics.AddError(resourcegroups.ErrSummaryUnableToFetchResourceGroups, err.Error())
		return
	}

	state.DefaultResourceGroupID = types.StringNull()
	for _, group := range groups {
		if group.IsDefault {
			state.DefaultResourceGroupID = types.StringValue(group.ID)
			break
		}
	}

	rows := make([]resourceGroupsRowTF, 0, len(groups))
	for _, group := range groups {
		if !state.Name.IsNull() && group.Name != state.Name.ValueString() {
			continue
		}
		row := resourceGroupsRowTF{
			ID:          types.StringValue(group.ID),
			Name:        types.StringValue(group.Name),
			Description: types.StringNull(),
			IsDefault:   types.BoolValue(group.IsDefault),
		}
		if group.Description != "" {
			row.Description = types.StringValue(group.Description)
		}
		rows = append(rows, row)
	}

	if !state.Name.IsNull() && len(rows) == 0 {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryNoResourceGroupMatches,
			fmt.Sprintf(resourcegroups.ErrDetailNoResourceGroupMatches, state.Name.ValueString()),
		)
		return
	}

	list, listDiags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: resourceGroupsRowTF{}.AttrTypes()}, rows)
	resp.Diagnostics.Append(listDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ResourceGroups = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
