package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/l2segments"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &l2SegmentResource{}
	_ resource.ResourceWithConfigure   = &l2SegmentResource{}
	_ resource.ResourceWithImportState = &l2SegmentResource{}
)

// NewL2SegmentResource is a helper function to simplify the provider implementation.
func NewL2SegmentResource() resource.Resource {
	return &l2SegmentResource{}
}

// l2SegmentResource is the resource implementation.
type l2SegmentResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *l2SegmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_l2_segment"
}

// Schema defines the schema for the resource.
func (r *l2SegmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an L2 segment: a layer-2 network that carries virtual machine traffic inside one datacenter. The API key needs l2-segment:read to read a segment, and l2-segment:create, l2-segment:update and l2-segment:delete to manage one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the L2 segment in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the L2 segment. Must be 1-255 characters",
				Required:    true,
				Validators: []validator.String{
					// The character rule is service-side and applies to a rename
					// only. A schema-level rule would make an adopted segment,
					// whose name came from a legacy network, unmanageable.
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter that holds the L2 segment. Changing this value requires replacing the segment",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Additional information about the L2 segment to provide context for its purpose",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(500),
				},
				Default: stringdefault.StaticString(""),
			},
			"state": schema.StringAttribute{
				Description: "Lifecycle state of the L2 segment: one of 'creating', 'ready', 'failed' or 'removing'",
				Computed:    true,
			},
			"offering": schema.StringAttribute{
				Description: "Provider offering the L2 segment was built on: 'plain', or 'config_drive' for a segment the platform adopted from a legacy custom network",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Explanation the platform recorded when the L2 segment failed. Null while nothing has failed",
				Computed:    true,
			},
			"datacenter_name": schema.StringAttribute{
				Description: "Display name of the datacenter that holds the L2 segment",
				Computed:    true,
			},
			"attached_nic_count": schema.Int64Attribute{
				Description: "The number of live network interfaces currently attached to this L2 segment",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the L2 segment was created",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the L2 segment was last updated",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *l2SegmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)

	if !ok {
		resp.Diagnostics.AddError(
			l2segments.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(l2segments.ErrDetailExpectedGpcnClient, req.ProviderData),
		)

		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *l2SegmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, l2segments.LogStartingCreateGPCNL2Segment)

	var plan l2segments.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A create the platform refuses, and a job that fails, both leave no id to
	// write. Terraform then plans a create again rather than a stale read.
	getResponse, err := l2segments.CreateL2Segment(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			l2segments.ErrSummaryUnableToCreateL2Segment,
			err.Error(),
		)
		return
	}

	plan = l2segments.MapL2SegmentResponseToModel(getResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, l2segments.LogSuccessfullyFinishedCreateGPCNL2Segment)
}

// Read refreshes the Terraform state with the latest data.
func (r *l2SegmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, l2segments.LogStartingReadGPCNL2Segment)

	var state l2segments.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResponse, err := l2segments.GetL2Segment(r.client, ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			tflog.Info(ctx, l2segments.LogL2SegmentNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			l2segments.ErrSummaryUnableToGetL2Segment,
			fmt.Sprintf(l2segments.ErrDetailUnableToGetL2SegmentWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}
	tflog.Info(ctx, l2segments.LogSuccessfullyRetrievedGPCNL2SegmentRead)

	state = l2segments.MapL2SegmentResponseToModel(getResponse, state)
	state = l2segments.RefreshL2SegmentModelFromResponse(getResponse, state)

	// A failed segment is live and tenant-visible, so it stays in state. Nothing
	// else in a plan tells the user the platform parked it.
	if state.State.ValueString() == l2segments.StateFailed {
		resp.Diagnostics.Append(l2segments.FailedSegmentWarning(
			state.ID.ValueString(),
			state.FailureReason.ValueString(),
		))
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, l2segments.LogSuccessfullyFinishedReadGPCNL2Segment)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *l2SegmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, l2segments.LogStartingUpdateGPCNL2Segment)

	var plan l2segments.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state l2segments.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResponse, err := l2segments.UpdateL2Segment(r.client, ctx, state.ID.ValueString(), plan, state)
	if err != nil {
		resp.Diagnostics.AddError(
			l2segments.ErrSummaryUnableToUpdateL2Segment,
			fmt.Sprintf(l2segments.ErrDetailUnableToUpdateL2SegmentWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	plan = l2segments.MapL2SegmentResponseToModel(getResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, l2segments.LogSuccessfullyFinishedUpdateGPCNL2Segment)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *l2SegmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, l2segments.LogStartingDeleteGPCNL2Segment)

	var state l2segments.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := l2segments.DeleteL2Segment(r.client, ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		tflog.Info(ctx, l2segments.LogL2SegmentAlreadyDeleted)
	} else if err != nil {
		resp.Diagnostics.AddError(
			l2segments.ErrSummaryUnableToDeleteL2Segment,
			fmt.Sprintf(l2segments.ErrDetailUnableToDeleteL2SegmentWithID, state.ID.ValueString())+": "+l2segments.DeleteRefusalDetail(err),
		)
		return
	}

	tflog.Info(ctx, l2segments.LogSuccessfullyFinishedDeleteGPCNL2Segment)
}

// The import id is the segment id. An adopted segment carries a new id, never the
// id of the legacy network the platform consumed.
func (r *l2SegmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
