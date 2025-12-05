package provider

import (
	"context"
	"fmt"
	"net/http"

	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &networksResource{}
	_ resource.ResourceWithConfigure   = &networksResource{}
	_ resource.ResourceWithImportState = &networksResource{}
)

// NewNetworksResource is a helper function to simplify the provider implementation.
func NewNetworksResource() resource.Resource {
	return &networksResource{}
}

// networksResource is the resource implementation.
type networksResource struct {
	client *http.Client
}

// Metadata returns the resource type name.
func (r *networksResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

// Schema defines the schema for the resource.
func (r *networksResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a private network to connect virtual machines within the same datacenter",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the network in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the network",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Additional information about the network to provide context for its purpose",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(500),
				},
				Default: stringdefault.StaticString(""),
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the network was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the network was last updated in ISO-8601 format",
				Computed:    true,
			},
			"snat": schema.StringAttribute{
				Description: "Source Network Address Translation (SNAT) status. Automatically set to 'true' for standard networks and 'false' for custom networks",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cidr_block": schema.StringAttribute{
				Description: "CIDR block defining the IP address range for the network (e.g., 10.0.0.0/24)",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					networks.StandardNetworkValidator{},
					networks.CIDRValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"gateway": schema.StringAttribute{
				Description: "The default gateway IP address for the network",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"connected_vms": schema.StringAttribute{
				Description: "The number of virtual machines currently connected to this network",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"network_type": schema.StringAttribute{
				Description: "Type of network: either 'standard' or 'custom'. Changing this value requires replacing the network",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the network_type requires us to destroy and create a new network
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(networks.NETWORK_TYPE_STANDARD, networks.NETWORK_TYPE_CUSTOM),
				},
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter where the network will be created. Changing this value requires replacing the network",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the datacenter_id requires us to destroy and create a new network
					stringplanmodifier.RequiresReplace(),
				},
			},
			"location": schema.MapAttribute{
				Description: "Location details including datacenter, region, and country information",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
				},
			},
			"dns_servers": schema.StringAttribute{
				Description: "Comma-separated list of DNS server IPv4 addresses. Only applicable for standard networks",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					networks.StandardNetworkValidator{},
					networks.DNSServersValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dhcp_start_address": schema.StringAttribute{
				Description: "Starting IP address of the DHCP range. Must be specified together with dhcp_end_address. Only applicable for standard networks",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.AlsoRequires(path.MatchRoot("dhcp_end_address")),
					networks.StandardNetworkValidator{},
					networks.IpAddressValidator{},
				},
			},
			"dhcp_end_address": schema.StringAttribute{
				Description: "Ending IP address of the DHCP range. Must be specified together with dhcp_start_address. Only applicable for standard networks",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.AlsoRequires(path.MatchRoot("dhcp_start_address")),
					networks.StandardNetworkValidator{},
					networks.IpAddressValidator{},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *networksResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*http.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *http.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create creates the resource and sets the initial Terraform state.
func (r *networksResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Info(ctx, networks.LogStartingCreateGPCNNetwork)
	// Retrieve values from plan
	var plan networks.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getNetworkResponse, err := networks.CreateNetwork(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create GPCN Network",
			err.Error(),
		)
		return
	}

	plan = networks.MapNetworkResponseToModel(ctx, getNetworkResponse, plan)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, networks.LogSuccessfullyFinishedCreateGPCNNetwork)
}

// Read refreshes the Terraform state with the latest data.
func (r *networksResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	tflog.Info(ctx, networks.LogStartingReadGPCNNetwork)
	// Get current state
	var state networks.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getNetworkResponse, err := networks.GetNetwork(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to get GPCN Network with ID "+state.ID.ValueString(), err.Error())
		return
	}
	tflog.Info(ctx, networks.LogSuccessfullyRetrievedGPCNNetworkRead)

	state = networks.MapNetworkResponseToModel(ctx, getNetworkResponse, state)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, networks.LogSuccessfullyFinishedReadGPCNNetwork)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *networksResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Info(ctx, networks.LogStartingUpdateGPCNNetwork)
	// Retrieve values from plan
	var plan networks.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getNetworkResponse, err := networks.UpdateNetwork(r.client, ctx, plan.ID.ValueString(), plan)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to update GPCN Network with ID "+plan.ID.ValueString(),
			err.Error(),
		)
		return
	}

	plan = networks.MapNetworkResponseToModel(ctx, getNetworkResponse, plan)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, networks.LogSuccessfullyFinishedUpdateGPCNNetwork)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *networksResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Info(ctx, networks.LogStartingDeleteGPCNNetwork)
	var state networks.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := networks.DeleteNetwork(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to delete GPCN Network with ID "+state.ID.ValueString(),
			err.Error(),
		)
		return
	}

	tflog.Info(ctx, networks.LogSuccessfullyFinishedDeleteGPCNNetwork)
}

func (r *networksResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
