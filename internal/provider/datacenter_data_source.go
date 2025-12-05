package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"terraform-provider-gpcn/internal/datacenters"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewDatacenterDataSource() datasource.DataSource {
	return &datacenterDataSource{}
}

type datacenterDataSource struct {
	client *http.Client
}

type datacenterDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	CountryName types.String `tfsdk:"country_name"`
	RegionName  types.String `tfsdk:"region_name"`
	DataCenters types.List   `tfsdk:"datacenters"`
}

type datacenterResponse struct {
	Success bool                     `json:"success"`
	Message string                   `json:"message"`
	Data    []datacenterDataResponse `json:"data"`
}
type datacenterDataResponse struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	RegionID            int64  `json:"regionId"`
	RegionName          string `json:"regionName"`
	CountryID           int64  `json:"countryId"`
	CountryName         string `json:"countryName"`
	CountryAbbreviation string `json:"countryAbbreviation"`
}
type datacenterDataResponseTF struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	RegionID            types.Int64  `tfsdk:"region_id"`
	RegionName          types.String `tfsdk:"region_name"`
	CountryID           types.Int64  `tfsdk:"country_id"`
	CountryName         types.String `tfsdk:"country_name"`
	CountryAbbreviation types.String `tfsdk:"country_abbreviation"`
}

type datacenterRegionResponse struct {
	Success bool                           `json:"success"`
	Message string                         `json:"message"`
	Data    []datacenterRegionDataResponse `json:"data"`
}
type datacenterRegionDataResponse struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	CountryID           int64  `json:"countryId"`
	CountryName         string `json:"countryName"`
	CountryAbbreviation string `json:"countryAbbreviation"`
}

type datacenterCountryResponse struct {
	Success bool                            `json:"success"`
	Message string                          `json:"message"`
	Data    []datacenterCountryDataResponse `json:"data"`
}
type datacenterCountryDataResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (o datacenterDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                   types.StringType,
		"name":                 types.StringType,
		"region_id":            types.Int64Type,
		"region_name":          types.StringType,
		"country_id":           types.Int64Type,
		"country_name":         types.StringType,
		"country_abbreviation": types.StringType,
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
						"country_id": schema.Int64Attribute{
							Computed:    true,
							Description: "Numeric identifier of the country where the datacenter is located.",
						},
						"country_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the country where the datacenter is located.",
						},
						"country_abbreviation": schema.StringAttribute{
							Computed:    true,
							Description: "Two-letter country code abbreviation (e.g., 'US').",
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

	client, ok := req.ProviderData.(*http.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *http.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

func (d *datacenterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
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
		additionalQueryString += "&name=" + url.QueryEscape(state.Name.ValueString())
	}

	datacenterResponse, err := d.getDatacenters(additionalQueryString)
	if err != nil {
		// Big failure, no helpful error message
		resp.Diagnostics.AddError(
			datacenters.ErrSummaryUnableGetDatacenters,
			err.Error(),
		)
		return
	}

	// If no data centers found, search with just country name to make a friendly error message
	if len(datacenterResponse.Data) < 1 {
		datacenterRegionResponse, err := d.getCountriesAndRegions(url.QueryEscape(state.CountryName.ValueString()))
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
		datacenterCountryResponse, err := d.getAllCountries()
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
			CountryID:           types.Int64Value(datacenter.CountryID),
			CountryName:         types.StringValue(datacenter.CountryName),
			CountryAbbreviation: types.StringValue(datacenter.CountryAbbreviation),
		})
	}

	state.DataCenters, _ = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: datacenterDataResponseTF{}.AttrTypes()}, datacenters)

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (d *datacenterDataSource) getDatacenters(queryString string) (*datacenterResponse, error) {
	datacenterUrl := datacenters.BASE_URL + "?page=1&limit=100"
	request, err := http.NewRequest("GET", datacenterUrl+queryString, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as datacenterResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var datacenterResponse datacenterResponse
	err = json.Unmarshal(body, &datacenterResponse)

	if err != nil {
		return nil, err
	}
	return &datacenterResponse, nil
}

func (d *datacenterDataSource) getCountriesAndRegions(countryName string) (*datacenterRegionResponse, error) {
	// Safe to use since it'll default to empty string if not provided, which will just search all countries
	datacenterUrl := datacenters.BASE_URL + "regions?page=1&limit=100&countryName=" + countryName
	request, err := http.NewRequest("GET", datacenterUrl, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as datacenterRegionResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var datacenterRegionResponse datacenterRegionResponse
	err = json.Unmarshal(body, &datacenterRegionResponse)

	if err != nil {
		return nil, err
	}
	return &datacenterRegionResponse, nil
}

func (d *datacenterDataSource) getAllCountries() (*datacenterCountryResponse, error) {
	// Safe to use since it'll default to empty string if not provided, which will just search all countries
	datacenterUrl := datacenters.BASE_URL + "countries?page=1&limit=100"
	request, err := http.NewRequest("GET", datacenterUrl, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as datacenterCountryResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var datacenterCountryResponse datacenterCountryResponse
	err = json.Unmarshal(body, &datacenterCountryResponse)

	if err != nil {
		return nil, err
	}
	return &datacenterCountryResponse, nil
}
