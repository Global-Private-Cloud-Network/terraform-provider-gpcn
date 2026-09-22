package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/virtualmachines"
	"terraform-provider-gpcn/internal/virtualmachinesizes"

	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &virtualMachinesResource{}
	_ resource.ResourceWithConfigure   = &virtualMachinesResource{}
	_ resource.ResourceWithImportState = &virtualMachinesResource{}
	_ resource.ResourceWithModifyPlan  = &virtualMachinesResource{}
)

// NewVirtualMachinesResource is a helper function to simplify the provider implementation.
func NewVirtualMachinesResource() resource.Resource {
	return &virtualMachinesResource{}
}

// virtualMachinesResource is the resource implementation.
type virtualMachinesResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *virtualMachinesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtualmachine"
}

// Schema defines the schema for the resource.
func (r *virtualMachinesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a virtual machine instance with configurable compute resources, networking, and storage",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the virtual machine in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the virtual machine. Must be 1-60 characters, starting and ending with an alphanumeric character, containing only letters, digits, spaces, periods, and hyphens",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 60),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9 .\-]*[a-zA-Z0-9])?$`),
						"Name must start and end with an alphanumeric character and contain only letters, digits, spaces, periods, and hyphens",
					),
				},
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter where the virtual machine will be created. Changing this value requires replacing the virtual machine",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the datacenter_id requires us to destroy and create a new VM
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size_id": schema.StringAttribute{
				Description: "Unique identifier (SKU ID) of the size to use for the virtual machine. Use the gpcn_virtualmachine_sizes data source to look up the size ID. Changing to a non-upgradeable size requires replacing the virtual machine",
				Required:    true,
			},
			"image_id": schema.StringAttribute{
				Description: "Unique identifier of the operating system image to use for the virtual machine. Use the gpcn_virtualmachine_images data source to look up the image ID. Changing this value requires replacing the virtual machine",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the virtual machine was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the virtual machine was last updated in ISO-8601 format",
				Computed:    true,
			},
			"location": schema.MapAttribute{
				Description: "Location details including datacenter, region, and country information",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
				},
			},
			"configuration": schema.MapAttribute{
				Description: "Hardware configuration details including CPU, RAM, and disk specifications",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Map{
					virtualmachines.ConfigurationPlanModifier{},
				},
			},
			"allocate_public_ip": schema.BoolAttribute{
				Description: "Whether to acquire an elastic public IP on the VPC that holds the birth interface and attach it to that interface. Changing this value in place needs the vpc-public-ip:create, vpc-public-ip:update and vpc-public-ip:delete permissions. Destroying the virtual machine releases an address acquired this way. Never inferred on import: an imported machine records its address as public_ip_id, so destroying it leaves the address held",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"public_ip": schema.StringAttribute{
				Description: "The public IP address on the primary interface. Null while the machine holds no address",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					virtualmachines.PublicIpPlanModifier{},
				},
			},
			"public_ip_id": schema.StringAttribute{
				Description: "ID of a held gpcn_vpc_public_ip to attach to the primary interface. Cannot be set together with allocate_public_ip. The address outlives the virtual machine, because the operator holds it. An import fills this from the address the primary interface carries. Name this address in the configuration after an import; otherwise the next apply detaches it. Import that address as a gpcn_vpc_public_ip too when Terraform should own its release",
				Optional:    true,
				Validators: []validator.String{
					virtualmachines.PublicIpIdConflictsValidator{},
				},
			},
			"subnet_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC subnet the virtual machine is born on. Use the gpcn_vpc_subnet resource to create one. A machine lives in exactly one VPC, so changing this value requires replacing the virtual machine",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"l2_segment_ids": schema.ListAttribute{
				Description: "IDs of the L2 segments the virtual machine carries. They attach after the machine is created, and the machine is stopped for a change unless its image supports network hotplug. After an import, name the segments the machine carries; otherwise the next apply detaches them. Maximum of 4, because the birth subnet interface holds one of the five interfaces GPCN allows",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					// The birth subnet interface holds one of the five GPCN allows.
					listvalidator.SizeAtMost(virtualmachines.MAX_NETWORKS_ATTACHED_ALLOWED - 1),
					listvalidator.UniqueValues(),
				},
				Default: listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
			},
			"network_hotplug": schema.BoolAttribute{
				Description: "Whether the virtual machine supports hot modifications without the virtual machine being in Shutoff status",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"network_interfaces": schema.ListNestedAttribute{
				Description: "The network interfaces attached to the virtual machine, one per attached network",
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					virtualmachines.NetworkInterfacesPlanModifier{},
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The ID of the network interface",
							Computed:    true,
						},
						"network_interface": schema.Int64Attribute{
							Description: "The interface index on the virtual machine",
							Computed:    true,
						},
						"is_primary": schema.BoolAttribute{
							Description: "Whether this is the primary interface",
							Computed:    true,
						},
						"mac_address": schema.StringAttribute{
							Description: "The MAC address of the interface. Null while the platform has not materialized the port",
							Computed:    true,
						},
						"public_ip": schema.StringAttribute{
							Description: "The public IP address on the interface, if one is allocated",
							Computed:    true,
						},
						"public_ip_id": schema.StringAttribute{
							Description: "The ID of the allocated public IP address, if one is allocated. On a 'vpc' interface this is a gpcn_vpc_public_ip ID",
							Computed:    true,
						},
						"private_ip": schema.StringAttribute{
							Description: "The private IP address on the interface. Null when world is 'l2', because a segment has no subnet to draw an address from",
							Computed:    true,
						},
						"world": schema.StringAttribute{
							Description: "The kind of network the interface attaches to: 'legacy', 'vpc' or 'l2'. The identity attributes below are per world",
							Computed:    true,
						},
						"network_name": schema.StringAttribute{
							Description: "The name of the attached legacy network. Null unless world is 'legacy'",
							Computed:    true,
						},
						"network_id": schema.StringAttribute{
							Description: "The ID of the attached legacy network. Null unless world is 'legacy'",
							Computed:    true,
						},
						"cidr_block": schema.StringAttribute{
							Description: "The CIDR block of the attached legacy network or VPC subnet. Null when world is 'l2'",
							Computed:    true,
						},
						"gateway_ip": schema.StringAttribute{
							Description: "The gateway IP address of the attached legacy network. Null unless world is 'legacy'",
							Computed:    true,
						},
						"network_type": schema.StringAttribute{
							Description: "The type of the attached legacy network. Null unless world is 'legacy'",
							Computed:    true,
						},
						"vpc_subnet_id": schema.StringAttribute{
							Description: "The ID of the VPC subnet the interface attaches to. Null unless world is 'vpc'",
							Computed:    true,
						},
						"subnet_name": schema.StringAttribute{
							Description: "The name of the VPC subnet the interface attaches to. Null unless world is 'vpc'",
							Computed:    true,
						},
						"vpc_id": schema.StringAttribute{
							Description: "The ID of the VPC that holds the subnet. Null unless world is 'vpc'",
							Computed:    true,
						},
						"vpc_name": schema.StringAttribute{
							Description: "The name of the VPC that holds the subnet. Null unless world is 'vpc'",
							Computed:    true,
						},
						"l2_segment_id": schema.StringAttribute{
							Description: "The ID of the L2 segment the interface attaches to. Null unless world is 'l2'",
							Computed:    true,
						},
						"l2_segment_name": schema.StringAttribute{
							Description: "The name of the L2 segment the interface attaches to. Null unless world is 'l2'",
							Computed:    true,
						},
					},
				},
			},
			"resource_group_id": schema.StringAttribute{
				Description: "Optional ID of the resource group to assign this virtual machine to",
				Optional:    true,
			},
			"initial_auth": schema.SingleNestedAttribute{
				Description: "Initial authentication configuration for the virtual machine. Either ssh_key_id or password must be specified. This block is only applied at creation time; subsequent changes update the Terraform state only and do not affect the running machine",
				Required:    true,
				Attributes: map[string]schema.Attribute{
					"ssh_key_id": schema.StringAttribute{
						Description: "ID of the SSH key to use for authentication. Cannot be set together with password",
						Optional:    true,
						Validators: []validator.String{
							stringvalidator.ConflictsWith(
								path.MatchRelative().AtParent().AtName("password"),
							),
							stringvalidator.AtLeastOneOf(
								path.MatchRelative().AtParent().AtName("ssh_key_id"),
								path.MatchRelative().AtParent().AtName("password"),
							),
						},
					},
					"username": schema.StringAttribute{
						Description: "Username for the virtual machine. Must be 3-20 characters matching ^[a-zA-Z_][a-zA-Z0-9_-]*$",
						Required:    true,
						Validators: []validator.String{
							stringvalidator.LengthBetween(3, 20),
							stringvalidator.RegexMatches(
								regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`),
								"Username must start with a letter or underscore and contain only letters, digits, underscores, and hyphens",
							),
						},
					},
					"password": schema.StringAttribute{
						Description: "Password for authentication. Must be 12-20 characters, contain only letters, digits, and ! @ # % - _ ., and include at least one uppercase letter, one lowercase letter, one digit, and one symbol. Cannot be set when ssh_key_id is set. username defaults to the image default if not specified",
						Optional:    true,
						Sensitive:   true,
						Validators: []validator.String{
							virtualmachines.PasswordValidator{},
							stringvalidator.ConflictsWith(
								path.MatchRelative().AtParent().AtName("ssh_key_id"),
							),
							stringvalidator.AtLeastOneOf(
								path.MatchRelative().AtParent().AtName("ssh_key_id"),
								path.MatchRelative().AtParent().AtName("password"),
							),
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *virtualMachinesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)

	if !ok {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(virtualmachines.ErrDetailExpectedGpcnClient, req.ProviderData),
		)

		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *virtualMachinesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingCreateGPCNVirtualMachine)
	// Retrieve values from plan
	var plan virtualmachines.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getVirtualMachineResponse, err := virtualmachines.CreateVirtualMachine(r.client, ctx, plan.ImageId.ValueString(), plan.SizeId.ValueString(), plan)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToCreateVM,
			err.Error(),
		)
		return
	}

	var mapDiags diag.Diagnostics
	plan, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, getVirtualMachineResponse, plan)
	resp.Diagnostics.Append(mapDiags...)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.attachSegmentsAfterCreate(ctx, getVirtualMachineResponse, plan, resp)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedCreateGPCNVirtualMachine)
}

