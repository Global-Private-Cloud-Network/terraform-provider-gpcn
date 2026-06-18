package provider

import (
	"context"
	"fmt"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/virtualmachineimages"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewVirtualMachineImagesDataSource() datasource.DataSource {
	return &virtualMachineImagesDataSource{}
}

type virtualMachineImagesDataSource struct {
	client *client.GpcnClient
}

type virtualMachineImagesDataSourceModel struct {
	DatacenterId types.String `tfsdk:"datacenter_id"`
	ImageType    types.String `tfsdk:"image_type"`
	ImageName    types.String `tfsdk:"image_name"`
	Images       types.List   `tfsdk:"images"`
}

type virtualMachineImageTF struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	ImageType types.String `tfsdk:"image_type"`
}

func (o virtualMachineImageTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":         types.StringType,
		"name":       types.StringType,
		"image_type": types.StringType,
	}
}

func (d *virtualMachineImagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtualmachine_images"
}

func (d *virtualMachineImagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves available virtual machine images for a given datacenter. Results can be filtered by image type and/or name",
		Attributes: map[string]schema.Attribute{
			"datacenter_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the datacenter to retrieve images for",
			},
			"image_type": schema.StringAttribute{
				Optional:    true,
				Description: fmt.Sprintf("Filter images by type. Must be one of: %s.", strings.Join(virtualmachineimages.ValidImageTypes, ", ")),
				Validators: []validator.String{
					stringvalidator.OneOf(virtualmachineimages.ValidImageTypes...),
				},
			},
			"image_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter images by name (case-insensitive substring match)",
			},
			"images": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of virtual machine images matching the specified filter criteria",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier of the image. Use this as the image_id in a gpcn_virtualmachine resource",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Human-readable name of the image",
						},
						"image_type": schema.StringAttribute{
							Computed:    true,
							Description: "The category this image belongs to",
						},
					},
				},
			},
		},
	}
}

func (d *virtualMachineImagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			virtualmachineimages.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(virtualmachineimages.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	d.client = gpcnClient
}

func (d *virtualMachineImagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)

	var state virtualMachineImagesDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	allImages, err := virtualmachineimages.FetchImages(d.client, ctx, state.DatacenterId.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachineimages.ErrSummaryUnableToFetchImages,
			err.Error(),
		)
		return
	}

	imageType := ""
	if !state.ImageType.IsNull() {
		imageType = state.ImageType.ValueString()
	}
	imageName := ""
	if !state.ImageName.IsNull() {
		imageName = state.ImageName.ValueString()
	}

	filtered := virtualmachineimages.FilterImages(ctx, allImages, imageType, imageName)

	if len(filtered) == 0 && imageName != "" {
		// Build a helpful list of available image names from the full set for context
		var names []string
		for _, img := range allImages {
			names = append(names, img.Name)
		}
		resp.Diagnostics.AddError(
			virtualmachineimages.ErrSummaryUnableToFetchImages,
			fmt.Sprintf(virtualmachineimages.ErrDetailImageNameNotFound, imageName, strings.Join(names, ", ")),
		)
		return
	}

	var tfImages []virtualMachineImageTF
	for _, img := range filtered {
		tfImages = append(tfImages, virtualMachineImageTF{
			ID:        types.StringValue(img.ID),
			Name:      types.StringValue(img.Name),
			ImageType: types.StringValue(img.ImageType),
		})
	}

	var listDiags diag.Diagnostics
	state.Images, listDiags = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: virtualMachineImageTF{}.AttrTypes()}, tfImages)
	if listDiags.HasError() {
		resp.Diagnostics.Append(listDiags...)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
