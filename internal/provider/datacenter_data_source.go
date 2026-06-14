package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/datacenters"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewDatacenterDataSource() datasource.DataSource {
	return &datacenterDataSource{}
}

type datacenterDataSource struct {
	client *client.GpcnClient
}

type datacenterDataSourceModel struct {
	Name         types.String `tfsdk:"name"`
	CountryName  types.String `tfsdk:"country_name"`
	RegionName   types.String `tfsdk:"region_name"`
	GPUEnabled   types.Bool   `tfsdk:"gpu_enabled"`
	CustomImages types.Bool   `tfsdk:"custom_images"`
	DataCenters  types.List   `tfsdk:"datacenters"`
}

type datacenterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		ID                  string `json:"id"`
		Name                string `json:"name"`
		RegionID            int64  `json:"regionId"`
		RegionName          string `json:"regionName"`
		CountryID           string `json:"countryId"`
		CountryName         string `json:"countryName"`
		CountryAbbreviation string `json:"countryAbbreviation"`
		GPUEnabled          bool   `json:"gpuEnabled"`
		CustomImages        bool   `json:"customImages"`
	} `json:"data"`
}
type datacenterDataResponseTF struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	RegionID            types.Int64  `tfsdk:"region_id"`
	RegionName          types.String `tfsdk:"region_name"`
	CountryID           types.String `tfsdk:"country_id"`
	CountryName         types.String `tfsdk:"country_name"`
	CountryAbbreviation types.String `tfsdk:"country_abbreviation"`
	GPUEnabled          types.Bool   `tfsdk:"gpu_enabled"`
	CustomImages        types.Bool   `tfsdk:"custom_images"`
}

type datacenterRegionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		ID                  int64  `json:"id"`
		Name                string `json:"name"`
		CountryID           int64  `json:"countryId"`
		CountryName         string `json:"countryName"`
		CountryAbbreviation string `json:"countryAbbreviation"`
	} `json:"data"`
}

type datacenterCountryResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

func (o datacenterDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                   types.StringType,
		"name":                 types.StringType,
		"region_id":            types.Int64Type,
		"region_name":          types.StringType,
		"country_id":           types.StringType,
		"country_name":         types.StringType,
		"country_abbreviation": types.StringType,
		"gpu_enabled":          types.BoolType,
		"custom_images":        types.BoolType,
	}
}

func (d *datacenterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datacenters"
}

func (d *datacenterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves information about available GPCN datacenters. Use this data source to filter and find datacenters by name, country, or region.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter datacenters by name",
			},
			"country_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter datacenters by country name (e.g., 'United States', 'Canada').",
			},
			"region_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter datacenters by region name within a country. (e.g., 'East', 'West', 'Central')",
			},
			"gpu_enabled": schema.BoolAttribute{
				Optional:    true,
				Description: "Filter datacenters to only those with GPU support enabled.",
			},
			"custom_images": schema.BoolAttribute{
				Optional:    true,
				Description: "Filter datacenters to only those that support custom images.",
			},
			"datacenters": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of datacenters matching the specified filter criteria.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier of the datacenter.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the datacenter.",
						},
						"region_id": schema.Int64Attribute{
							Computed:    true,
							Description: "Numeric identifier of the region where the datacenter is located.",
						},
						"region_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the region where the datacenter is located.",
						},
						"country_id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier of the country where the datacenter is located.",
						},
						"country_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the country where the datacenter is located.",
						},
						"country_abbreviation": schema.StringAttribute{
							Computed:    true,
							Description: "Two-letter country code abbreviation (e.g., 'US').",
						},
						"gpu_enabled": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether GPU resources are available in this datacenter.",
						},
						"custom_images": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether custom images are supported in this datacenter.",
						}},
				},
			},
		},
	}
}

func (d *datacenterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = gpcnClient
}