// The create body carries no segment, so every segment attaches after the machine
// exists. State already holds the machine, and it holds the segments that attached. A
// refused attach therefore leaves nothing outside Terraform, and leaves a later plan
// work to do.
func (r *virtualMachinesResource) attachSegmentsAfterCreate(ctx context.Context, response *virtualmachines.ReadVirtualMachinesResponse, plan virtualmachines.ResourceModel, resp *resource.CreateResponse) diag.Diagnostics {
	var diags diag.Diagnostics

	var segmentIds []string
	if !plan.L2SegmentIds.IsNull() && !plan.L2SegmentIds.IsUnknown() {
		diags.Append(plan.L2SegmentIds.ElementsAs(ctx, &segmentIds, true)...)
		if diags.HasError() {
			return diags
		}
	}
	if len(segmentIds) == 0 {
		return diags
	}

	virtualMachineID := plan.ID.ValueString()
	attached := []string{}
	var attachErr error
	failedSegmentId := ""

	// GPCN refuses an add-NIC on a running machine whose image has no network hotplug.
	// The update path takes the same gate.
	stopped := false
	if !plan.NetworkHotplug.ValueBool() {
		if stopErr := virtualmachines.StopVirtualMachine(r.client, ctx, virtualMachineID); stopErr != nil {
			attachErr = fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, virtualMachineID), stopErr)
			failedSegmentId = segmentIds[0]
		} else {
			stopped = true
		}
	}

	if attachErr == nil {
		for _, segmentId := range segmentIds {
			attachErr = networks.AddL2SegmentInterface(r.client, ctx, virtualMachineID, segmentId)
			if attachErr != nil {
				failedSegmentId = segmentId
				break
			}
			attached = append(attached, segmentId)
		}
	}

	// The provider stops the machine for the attach, so it starts the machine again.
	// A start that fails leaves the machine stopped, and the user learns that from the
	// diagnostic below the state write.
	var startErr error
	if stopped {
		startErr = virtualmachines.StartVirtualMachine(r.client, ctx, virtualMachineID)
	}

	var mapDiags diag.Diagnostics
	plan, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, response, plan)
	diags.Append(mapDiags...)

	attachedIds, attachedDiags := types.ListValueFrom(ctx, types.StringType, attached)
	diags.Append(attachedDiags...)
	if !attachedDiags.HasError() {
		plan.L2SegmentIds = attachedIds
	}

	diags.Append(resp.State.Set(ctx, plan)...)

	if startErr != nil {
		diags.AddError(
			virtualmachines.ErrSummaryVMLeftStopped,
			fmt.Sprintf(virtualmachines.ErrDetailVMLeftStoppedCreate, virtualMachineID, startErr.Error()),
		)
	}
	if attachErr != nil {
		diags.AddError(
			virtualmachines.ErrSummaryVMCreatedAttachFailed,
			fmt.Sprintf(virtualmachines.ErrDetailVMCreatedAttachFailed, virtualMachineID, failedSegmentId, attachErr.Error()),
		)
	}

	return diags
}

