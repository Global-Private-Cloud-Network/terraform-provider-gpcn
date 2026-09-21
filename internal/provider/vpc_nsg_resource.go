package provider

import (
	"context"
	"fmt"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/vpcnsgs"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &vpcNsgResource{}
	_ resource.ResourceWithConfigure   = &vpcNsgResource{}
	_ resource.ResourceWithImportState = &vpcNsgResource{}
)

// NewVpcNsgResource is a helper function to simplify the provider implementation.
func NewVpcNsgResource() resource.Resource {
	return &vpcNsgResource{}
}

// vpcNsgResource is the resource implementation.
type vpcNsgResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *vpcNsgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_nsg"
}

// Schema defines the schema for the resource.
func (r *vpcNsgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a network security group inside a GPCN VPC, with its rules inline. GPCN replaces the whole rule set on every change and identifies a rule by its content, so the rules belong to this resource rather than to one of their own. The API key needs vpc:read to read a group, and vpc-nsg:create, vpc-nsg:update and vpc-nsg:delete to manage one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the security group in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC the security group belongs to. Changing this value requires replacing the security group",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the security group. It must be unique within the VPC",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Additional information about the security group to provide context for its purpose",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"is_default": schema.BoolAttribute{
				Description: "Whether this is the VPC's own default security group. GPCN creates that group with the VPC and refuses to delete it",
				Computed:    true,
			},
			"state": schema.StringAttribute{
				Description: "Lifecycle state of the security group: one of 'creating', 'ready', 'failed' or 'removing'",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Why the last operation on the security group failed, or null when none has",
				Computed:    true,
			},
			"rule_count": schema.Int64Attribute{
				Description: "The number of rules the security group holds",
				Computed:    true,
			},
			"subnet_count": schema.Int64Attribute{
				Description: "The number of subnets bound to this security group. GPCN refuses to delete a group while this count is above zero",
				Computed:    true,
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the security group was created",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the security group was last updated",
				Computed:    true,
			},
		},
		Blocks: map[string]schema.Block{
			"rule": schema.SetNestedBlock{
				Description: "One traffic rule of the security group. GPCN identifies a rule by its direction, protocol, port range and remote CIDR, so two rules that agree on all four are the same rule",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"direction": schema.StringAttribute{
							Description: "Direction the rule applies to: either 'ingress' or 'egress'",
							Required:    true,
							Validators: []validator.String{
								stringvalidator.OneOf(vpcnsgs.RuleDirections...),
							},
						},
						"protocol": schema.StringAttribute{
							Description: "Protocol the rule applies to: one of 'tcp', 'udp', 'icmp' or 'all'",
							Required:    true,
							Validators: []validator.String{
								stringvalidator.OneOf(vpcnsgs.RuleProtocols...),
							},
						},
						"port_range_min": schema.Int64Attribute{
							Description: "Lowest port the rule applies to. Ports apply to the tcp and udp protocols only, and must be given together with port_range_max",
							Optional:    true,
						},
						"port_range_max": schema.Int64Attribute{
							Description: "Highest port the rule applies to. Ports apply to the tcp and udp protocols only, and must be given together with port_range_min",
							Optional:    true,
						},
						"remote_cidr": schema.StringAttribute{
							Description: "Remote address range the rule applies to, given as an IPv4 CIDR at its network address (e.g., 0.0.0.0/0 or 203.0.113.0/24)",
							Required:    true,
						},
						"description": schema.StringAttribute{
							Description: "Additional information about the rule. GPCN edits it in place, because it is not part of the rule's identity",
							Optional:    true,
						},
					},
					Validators: []validator.Object{
						vpcnsgs.RulePortValidator{},
					},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *vpcNsgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			vpcnsgs.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(vpcnsgs.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *vpcNsgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcnsgs.LogStartingCreateGPCNNsg)

	var plan vpcnsgs.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, diags := vpcnsgs.RulesFromSet(ctx, plan.Rules)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	detail, err := vpcnsgs.CreateNsg(r.client, ctx, plan.VpcID.ValueString(), plan.Name.ValueString(), plan.Description.ValueString(), rules)
	if err != nil {
		resp.Diagnostics.AddError(vpcnsgs.ErrSummaryUnableToCreateNsg, err.Error())
		return
	}

	plan, diags = vpcnsgs.MapNsgResponseToModel(ctx, detail, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcnsgs.LogSuccessfullyFinishedCreateGPCNNsg)
}