func (d *datacenterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)

	var state datacenterDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Construct request URL from values we have available
	var additionalQueryString string
	if !state.CountryName.IsNull() {
		additionalQueryString += "&countryName=" + url.QueryEscape(state.CountryName.ValueString())
	}
	if !state.RegionName.IsNull() {
		additionalQueryString += "&regionName=" + url.QueryEscape(state.RegionName.ValueString())
	}
	if !state.Name.IsNull() {
		additionalQueryString += "&search=" + url.QueryEscape(state.Name.ValueString())
	}

	datacenterResponse, err := d.getDatacenters(ctx, additionalQueryString)
	if err != nil {
		// Big failure, no helpful error message
		resp.Diagnostics.AddError(
			datacenters.ErrSummaryUnableGetDatacenters,
			err.Error(),
		)
		return
	}

	if !state.GPUEnabled.IsNull() && state.GPUEnabled.ValueBool() {
		unfiltered := datacenterResponse.Data
		filtered := unfiltered[:0]
		for _, dc := range unfiltered {
			if dc.GPUEnabled {
				filtered = append(filtered, dc)
			}
		}
		datacenterResponse.Data = filtered
		// If filtering removed all results, give a specific error rather than falling
		// through to the region/country suggestion logic which would be misleading.
		if len(unfiltered) > 0 && len(filtered) == 0 {
			resp.Diagnostics.AddError(
				datacenters.ErrSummaryUnableGetDatacenters,
				datacenters.ErrDetailDatacenterNoGPUEnabled,
			)
			return
		}
	}

	if !state.CustomImages.IsNull() && state.CustomImages.ValueBool() {
		unfiltered := datacenterResponse.Data
		filtered := unfiltered[:0]
		for _, dc := range unfiltered {
			if dc.CustomImages {
				filtered = append(filtered, dc)
			}
		}
		datacenterResponse.Data = filtered
		if len(unfiltered) > 0 && len(filtered) == 0 {
			resp.Diagnostics.AddError(
				datacenters.ErrSummaryUnableGetDatacenters,
				datacenters.ErrDetailDatacenterNoCustomImages,
			)
			return
		}
	}

	// If no data centers found, search with just country name to make a friendly error message
	if len(datacenterResponse.Data) < 1 {
		datacenterRegionResponse, err := d.getCountriesAndRegions(ctx, state.CountryName.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				datacenters.ErrSummaryUnableGetDatacenters,
				err.Error(),
			)
			return
		}

		if len(datacenterRegionResponse.Data) > 0 {
			var countryAndRegion []string
			for _, region := range datacenterRegionResponse.Data {
				countryAndRegion = append(countryAndRegion, region.CountryName+" - "+region.Name)
			}
			countryAndRegionFormatted := strings.Join(countryAndRegion, ", ")
			resp.Diagnostics.AddError(datacenters.ErrSummaryUnableGetDatacenters, fmt.Sprintf(datacenters.ErrDetailDatacenterNotFound, countryAndRegionFormatted))
			return
		}

		// If no data centers found still, search with nothing and return first 10
		datacenterCountryResponse, err := d.getAllCountries(ctx)
		if err != nil {
			resp.Diagnostics.AddError(
				datacenters.ErrSummaryUnableGetDatacenters,
				err.Error(),
			)
			return
		}
		var countries []string
		for _, country := range datacenterCountryResponse.Data {
			countries = append(countries, country.Name)
		}
		countryAndRegionFormatted := strings.Join(countries, ", ")
		resp.Diagnostics.AddError(datacenters.ErrSummaryUnableGetDatacenters, fmt.Sprintf(datacenters.ErrDetailDatacenterNotFoundCountries, countryAndRegionFormatted))
		return
	}

	var datacenters []datacenterDataResponseTF
	for _, datacenter := range datacenterResponse.Data {
		datacenters = append(datacenters, datacenterDataResponseTF{
			ID:                  types.StringValue(datacenter.ID),
			Name:                types.StringValue(datacenter.Name),
			RegionID:            types.Int64Value(datacenter.RegionID),
			RegionName:          types.StringValue(datacenter.RegionName),
			CountryID:           types.StringValue(datacenter.CountryID),
			CountryName:         types.StringValue(datacenter.CountryName),
			CountryAbbreviation: types.StringValue(datacenter.CountryAbbreviation),
			GPUEnabled:          types.BoolValue(datacenter.GPUEnabled),
			CustomImages:        types.BoolValue(datacenter.CustomImages),
		})
	}

	var listDiags diag.Diagnostics
	state.DataCenters, listDiags = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: datacenterDataResponseTF{}.AttrTypes()}, datacenters)
	if listDiags.HasError() {
		resp.Diagnostics.Append(listDiags...)
		return
	}

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (d *datacenterDataSource) getDatacenters(ctx context.Context, queryString string) (*datacenterResponse, error) {
	datacenterUrl := datacenters.BASE_URL_V1 + "?page=1&limit=100"
	request, err := http.NewRequestWithContext(ctx, "GET", datacenterUrl+queryString, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as datacenterResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var dcResp datacenterResponse
	err = json.Unmarshal(body, &dcResp)

	if err != nil {
		return nil, err
	}
	return &dcResp, nil
}

func (d *datacenterDataSource) getCountriesAndRegions(ctx context.Context, countryName string) (*datacenterRegionResponse, error) {
	// Safe to use since it'll default to empty string if not provided, which will just search all countries
	// countryName is already URL-encoded by the caller
	datacenterUrl := datacenters.BASE_URL_V1 + "regions?page=1&limit=100&countryName=" + url.QueryEscape(countryName)
	request, err := http.NewRequestWithContext(ctx, "GET", datacenterUrl, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as datacenterRegionResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var dcRegionResp datacenterRegionResponse
	err = json.Unmarshal(body, &dcRegionResp)

	if err != nil {
		return nil, err
	}
	return &dcRegionResp, nil
}

func (d *datacenterDataSource) getAllCountries(ctx context.Context) (*datacenterCountryResponse, error) {
	// Safe to use since it'll default to empty string if not provided, which will just search all countries
	datacenterUrl := datacenters.BASE_URL_V1 + "countries?page=1&limit=100"
	request, err := http.NewRequestWithContext(ctx, "GET", datacenterUrl, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as datacenterCountryResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var dcCountryResp datacenterCountryResponse
	err = json.Unmarshal(body, &dcCountryResp)

	if err != nil {
		return nil, err
	}
	return &dcCountryResp, nil
}