// Read refreshes the Terraform state with the latest data.
func (r *virtualMachinesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingReadGPCNVirtualMachine)
	// Get current state
	var state virtualmachines.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Perform a GET call to retrieve actual information about the Virtual Machine
	getVirtualMachineResponse, err := virtualmachines.GetVirtualMachine(r.client, ctx, state.ID.ValueString())
	if err != nil {
		// Resource was deleted outside of Terraform
		if client.IsNotFound(err) {
			tflog.Info(ctx, virtualmachines.LogVirtualMachineNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryRetrievingVMInfoFailed,
			fmt.Errorf("%s: %w", virtualmachines.ErrDetailVMInfoFailedCanImport, err).Error(),
		)
		return
	}

	var mapDiags diag.Diagnostics
	state, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, getVirtualMachineResponse, state)
	resp.Diagnostics.Append(mapDiags...)
	state = virtualmachines.RefreshVirtualMachineModelFromResponse(getVirtualMachineResponse, state)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedReadGPCNVirtualMachine)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *virtualMachinesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingUpdateGPCNVirtualMachine)
	var plan virtualmachines.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state virtualmachines.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The stop decision and the steps after it read the machine once here. State can lag
	// the platform after a failed read-back. A retry then asks for work the machine
	// already carries. A stop for finished work costs the user the whole downtime.
	liveDetail, detailErr := virtualmachines.GetVirtualMachine(r.client, ctx, state.ID.ValueString())
	if detailErr != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryRetrievingVMInfoFailed,
			detailErr.Error(),
		)
		return
	}

	liveInterfaces, interfacesErr := networks.GetNetworkInterfaces(r.client, ctx, state.ID.ValueString())
	if interfacesErr != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
			interfacesErr.Error(),
		)
		return
	}

	// Controls stopping the VM. Since this is time-expensive, we only need to do this in a few cases
	needStopVM := determineIfVMNeedsStopped(state, plan, liveDetail, liveInterfaces)

	// Before proceeding with update, conditionally stop the virtual machine
	if needStopVM {
		err := virtualmachines.StopVirtualMachine(r.client, ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryUnableToUpdateVM,
				fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, state.ID.ValueString()), err).Error(),
			)
			return
		}
	}

	// Every step between the stop and the state write leaves the machine stopped when
	// it fails. The steps run in order through one runner, so one early return owns
	// that repair. The runner keeps the response of the read-back for the mapping.
	var getVirtualMachineResponse *virtualmachines.ReadVirtualMachinesResponse
	acquiredPublicIpID := ""
	updateSteps := []func() diag.Diagnostics{
		func() diag.Diagnostics {
			return virtualmachines.UpdateL2SegmentsIfChanged(r.client, ctx, state.ID.ValueString(), state, plan, liveInterfaces)
		},
		func() diag.Diagnostics {
			var publicIpDiags diag.Diagnostics
			acquiredPublicIpID, publicIpDiags = virtualmachines.UpdatePublicIPIfChanged(r.client, ctx, state.ID.ValueString(), state, plan)
			return publicIpDiags
		},
		func() diag.Diagnostics {
			return virtualmachines.UpdateSizeIfChanged(r.client, ctx, plan.ID.ValueString(), state, plan, liveDetail)
		},
		func() diag.Diagnostics {
			return virtualmachines.UpdateChangeableAttributesIfChanged(r.client, ctx, state.ID.ValueString(), state, plan)
		},
		func() diag.Diagnostics {
			var readBackDiags diag.Diagnostics
			tflog.Info(ctx, virtualmachines.LogAllVMUpdateOpsCompleteRetrievingLatestInfo)
			var readBackErr error
			getVirtualMachineResponse, readBackErr = virtualmachines.GetVirtualMachine(r.client, ctx, plan.ID.ValueString())
			if readBackErr != nil {
				readBackDiags.AddError(
					virtualmachines.ErrSummaryRetrievingVMInfoFailed,
					fmt.Errorf("%s: %w", virtualmachines.ErrDetailVMInfoFailedCanImport, readBackErr).Error(),
				)
				// The address is on the machine, and this update writes no state. No
				// attribute records it, so this diagnostic is the only place it appears.
				if acquiredPublicIpID != "" {
					readBackDiags.AddError(
						virtualmachines.ErrSummaryUnableToUpdatePublicIPConfiguration,
						fmt.Sprintf(virtualmachines.ErrDetailPublicIpOrphaned, acquiredPublicIpID,
							plan.ID.ValueString(), virtualmachines.ErrPhrasePublicIpReadBackFailed, readBackErr.Error()),
					)
				}
			}
			return readBackDiags
		},
	}

	// A start that fails is the only report the user gets. It follows the diagnostics
	// of the step that fails. The change is not in state. An earlier step can still have
	// succeeded, so the remedy sends the user to the next plan.
	for _, updateStep := range updateSteps {
		resp.Diagnostics.Append(updateStep()...)
		if !resp.Diagnostics.HasError() {
			continue
		}
		if needStopVM {
			startErr := virtualmachines.StartVirtualMachine(r.client, ctx, state.ID.ValueString())
			if startErr != nil {
				resp.Diagnostics.AddError(
					virtualmachines.ErrSummaryVMLeftStopped,
					fmt.Sprintf(virtualmachines.ErrDetailVMLeftStoppedRetry, state.ID.ValueString(), startErr.Error()),
				)
			}
		}
		return
	}

	tflog.Info(ctx, virtualmachines.LogRetrievedLatestVMInfoMappingToModel)
	// The plan modifiers pin the interface list and the address to state when no network
	// input changed. Terraform refuses a state that differs from that plan. The platform
	// can fill a late column, such as a MAC address, inside this apply. It can also move
	// the address. The read-back is therefore kept only where the plan asked for a fresh
	// value. The next refresh records the rest.
	plannedNetworkInterfaces := plan.NetworkInterfaces
	plannedPublicIp := plan.PublicIp
	var mapDiags diag.Diagnostics
	plan, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, getVirtualMachineResponse, plan)
	resp.Diagnostics.Append(mapDiags...)
	if !plannedNetworkInterfaces.IsUnknown() {
		plan.NetworkInterfaces = plannedNetworkInterfaces
	}
	if !plannedPublicIp.IsUnknown() {
		plan.PublicIp = plannedPublicIp
	}

	// Once finished, conditionally start the virtual machine again. The diagnostic below
	// the state write reports a failed start.
	var startErr error
	if needStopVM {
		startErr = virtualmachines.StartVirtualMachine(r.client, ctx, state.ID.ValueString())
	}
	tflog.Debug(ctx, fmt.Sprintf(virtualmachines.LogSuccessfullyUpdatedVMMayNotBeRunning, state.ID.ValueString()))

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)

	if startErr != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryVMLeftStopped,
			fmt.Sprintf(virtualmachines.ErrDetailVMLeftStoppedUpdate, state.ID.ValueString(), startErr.Error()),
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedUpdateGPCNVirtualMachine)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *virtualMachinesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingDeleteGPCNVirtualMachine)
	var state virtualmachines.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Before proceeding with delete, stop the virtual machine
	err := virtualmachines.StopVirtualMachine(r.client, ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		// Already deleted outside of Terraform
		tflog.Info(ctx, virtualmachines.LogVirtualMachineAlreadyDeleted)
		return
	} else if virtualmachines.IsTerminalStatusError(err) {
		// The platform writes Destroyed and Deleting from its own lifecycle. The machine
		// never stops, and no machine remains to delete.
		tflog.Info(ctx, fmt.Sprintf(virtualmachines.LogVirtualMachineTerminalRemovingFromState, err.Error()))
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToDeleteVM,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, state.ID.ValueString()), err).Error(),
		)
		return
	}

	// Before deleting, detach any network interfaces first
	networkInterfaces, err := networks.GetNetworkInterfaces(r.client, ctx, state.ID.ValueString())

	if client.IsNotFound(err) {
		tflog.Info(ctx, virtualmachines.LogVirtualMachineAlreadyDeleted)
		return
	} else if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailNetworkInterfacesForVM, state.ID.ValueString()), err).Error(),
		)
		return
	}

	for _, adapter := range networkInterfaces {
		// Cannot remove the primary interface
		if !adapter.IsPrimary.ValueBool() {
			err = networks.RemoveNetworkInterface(r.client, ctx, state.ID.ValueString(), adapter.ID.ValueString())
			if err != nil {
				resp.Diagnostics.AddWarning(
					virtualmachines.WarnSummaryRemovingNetworkInterfaceFailed,
					fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.WarnDetailRemovingNetworkInterfaceWithIDFailed, adapter.ID.ValueString()), err).Error(),
				)
			}
		}
	}

	// GPCN keeps the addresses a deleted machine carried unless the delete asks for
	// them back. Terraform destroys the address it acquired and leaves a held
	// public_ip_id to the operator who holds it.
	var deleteRequestBody io.Reader
	if virtualmachines.ReleasesAcquiredAddress(state, networkInterfaces) {
		jsonDeleteRequestBody, err := json.Marshal(map[string]any{"releasePublicIps": true})
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryUnableToCreateDeleteRequest,
				err.Error(),
			)
			return
		}
		deleteRequestBody = bytes.NewBuffer(jsonDeleteRequestBody)
	}

	request, err := http.NewRequestWithContext(ctx, "DELETE", virtualmachines.BASE_URL_V1+state.ID.ValueString(), deleteRequestBody)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToCreateDeleteRequest,
			err.Error(),
		)
		return
	}
	tflog.Info(ctx, virtualmachines.LogConstructedDeleteGPCNVirtualMachineRequest)

	response, err := r.client.DoWithRetry(request)
	if client.IsNotFound(err) {
		// Already deleted outside of Terraform
		tflog.Info(ctx, virtualmachines.LogVirtualMachineAlreadyDeleted)
		return
	} else if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToDeleteVM,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailUnableToDeleteVMWithID, state.ID.ValueString()), err).Error(),
		)
		return
	}
	defer response.Body.Close()
	tflog.Info(ctx, virtualmachines.LogIssuedDeleteGPCNVirtualMachineJob)

	// Read the response body and process it as deleteVirtualMachineResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorReadingDeleteBody,
			err.Error(),
		)
		return
	}

	var deleteVirtualMachineResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &deleteVirtualMachineResponse)

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorUnmarshalingDelete,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailUnmarshalingDeleteWithID, state.ID.ValueString()), err).Error(),
		)
		return
	}

	_, err = client.PerformLongPolling(r.client, ctx, "Delete GPCN Virtual Machine", deleteVirtualMachineResponse.Data.JobID)

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryEncounteredErrorGettingJobInfo,
			fmt.Errorf("%s: %w", virtualmachines.ErrDetailJobInfoCheckDashboard, err).Error(),
		)
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedDeleteGPCNVirtualMachine)
}