// Read refreshes the Terraform state with the latest data.
func (r *vpcNsgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcnsgs.LogStartingReadGPCNNsg)

	var state vpcnsgs.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	detail, err := vpcnsgs.GetNsg(r.client, ctx, state.VpcID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			tflog.Info(ctx, vpcnsgs.LogNsgNotFoundRemovingState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			vpcnsgs.ErrSummaryUnableToGetNsg,
			fmt.Sprintf(vpcnsgs.ErrDetailUnableToGetNsgWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	state, diags = vpcnsgs.MapNsgResponseToModel(ctx, detail, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags = vpcnsgs.RefreshNsgModelFromResponse(ctx, detail, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcnsgs.LogSuccessfullyFinishedReadGPCNNsg)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *vpcNsgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcnsgs.LogStartingUpdateGPCNNsg)

	var plan vpcnsgs.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state vpcnsgs.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	nsgID := state.ID.ValueString()

	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) {
		if err := vpcnsgs.RenameNsg(r.client, ctx, vpcID, nsgID, plan.Name.ValueString(), plan.Description.ValueString()); err != nil {
			resp.Diagnostics.AddError(
				vpcnsgs.ErrSummaryUnableToUpdateNsg,
				fmt.Sprintf(vpcnsgs.ErrDetailUnableToUpdateNsgWithID, nsgID)+": "+err.Error(),
			)
			return
		}
	}

	// The rules route is a full replace, so one changed rule sends them all.
	// Sending them when nothing changed would file a job for no work.
	if !plan.Rules.Equal(state.Rules) {
		rules, ruleDiags := vpcnsgs.RulesFromSet(ctx, plan.Rules)
		resp.Diagnostics.Append(ruleDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := vpcnsgs.ReplaceNsgRules(r.client, ctx, vpcID, nsgID, rules); err != nil {
			resp.Diagnostics.AddError(
				vpcnsgs.ErrSummaryUnableToUpdateNsg,
				fmt.Sprintf(vpcnsgs.ErrDetailUnableToUpdateNsgWithID, nsgID)+": "+err.Error(),
			)
			return
		}
	}

	detail, err := vpcnsgs.GetNsg(r.client, ctx, vpcID, nsgID)
	if err != nil {
		resp.Diagnostics.AddError(
			vpcnsgs.ErrSummaryUnableToGetNsg,
			fmt.Sprintf(vpcnsgs.ErrDetailUnableToGetNsgWithID, nsgID)+": "+err.Error(),
		)
		return
	}

	plan, diags = vpcnsgs.MapNsgResponseToModel(ctx, detail, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcnsgs.LogSuccessfullyFinishedUpdateGPCNNsg)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *vpcNsgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcnsgs.LogStartingDeleteGPCNNsg)

	var state vpcnsgs.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := vpcnsgs.DeleteNsg(r.client, ctx, state.VpcID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		tflog.Info(ctx, vpcnsgs.LogNsgAlreadyDeleted)
	} else if err != nil {
		resp.Diagnostics.AddError(
			vpcnsgs.ErrSummaryUnableToDeleteNsg,
			fmt.Sprintf(vpcnsgs.ErrDetailUnableToDeleteNsgWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, vpcnsgs.LogSuccessfullyFinishedDeleteGPCNNsg)
}

// ImportState reads the composite ID the read needs. A security group
// projection carries no VPC ID, and the route is scoped under the VPC.
func (r *vpcNsgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			vpcnsgs.ErrSummaryInvalidImportID,
			fmt.Sprintf(vpcnsgs.ErrDetailImportIDFormat, req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vpc_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