func (r *virtualMachinesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ModifyPlan determines whether a size_id change is a valid in-place upgrade or requires replacement.
// It calls the API with vmId to get only sizes that are valid upgrade targets for the current VM.
// If the planned size_id is not among them, the resource must be replaced.
func (r *virtualMachinesResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.client == nil {
		return
	}

	// Only relevant on update (state and plan both non-null)
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state, plan virtualmachines.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Skip if size_id is unchanged or unknown
	if state.SizeId.Equal(plan.SizeId) || plan.SizeId.IsUnknown() {
		return
	}

	// A read-back that failed after a resize leaves state behind the machine. GPCN keeps
	// the current SKU out of its own upgrade list, so the list alone would answer with a
	// replacement. A machine that already carries the planned SKU needs no change. An
	// unreadable detail falls through to the list, which fails the plan on its own.
	detail, detailErr := virtualmachines.GetVirtualMachine(r.client, ctx, state.ID.ValueString())
	if detailErr == nil && detail.Data.Configuration.SkuId == plan.SizeId.ValueString() {
		return
	}

	// Fetch only the sizes that are valid in-place upgrade targets for this VM
	upgradeable, err := virtualmachinesizes.FetchSizes(r.client, ctx, state.DatacenterId.ValueString(), state.ID.ValueString())
	if err != nil {
		// A transient error here must fail the plan, not propose a destroy.
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToDetermineSizeChange,
			fmt.Sprintf(virtualmachines.ErrDetailFetchUpgradeSizesFailed, err.Error()),
		)
		return
	}

	for _, s := range upgradeable {
		if s.ID == plan.SizeId.ValueString() {
			return
		}
	}

	resp.RequiresReplace.Append(path.Root("size_id"))
}

/*
A stop and a start cost the user real time. The provider takes them only where GPCN
needs them. The image of the machine must take no network hotplug. State reports that
capability, because the image of a machine does not drift. The decision then asks two
questions of each attribute below. Does the configuration ask for a change? Does the
machine still lack that change?
  - l2_segment_ids against the live segment set
  - size_id against the live SKU
*/
func determineIfVMNeedsStopped(state, plan virtualmachines.ResourceModel, live *virtualmachines.ReadVirtualMachinesResponse, liveInterfaces []networks.ReadVirtualMachineNetworkDataResponseTF) bool {
	if state.NetworkHotplug.ValueBool() {
		return false
	}

	segmentsOutstanding := !plan.L2SegmentIds.Equal(state.L2SegmentIds) &&
		!slices.Equal(liveSegmentIds(liveInterfaces), sortedSegmentIds(plan.L2SegmentIds))
	sizeOutstanding := !state.SizeId.Equal(plan.SizeId) &&
		live.Data.Configuration.SkuId != plan.SizeId.ValueString()

	return segmentsOutstanding || sizeOutstanding
}

// liveSegmentIds reads the segments the machine carries now. It renders them the way
// sortedSegmentIds renders a planned element. An unknown planned element therefore
// matches no live segment and still asks for a stop.
func liveSegmentIds(liveInterfaces []networks.ReadVirtualMachineNetworkDataResponseTF) []string {
	segmentIds := make([]string, 0, len(liveInterfaces))
	for _, liveInterface := range liveInterfaces {
		if liveInterface.World.ValueString() != networks.NicWorldL2 || liveInterface.L2SegmentID.IsNull() {
			continue
		}
		segmentIds = append(segmentIds, liveInterface.L2SegmentID.String())
	}
	slices.Sort(segmentIds)
	return segmentIds
}

// sortedSegmentIds reads a segment list in a stable order. GPCN gives the interfaces of
// a machine no order. A reordered list carries the same segments and asks for nothing.
// An unknown element sorts under its own rendering and therefore still asks for a stop.
func sortedSegmentIds(list types.List) []string {
	segmentIds := make([]string, 0, len(list.Elements()))
	for _, element := range list.Elements() {
		segmentIds = append(segmentIds, element.String())
	}
	slices.Sort(segmentIds)
	return segmentIds
}
